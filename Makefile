# Confii-Go Makefile
# ==================

MODULE   := github.com/confiify/confii-go
CLI_PKG  := ./confii
CLI_BIN  := confii
BUILD_DIR := bin

# Build tags for cloud providers
TAGS_AWS   := aws
TAGS_AZURE := azure
TAGS_GCP   := gcp
TAGS_VAULT := vault
TAGS_IBM   := ibm
TAGS_ALL   := $(TAGS_AWS),$(TAGS_AZURE),$(TAGS_GCP),$(TAGS_VAULT),$(TAGS_IBM)

# Go commands
GO       := go
GOTEST   := $(GO) test
GOBUILD  := $(GO) build
GOVET    := $(GO) vet
GOFMT    := gofmt

# Default target
.DEFAULT_GOAL := help

# ---- Build ----

.PHONY: build
build: ## Build the CLI binary
	$(GOBUILD) -o $(BUILD_DIR)/$(CLI_BIN) $(CLI_PKG)

.PHONY: build-all
build-all: ## Build CLI with all cloud provider tags
	$(GOBUILD) -tags "$(TAGS_ALL)" -o $(BUILD_DIR)/$(CLI_BIN) $(CLI_PKG)

.PHONY: install
install: ## Install the CLI binary to $GOPATH/bin
	$(GO) install $(CLI_PKG)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache

# ---- Test ----

.PHONY: test
test: ## Run all unit tests (excludes integration and cloud)
	$(GOTEST) ./... -count=1 -timeout 60s \
		-skip 'Integration' \
		$(if $(VERBOSE),-v)

.PHONY: test-verbose
test-verbose: ## Run all unit tests with verbose output
	VERBOSE=1 $(MAKE) test

.PHONY: test-short
test-short: ## Run tests in short mode (skip slow tests)
	$(GOTEST) ./... -short -count=1 -timeout 30s

.PHONY: test-integration
test-integration: ## Run integration tests
	$(GOTEST) ./integration/... -count=1 -timeout 120s -v

.PHONY: test-race
test-race: ## Run tests with race detector
	$(GOTEST) ./... -race -count=1 -timeout 120s

.PHONY: test-cover
test-cover: ## Run tests with coverage report
	$(GOTEST) ./... -count=1 -timeout 60s \
		-coverprofile=coverage.out \
		-covermode=atomic
	$(GO) tool cover -func=coverage.out
	@echo ""
	@echo "To view HTML report: make test-cover-html"

.PHONY: test-cover-html
test-cover-html: test-cover ## Generate and open HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: test-cloud
test-cloud: ## Run tests with all cloud build tags (requires credentials)
	$(GOTEST) -tags "$(TAGS_ALL)" ./... -count=1 -timeout 120s

.PHONY: bench
bench: ## Run benchmarks
	$(GOTEST) ./... -bench=. -benchmem -run='^$$' -timeout 120s

# ---- Code Quality ----

.PHONY: fmt
fmt: ## Format all Go files with gofmt -s
	$(GOFMT) -s -w .

.PHONY: fmt-check
fmt-check: ## Check if all Go files are formatted (CI-friendly)
	@test -z "$$($(GOFMT) -s -l .)" || { \
		echo "Files not formatted:"; \
		$(GOFMT) -s -l .; \
		exit 1; \
	}

.PHONY: vet
vet: ## Run go vet
	$(GOVET) ./...

.PHONY: vet-all
vet-all: ## Run go vet with all cloud build tags
	$(GOVET) -tags "$(TAGS_ALL)" ./...

.PHONY: lint
lint: fmt-check vet ## Run all linters (fmt-check + vet)
	@echo "All lint checks passed."

.PHONY: tidy
tidy: ## Run go mod tidy and verify
	$(GO) mod tidy
	$(GO) mod verify

# ---- CI ----

.PHONY: ci
ci: tidy fmt-check vet test ## Run full CI pipeline (tidy + lint + test)
	@echo ""
	@echo "CI passed."

.PHONY: ci-full
ci-full: tidy fmt-check vet-all test test-race test-integration ## Full CI with race detection and integration tests
	@echo ""
	@echo "Full CI passed."

# ---- Development ----

.PHONY: deps
deps: ## Download and verify dependencies
	$(GO) mod download
	$(GO) mod verify

.PHONY: update-deps
update-deps: ## Update all dependencies to latest minor/patch
	$(GO) get -u ./...
	$(GO) mod tidy

.PHONY: check
check: fmt vet test ## Quick check: format, vet, and test
	@echo ""
	@echo "All checks passed."

# ---- Help ----

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
