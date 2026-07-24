# Confii-Go Makefile
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT
# ==================

MODULE   := github.com/confiify/confii-go
CLI_PKG  := ./confii
CLI_BIN  := confii
BUILD_DIR := bin
TOOLS_DIR := $(BUILD_DIR)/tools

GOBCO_VERSION := v1.3.4
GOBCO_BIN := $(abspath $(TOOLS_DIR)/gobco)

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

.PHONY: reproducible-build-check
reproducible-build-check: ## Verify two clean CLI builds are byte-for-byte identical
	sh scripts/check-reproducible-build.sh

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
	sh scripts/check-statement-coverage.sh coverage.out
	@echo ""
	@echo "To view HTML report: make test-cover-html"

.PHONY: test-cover-html
test-cover-html: test-cover ## Generate and open HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: test-branch-cover
test-branch-cover: $(GOBCO_BIN) ## Enforce 80% condition/branch coverage with pinned Gobco
	GOBCO="$(GOBCO_BIN)" sh scripts/check-branch-coverage.sh

$(GOBCO_BIN):
	@mkdir -p "$(@D)"
	GOBIN="$(abspath $(@D))" $(GO) install github.com/rillig/gobco@$(GOBCO_VERSION)

.PHONY: test-cloud
test-cloud: ## Test all cloud providers in an isolated consumer module
	sh scripts/test-cloud-consumer.sh "$(TAGS_ALL)" test

.PHONY: bench
bench: ## Run benchmarks
	$(GOTEST) ./... -bench=. -benchmem -run='^$$' -timeout 120s

# ---- Fuzz ----
#
# `make fuzz` runs every known fuzz target sequentially for a configurable
# duration (default 30 seconds per target). Set FUZZTIME to override:
#
#   make fuzz FUZZTIME=2m
#
# The targets listed below mirror the matrix in .github/workflows/ci.yaml.
# When adding a new Fuzz<Name> function, append it to FUZZ_TARGETS so CI
# and `make fuzz` agree on coverage.

FUZZTIME ?= 30s

FUZZ_TARGETS := \
	./loader:FuzzYAMLLoader \
	./loader:FuzzJSONLoader \
	./loader:FuzzTOMLLoader \
	./loader:FuzzEnvFileLoader \
	./loader:FuzzUnquoteEnvValue \
	./secret:FuzzResolverResolve \
	./internal/dictutil:FuzzGetNested \
	./internal/dictutil:FuzzSetNested \
	./internal/dictutil:FuzzDeepMerge

.PHONY: fuzz
fuzz: ## Run all fuzz targets for FUZZTIME each (default 30s; e.g. make fuzz FUZZTIME=2m)
	@for target in $(FUZZ_TARGETS); do \
		pkg=$${target%%:*}; \
		fn=$${target##*:}; \
		echo "==> Fuzzing $$fn in $$pkg for $(FUZZTIME)"; \
		$(GOTEST) -run='^$$' -fuzz="^$${fn}$$" -fuzztime=$(FUZZTIME) -timeout=120s $$pkg || exit 1; \
	done
	@echo ""
	@echo "All fuzz targets passed."

.PHONY: fuzz-seeds
fuzz-seeds: ## Run fuzz targets in seed-only mode (no -fuzz flag) — fast unit-test path
	$(GOTEST) -run='^Fuzz' -count=1 -timeout 60s \
		./loader/... ./secret/... ./internal/dictutil/...

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
vet-all: ## Vet core and cloud providers (cloud SDKs isolated from go.mod)
	$(GOVET) ./...
	sh scripts/test-cloud-consumer.sh "$(TAGS_ALL)" vet

.PHONY: lint
lint: fmt-check vet ## Run all linters (fmt-check + vet + golangci-lint)
	golangci-lint run ./...

.PHONY: docs-check
docs-check: ## Build documentation with warnings treated as errors
	mkdocs build --strict

.PHONY: vulncheck
vulncheck: ## Run Go vulnerability checks for core and cloud modules
	govulncheck ./...
	sh scripts/test-cloud-consumer.sh "$(TAGS_ALL)" vuln

.PHONY: mod-verify
mod-verify: ## Verify every module is tidy and internally consistent
	sh scripts/verify-modules.sh

# ---- CI ----

.PHONY: ci
ci: mod-verify fmt-check vet test ## Run core module, format, vet, and test gates
	@echo ""
	@echo "CI passed."

.PHONY: ci-full
ci-full: mod-verify fmt-check vet-all reproducible-build-check test test-race test-integration test-cloud test-branch-cover ## Full CI including reproducibility, coverage, race, integration, and cloud consumer tests
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
	$(GO) mod verify

.PHONY: check
check: fmt vet test ## Quick check: format, vet, and test
	@echo ""
	@echo "All checks passed."

# ---- Help ----

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
