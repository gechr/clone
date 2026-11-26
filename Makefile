GO         ?= go
GO_BIN     ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS   ?= $(shell $(GO) list tool)

VERSION    ?= $(shell git describe --tags 2>/dev/null || echo 0.0.0-dev)
GO_LDFLAGS ?= -s -w -X github.com/gechr/clive.version=$(VERSION)

.PHONY: all
all: fmt lint test

.PHONY: fmt
fmt:
	@clover format
	@rumdl fmt --quiet
	@$(GO) fix ./...
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint fmt --enable=gci,golines,gofumpt
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run --fix -c .golangci.ruleguard.yml

DIST_DIR ?= dist

.PHONY: demo
demo:
	@set -eu; \
	  DEMO_DIR=$$(mktemp -d); export DEMO_DIR; \
	  trap 'rm -rf "$$DEMO_DIR"' EXIT; \
	  mkdir "$$DEMO_DIR/repos"; \
	  $(MAKE) --no-print-directory build DIST_DIR="$$DEMO_DIR/bin"; \
	  reel run assets/help.tape -o assets/help.png; \
	  reel run assets/demo.tape --padding=15 --hold-last=1500ms -o assets/demo.gif

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
	@clover run
	@$(GO) get $(GO_TOOLS) $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -m all)
	@$(GO) mod tidy
