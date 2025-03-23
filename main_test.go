package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivershared/riversharedtest"
)

var testConfig = &EnvConfig{ //nolint:gochecknoglobals
	SMTPHost: "example.com:1234",
	SMTPPass: "not-a-pass",
	SMTPUser: "not-a-user",
}

func TestAPIServiceEmailCreate(t *testing.T) {
	t.Parallel()

	type testBundle struct {
		apiServer *APIService
		tx        pgx.Tx
	}

	setup := func(t *testing.T) (*testBundle, context.Context) {
		t.Helper()

		var (
			ctx = t.Context()
			tx  = riversharedtest.TestTx(ctx, t)
		)

		riverClient, err := river.NewClient(riverpgxv5.New(nil), &river.Config{
			TestOnly: true,
			Workers:  makeWorkers(testConfig),
		})
		require.NoError(t, err)

		return &testBundle{
			apiServer: &APIService{
				begin:       tx.Begin,
				riverClient: riverClient,
			},
			tx: tx,
		}, ctx
	}

	var (
		accountID      = uuid.New()
		idempotencyKey = uuid.New()
	)

	testArgs := func(overrides *HandleEmailCreateRequest) *HandleEmailCreateRequest {
		if overrides == nil {
			overrides = &HandleEmailCreateRequest{}
		}

		return &HandleEmailCreateRequest{
			AccountID:      cmp.Or(overrides.AccountID, accountID),
			Body:           cmp.Or(overrides.Body, "Hello from River's idempotent mail demo."),
			EmailRecipient: cmp.Or(overrides.EmailRecipient, "receiver@example.com"),
			EmailSender:    cmp.Or(overrides.EmailSender, "sender@example.com"),
			IdempotencyKey: cmp.Or(overrides.IdempotencyKey, idempotencyKey),
			Subject:        cmp.Or(overrides.Subject, "Hello."),
		}
	}

	t.Run("InsertsJobOnce", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)
	})

	t.Run("InsertsJobIdempotently", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)

		resp, err = invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email was already queued and is pending send."}, resp)
	})

	t.Run("ReportsAlreadySent", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)

		// Cheat a little by setting the job row directly to completed as if it
		// it'd been worked by the background worker already.
		_, err = bundle.tx.Exec(ctx, "UPDATE river_job SET finalized_at = now(), state = 'completed' WHERE kind = $1", (SendEmailArgs{}).Kind())
		require.NoError(t, err)

		resp, err = invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been sent."}, resp)
	})

	t.Run("UniqueVariesOnAccountID", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)

		resp, err = invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(&HandleEmailCreateRequest{
			AccountID: uuid.New(),
		}))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)
	})

	t.Run("UniqueVariesOnIdempotencyKey", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)

		resp, err = invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(&HandleEmailCreateRequest{
			IdempotencyKey: uuid.New(),
		}))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)
	})

	// Unique depends on account ID and idempotency key only. Varying other
	// fields results in a mismatched parameters error.
	t.Run("MismatchedParametersError", func(t *testing.T) {
		t.Parallel()

		bundle, ctx := setup(t)

		resp, err := invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(nil))
		require.NoError(t, err)
		require.Equal(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, resp)

		// Test each field in its own API request to make sure a mismatch produces the expected error.
		for _, overrides := range []*HandleEmailCreateRequest{
			{Body: "A different body"},
			{EmailRecipient: "different@example.com"},
			{EmailSender: "different@example.com"},
			{Subject: "A different subject"},
		} {
			_, err = invokeHandler(ctx, bundle.apiServer.EmailCreate, testArgs(overrides))
			require.Equal(t, &APIError{StatusCode: http.StatusBadRequest, Message: "Incoming parameters don't match those of queued email. You may have a bug."}, err)
		}
	})
}

// Integration tests that exercise the entire HTTP stack.
func TestAPIServiceServeMux(t *testing.T) {
	t.Parallel()

	type testBundle struct {
		mux *http.ServeMux
		tx  pgx.Tx
	}

	setup := func(t *testing.T) (*testBundle, context.Context) {
		t.Helper()

		var (
			ctx = t.Context()
			tx  = riversharedtest.TestTx(ctx, t)
		)

		riverClient, err := river.NewClient(riverpgxv5.New(nil), &river.Config{
			TestOnly: true,
			Workers:  makeWorkers(testConfig),
		})
		require.NoError(t, err)

		return &testBundle{
			mux: (&APIService{
				begin:       tx.Begin,
				riverClient: riverClient,
			}).ServeMux(),
			tx: tx,
		}, ctx
	}

	mustMarshalJSON := func(t *testing.T, value any) []byte {
		t.Helper()

		data, err := json.Marshal(value)
		require.NoError(t, err)

		return data
	}

	requireStatus := func(t *testing.T, expected int, recorder *httptest.ResponseRecorder) {
		t.Helper()

		require.Equal(t, expected, recorder.Code, "Unexpected status; response body: %s", recorder.Body.String())
	}

	t.Run("EmailCreate", func(t *testing.T) {
		t.Parallel()

		bundle, _ := setup(t)

		accountID := uuid.New()

		recorder := httptest.NewRecorder()

		bundle.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/emails", bytes.NewReader(mustMarshalJSON(t, &HandleEmailCreateRequest{
			AccountID:      accountID,
			Body:           "Hello from River's idempotent mail demo.",
			EmailRecipient: "receiver@example.com",
			EmailSender:    "sender@example.com",
			IdempotencyKey: uuid.New(),
			Subject:        "Hello.",
		}))))
		requireStatus(t, http.StatusOK, recorder)
		require.Equal(t,
			string(mustMarshalJSON(t, &HandleEmailCreateResponse{Message: "Email has been queued for sending."})),
			recorder.Body.String(),
		)
	})
}

// invokeHandler invokes a service handler and returns its results.
//
// Service handlers are normal functions and can be invoked directly, but it's
// preferable to invoke them with this function because a few extra niceties are
// observed that are normally only available from the API framework:
//
//   - Incoming request structs are validated and an API error is emitted in case
//     they're invalid (any `validate` tags are checked).
//   - Outgoing response structs are validated.
//
// Sample invocation:
//
//	endpoint := &testEndpoint{}
//	resp, err := apitest.InvokeHandler(ctx, endpoint.Execute, &testRequest{ReqField: "string"})
//	require.NoError(t, err)
func invokeHandler[TReq any, TResp any](ctx context.Context, handler func(context.Context, *TReq) (*TResp, error), req *TReq) (*TResp, error) {
	if err := validate.StructCtx(ctx, req); err != nil {
		return nil, &APIError{StatusCode: http.StatusBadRequest, Message: "Invalid parameters: " + err.Error()}
	}

	resp, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := validate.StructCtx(ctx, resp); err != nil {
		return nil, fmt.Errorf("apitest: error validating response API resource: %w", err)
	}

	return resp, nil
}
