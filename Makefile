GO       ?= go
GO_BIN   ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS ?= $(shell $(GO) tool | grep /)

GIT_DESCRIBE ?= $(shell git describe --tags 2>/dev/null || echo 0.0.0-dev)
VERSION      ?= $(shell echo "$(GIT_DESCRIBE)" | sed -E 's/^(v?[0-9]+\.[0-9]+\.[0-9]+)-[0-9]+-g([a-f0-9]+)$$/\1-\2-dev/')
GO_LDFLAGS   ?= -s -w -X main.version=$(VERSION)

.PHONY: all
all: fmt lint test

.PHONY: fmt
fmt:
	@$(GO) fix ./...
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint fmt --enable=gci,golines,gofumpt

DIST_DIR ?= dist

.PHONY: demo
demo:
	@vhs assets/demo.tape > /dev/null
	@vhs assets/help.tape > /dev/null

.PHONY: build
build:
	@$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(DIST_DIR)/clone .

.PHONY: install
install:
	@$(GO) install -ldflags "$(GO_LDFLAGS)"
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
