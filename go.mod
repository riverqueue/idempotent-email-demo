module github.com/riverqueue/idempotent-email-demo

go 1.24.1

require (
	github.com/go-playground/validator/v10 v10.25.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.4
	github.com/riverqueue/river v0.20.1
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.20.1
	github.com/riverqueue/river/rivershared v0.20.1
	github.com/riverqueue/river/rivertype v0.20.1
	github.com/sethvargo/go-envconfig v1.1.1
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/riverqueue/river/riverdriver v0.20.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// replace github.com/riverqueue/river => ../river

// replace github.com/riverqueue/river/rivershared => ../river/rivershared
