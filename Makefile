.PHONY: build install uninstall test test-bdd test-e2e test-all lint fmt clean tidy run doctor help

INSTALL_DIR ?= /usr/local/bin

GO       ?= go
BIN_DIR  ?= bin
BIN      := $(BIN_DIR)/mosaic
PKG      := github.com/Mizune/mosaic
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the mosaic binary into ./bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/mosaic

install: build ## Install mosaic to /usr/local/bin (or INSTALL_DIR)
	@install -m 0755 $(BIN) $(INSTALL_DIR)/mosaic
	@echo "installed $(INSTALL_DIR)/mosaic ($(VERSION))"

uninstall: ## Remove mosaic from INSTALL_DIR
	@rm -f $(INSTALL_DIR)/mosaic
	@echo "removed $(INSTALL_DIR)/mosaic"

run: build ## Build and run mosaic
	./$(BIN)

doctor: build ## Build and run mosaic doctor
	./$(BIN) doctor

test: ## Run unit and integration tests
	$(GO) test -race ./...

test-bdd: ## Run Ginkgo BDD specs
	$(GO) test -race ./internal/...

test-e2e: build ## Run Godog E2E feature scenarios
	$(GO) test -race ./features/...

test-all: test test-e2e ## Run all tests

lint: ## go vet + staticcheck
	$(GO) vet ./...
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed; skipping"

fmt: ## Format Go source
	$(GO) fmt ./...

tidy: ## go mod tidy
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) dist cover.out cover.html
