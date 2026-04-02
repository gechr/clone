GO       ?= go
GO_BIN   ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS ?= $(shell $(GO) tool | grep /)

.PHONY: all
all: fmt lint test

.PHONY: fmt
fmt:
	@$(GO) fix ./...
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint fmt --enable=gci,golines,gofumpt
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run --fix --enable-only tagalign

.PHONY: install
install:
	@$(GO) install
	@$(GO_BIN)/clone --install-completion

.PHONY: lint
lint:
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run

.PHONY: test
test:
	@$(GO) test -timeout 30s -race ./...

.PHONY: update
update:
	@$(GO) get $(GO_TOOLS) $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -m all)
	@$(GO) mod tidy
