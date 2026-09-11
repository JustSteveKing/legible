# legible — see `make help`.
#
# Targets carrying a `##` comment are the ones meant to be run by hand; help
# lists exactly those, so a new target documents itself or stays out of the way.

BIN     := bin/legible
PKG     := ./cmd/legible
SPEC    ?= testdata/petstore.json

# Stamped into main.version. Without it every binary reports "dev", which is
# unhelpful the moment one of them is running in someone else's CI.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
GO      ?= go

.DEFAULT_GOAL := help

.PHONY: build
build: ## Build the binary into bin/
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

.PHONY: install
install: ## Install legible into GOBIN
	$(GO) install -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: check
check: fmt-check tidy-check vet test-race ## Everything CI runs

.PHONY: fmt
fmt: ## Format the code
	gofmt -w .

# Checked rather than applied: a build should tell you a file is unformatted,
# not quietly rewrite it underneath you.
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

# go.mod drifting from the imports is the classic green-locally-red-in-CI
# failure, so it is checked rather than trusted.
.PHONY: tidy-check
tidy-check:
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak
	@$(GO) mod tidy
	@if ! cmp -s go.mod go.mod.bak || ! cmp -s go.sum go.sum.bak; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum; \
		echo "go.mod or go.sum is not tidy; run 'go mod tidy'"; exit 1; \
	fi
	@rm -f go.mod.bak go.sum.bak

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test: ## Run the tests
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: run
run: build ## Check a spec (SPEC=path, default the petstore fixture)
	$(BIN) check $(SPEC) --fail-on none

.PHONY: clean
clean: ## Remove build output
	rm -rf bin/ dist/ coverage.out

.PHONY: help
help: ## Show this help
	@echo "legible $(VERSION)"
	@echo
	@awk 'BEGIN {FS = ":.*?## "} \
		/^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
