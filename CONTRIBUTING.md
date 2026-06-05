# Contributing

Thanks for considering a contribution. A few ground rules.

## Development setup

```bash
git clone https://github.com/wh0amibjm/brainapi-go-sdk.git
cd brainapi-go-sdk
go mod download

# Install hooks. Requires the pre-commit framework — the .githooks/ wrapper
# delegates to it (install: pip install pre-commit, or uvx pre-commit).
make install-hooks
```

### Prerequisites

- **Go 1.26.1+** (`go version`) — building, testing.
- **gofumpt** — formatting (`go install mvdan.cc/gofumpt@latest`).
- **golangci-lint v2.x** — linting (see https://golangci-lint.run/welcome/install/).
- **pre-commit** (Python) — the commit-hook runner (`pip install pre-commit`).

`make build` / `make build-mcp` need only Go; `make all` / `make lint` / `make fmt`
and the commit hooks need the tools above. `make vuln` self-bootstraps
`govulncheck` via `go run`, so it needs nothing extra.

## The protocol contract is upstream

`pkg/brainapi/types.go` mirrors the Chrome-verified HTTP shapes at
`docs/protocol.md`. When BRAIN's protocol changes:

1. Update `docs/protocol.md` first with the new captured payload.
2. Add a fixture under `testdata/` that pins the new shape.
3. Update the typed struct.
4. Add or update the test that asserts the new shape.

Changes that aren't anchored to a captured payload won't be merged — the
spec is the source of truth, not assumptions.

## Pre-commit gate

Every commit must pass (defined in `.pre-commit-config.yaml`):

- `gofumpt -extra` (format)
- `go vet ./...`
- `golangci-lint run --timeout=3m`
- `go mod tidy`
- stock hygiene hooks (trailing-whitespace, end-of-file, check-yaml, …)
- a Conventional-Commits subject check (commit-msg stage)

The full `-race` test suite is **not** a commit hook — it runs on `pre-push`
and in CI. Hooks run via `pre-commit install` or `make install-hooks` (both
delegate to the pre-commit framework).

## Testing

```bash
make test          # full suite with race detector
make test-short    # same as pre-commit
make cover         # coverage report
```

Every public method needs at least one happy-path test. Retry-policy
changes need a dedicated test that asserts the observed behavior under
a representative status code.

For live BRAIN validation:

```bash
BRAINAPI_USER=... BRAINAPI_PASS=... go run ./scripts/live-smoke
```

See `scripts/live-smoke/README.md` for what it does.

## Commit hygiene

- One logical change per commit.
- Conventional-commit-style subject: `feat(scope): ...`, `fix(scope): ...`,
  `chore(scope): ...`, `docs(scope): ...`.
- Body explains the *why*, not the *what*. The diff explains the what.

## Don't

- Don't add features the spec doesn't document.
- Don't add dependencies without discussion. The dep graph is minimal by
  design (cobra, tls-client/fhttp, stdlib).
- Don't bypass the pre-commit gate. If a hook fails, fix the root cause.
