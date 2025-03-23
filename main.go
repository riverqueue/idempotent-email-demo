package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sethvargo/go-envconfig"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

type APIService struct {
	begin       func(ctx context.Context) (pgx.Tx, error)
	riverClient *river.Client[pgx.Tx]
}

type HandleEmailCreateRequest struct {
	AccountID      uuid.UUID `json:"account_id"      validate:"required"`
	Body           string    `json:"body"            validate:"required"`
	EmailRecipient string    `json:"email_recipient" validate:"required"`
	EmailSender    string    `json:"email_sender"    validate:"required"`
	IdempotencyKey uuid.UUID `json:"idempotency_key" validate:"required"`
	Subject        string    `json:"subject"         validate:"required"`
}

type HandleEmailCreateResponse struct {
	Message string `json:"message"`
}

func (s *APIService) EmailCreate(ctx context.Context, req *HandleEmailCreateRequest) (*HandleEmailCreateResponse, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	insertRes, err := s.riverClient.InsertTx(ctx, tx, SendEmailArgs{
		AccountID:      req.AccountID,
		Body:           req.Body,
		EmailRecipient: req.EmailRecipient,
		EmailSender:    req.EmailSender,
		IdempotencyKey: req.IdempotencyKey,
		Subject:        req.Subject,
	}, nil)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	if insertRes.UniqueSkippedAsDuplicate {
		var existingArgs SendEmailArgs
		if err := json.Unmarshal(insertRes.Job.EncodedArgs, &existingArgs); err != nil {
			return nil, err
		}

		// If incoming parameters don't match those of an already queued job,
		// tell the user about it. There's probably a bug in the caller.
		if req.Body != existingArgs.Body ||
			req.EmailRecipient != existingArgs.EmailRecipient ||
			req.EmailSender != existingArgs.EmailSender ||
			req.Subject != existingArgs.Subject {
			return nil, &APIError{
				Message:    "Incoming parameters don't match those of queued email. You may have a bug.",
				StatusCode: http.StatusBadRequest,
			}
		}

		if insertRes.Job.State == rivertype.JobStateCompleted {
			return &HandleEmailCreateResponse{Message: "Email has been sent."}, nil
		}

		return &HandleEmailCreateResponse{Message: "Email was already queued and is pending send."}, nil
	}

	return &HandleEmailCreateResponse{Message: "Email has been queued for sending."}, nil
}

func (s *APIService) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /emails", MakeHandler(s.EmailCreate))
	return mux
}

type SendEmailArgs struct {
	AccountID      uuid.UUID `json:"account_id"      river:"unique"` // simplified for demo; this would be determined through an auth token in real life
	Body           string    `json:"body"            river:"-"`
	EmailRecipient string    `json:"email_recipient" river:"-"`
	EmailSender    string    `json:"email_sender"    river:"-"`
	IdempotencyKey uuid.UUID `json:"idempotency_key" river:"unique"` // simplified for demo; this would come in by `Idempotency-Key` header by convention
	Subject        string    `json:"subject"         river:"-"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

func (SendEmailArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

type SendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
	smtpHost, smtpPass, smtpUser string
}

func (w *SendEmailWorker) Work(ctx context.Context, job *river.Job[SendEmailArgs]) error {
	// This will probably too simple to work in reality, but is here to
	// demonstrate the basic shape of what sending an email would look like.
	var (
		auth    = smtp.PlainAuth("", w.smtpUser, w.smtpPass, w.smtpHost)
		message = []byte(fmt.Sprintf("To: %s\r\n"+
			"Subject: %s\r\n"+
			"\r\n"+
			"%s\r\n",
			job.Args.EmailRecipient,
			job.Args.Subject,
			job.Args.Body,
		))
	)
	return smtp.SendMail(w.smtpHost, auth, job.Args.EmailSender, []string{job.Args.EmailRecipient}, message)
}

func main() {
	ctx := context.Background()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s", err)
		os.Exit(1)
	}
}

type EnvConfig struct {
	DatabaseURL string `env:"DATABASE_URL,required"`
	SMTPHost    string `env:"SMTP_HOST,required"`
	SMTPPass    string `env:"SMTP_PASS,required"`
	SMTPUser    string `env:"SMTP_USER,required"`
}

func makeWorkers(config *EnvConfig) *river.Workers {
	workers := river.NewWorkers()
	river.AddWorker(workers, &SendEmailWorker{
		smtpHost: config.SMTPHost,
		smtpPass: config.SMTPPass,
		smtpUser: config.SMTPUser,
	})
	return workers
}

func run(ctx context.Context) error {
	var config EnvConfig
	if err := envconfig.Process(ctx, &config); err != nil {
		return err
	}

	dbPool, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return err
	}

	riverClient, err := river.NewClient(riverpgxv5.New(dbPool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 100},
		},
		Workers: makeWorkers(&config),
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: ":8080",
		Handler: (&APIService{
			begin:       dbPool.Begin,
			riverClient: riverClient,
		}).ServeMux(),

		// Specified to prevent the "Slowloris" DOS attack, in which an attacker
		// sends many partial requests to exhaust a target server's connections.
		//
		// https://en.wikipedia.org/wiki/Slowloris_(computer_security)
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("Listening on %s\n", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

type APIError struct {
	Message    string `json:"message"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string { return e.Message }

var validate = validator.New() //nolint:gochecknoglobals

// MakeHandler makes an http.Handler that wraps a "service" function. A service
// function is a higher level handler that takes a typed request struct and
// returns a typed response struct along with an error. MakeHandler reads a
// request body, unmarshals it to a typed request, validates the request,
// invokes the inner service function, marshals the response struct to JSON, and
// writes it to the response.
func MakeHandler[TReq any, TResp any](serviceFunc func(ctx context.Context, req *TReq) (*TResp, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqData, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, err)
			return
		}
		defer r.Body.Close()

		var req TReq
		if err := json.Unmarshal(reqData, &req); err != nil {
			writeError(w, &APIError{StatusCode: http.StatusBadRequest, Message: "Error unmarshaling request: " + err.Error()})
			return
		}

		ctx := r.Context()

		if err := validate.StructCtx(ctx, &req); err != nil {
			writeError(w, &APIError{StatusCode: http.StatusBadRequest, Message: "Invalid parameters: " + err.Error()})
			return
		}

		resp, err := serviceFunc(ctx, &req)
		if err != nil {
			writeError(w, err)
			return
		}

		respData, err := json.Marshal(resp)
		if err != nil {
			writeError(w, err)
			return
		}

		if _, err := w.Write(respData); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing response: %s", err)
		}
	})
}

// writeError writes an APIError to w according to its status code and JSON
// marshaled form. If err isn't an APIError, the error is logged and an internal
// server error is sent back.
func writeError(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		fmt.Fprintf(os.Stderr, "Internal error: %s\n", err)
		apiErr = &APIError{StatusCode: http.StatusInternalServerError, Message: "Internal server error."}
	}

	w.WriteHeader(apiErr.StatusCode)

	errorData, err := json.Marshal(apiErr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling error JSON data: %s", err)
		return
	}

	if _, err := w.Write(errorData); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing response: %s", err)
	}
}
