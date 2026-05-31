# Contributing

Thanks for considering a contribution. A few ground rules.

## Development setup

```bash
git clone https://github.com/wh0amibjm/brainapi-go-sdk.git
cd brainapi-go-sdk
go mod download

# Install hooks (uses .pre-commit-config.yaml if pre-commit is installed,
# falls back to .githooks/pre-commit otherwise)
make install-hooks
```

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

Every commit must pass:

- `gofumpt -extra` (format)
- `go vet ./...`
- `golangci-lint run --timeout=3m`
- `go mod tidy`
- `go test -race -short ./...`

These run automatically via `pre-commit install` (Python framework) or
`make install-hooks` (POSIX shim).

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
