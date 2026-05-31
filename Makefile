BINARY     := brainapi
MODULE     := github.com/wh0amibjm/brainapi-go-sdk
PKG_VER    := $(MODULE)/internal/version

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X $(PKG_VER).Version=$(VERSION) \
  -X $(PKG_VER).Commit=$(COMMIT)   \
  -X $(PKG_VER).Date=$(BUILD_DATE)

GOFLAGS := -trimpath

.PHONY: all build build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64 \
        release test test-short cover lint fmt vet tidy clean help install-hooks \
        install-skill test-live-smoke

all: lint test build ## Run lint, test, and build

build: ## Build for current platform
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/brainapi

build-linux: ## Cross-compile for linux/amd64
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-linux-amd64 ./cmd/brainapi

build-linux-arm64: ## Cross-compile for linux/arm64
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-linux-arm64 ./cmd/brainapi

build-windows: ## Cross-compile for windows/amd64
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-windows-amd64.exe ./cmd/brainapi

build-darwin: ## Cross-compile for darwin/amd64
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-darwin-amd64 ./cmd/brainapi

build-darwin-arm64: ## Cross-compile for darwin/arm64
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-darwin-arm64 ./cmd/brainapi

release: build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64 ## Build all release binaries

test: ## Run tests with race detector
	go test -race -count=1 ./...

test-short: ## Run tests with -short flag
	go test -race -short -count=1 ./...

cover: ## Run tests with coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format with gofumpt -extra
	gofumpt -extra -w .

vet: ## go vet
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

clean: ## Remove build artifacts (cross-platform via Go stdlib)
	@go run ./internal/tools/clean

install-hooks: ## Install pre-commit hooks (sets core.hooksPath=.githooks/)
	@if ! command -v pre-commit >/dev/null 2>&1; then \
		echo "ERROR: pre-commit framework is required by .githooks/pre-commit wrapper."; \
		echo "  Install: pip install pre-commit  (or  uvx pre-commit), then re-run."; \
		exit 1; \
	fi
	git config core.hooksPath .githooks
	@echo "core.hooksPath set to .githooks (uninstall: git config --unset core.hooksPath)"
	@echo "Active hooks: .githooks/{pre-commit, commit-msg, pre-push}"
	@echo "  pre-commit delegates to pre-commit framework (.pre-commit-config.yaml)."
	@echo "  commit-msg enforces Conventional Commits 1.0."
	@echo "  pre-push runs full -race tests + 5-target cross-compile smoke."

install-skill: ## Install the brainapi Agent Skill into ~/.claude/skills (one-command setup)
	@bash clients/skill/install.sh

test-live-smoke: ## Live: 14-step read-only smoke. Requires BRAINAPI_USER/PASS pointing at a data-having test account.
	@go run ./scripts/live-smoke

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
