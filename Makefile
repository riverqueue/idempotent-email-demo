.DEFAULT_GOAL := help

# Looks at comments using ## on targets and uses them to produce a help output.
.PHONY: help
help: ALIGN=22
help: ## Print this message
	@awk -F '::? .*## ' -- "/^[^':]+::? .*## /"' { printf "'$$(tput bold)'%-$(ALIGN)s'$$(tput sgr0)' %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: lint
lint:: ## Run linter
	golangci-lint run --fix

.PHONY: test
test:: ## Run test suite
	go test ./...

.PHONY: test/race
test/race:: ## Run test suite with race detector
	go test ./... -race

.PHONY: tidy
tidy:: ## Run `go mod tidy`
	go mod tidy