# AGENTS.md

Guidance for AI agents working in this repository. (Humans: start with
[`README.md`](README.md) and [`CONTRIBUTING.md`](CONTRIBUTING.md).)

`brainapi-go-sdk` is a typed Go SDK + cross-platform CLI for the WorldQuant BRAIN
HTTP API, with an MCP server and an Agent Skill layered on top.

## Build, test, lint

```bash
go build ./...                 # compile everything (no CGO)
make build build-mcp           # the brainapi CLI + brainapi-mcp binaries → bin/
go test -race -short ./...     # the pre-push / CI test command
make lint                      # golangci-lint v2.x
```

Requires Go 1.26.1+. `make lint` / `make fmt` need `golangci-lint` v2.x and
`gofumpt`; the commit hooks need the `pre-commit` framework. See
[CONTRIBUTING.md](CONTRIBUTING.md#prerequisites).

## Conventions (enforced — don't bypass)

- **The protocol is upstream.** `pkg/brainapi/types.go` mirrors the
  Chrome-verified HTTP shapes in [`docs/protocol.md`](docs/protocol.md). A
  protocol change must, in order: update the doc, add a `testdata/` fixture,
  update the struct, add/Update the test. Changes not anchored to a captured
  payload are rejected.
- **Format / lint / test before committing.** `gofumpt -extra`, `go vet`,
  `golangci-lint`, `go mod tidy` run on every commit; the `-race` suite runs on
  push and in CI.
- **Conventional Commits** for subjects (`feat(scope): …`, `fix(scope): …`).
- **No new dependencies** without discussion — the dep graph is intentionally minimal.
- Every public method needs at least one test; retry-policy changes need a test
  asserting the behavior under a representative status code.

## Using this SDK from an agent (not editing it)

This repo is built to be consumed by agents. Pick a mode (full table in the
[README](README.md#which-integration-should-i-use)):

- **MCP server** — [`cmd/brainapi-mcp`](cmd/brainapi-mcp) over stdio. Read-only by
  default; the 10 mutating tools need `--enable-writes`. Tool errors come back as
  structured `{kind, message, details}` JSON — branch on `kind`
  (`rate_limit`, `budget`, `banned`, `drf_validation`, …).
- **Agent Skill** — [`clients/skill/`](clients/skill) (`make install-skill`), for
  Claude Code and other Claude-family agents.
- **CLI as a subprocess** — a stable `{ok,data}` / `{ok,error}` JSON envelope and
  documented exit codes. Run `brainapi describe` for a machine-readable spec, and
  read [`docs/sdk-protocol.md`](docs/sdk-protocol.md).
- **Library** — `import "github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"`.

### Safety rules for any mode

- Reads are safe. `submit` / `register` / `login` / `password_*` mutate scarce or
  near-irreversible state — confirm before running, and never call them in a loop.
- `submit` consumes a scarce daily slot; pass the self-correlation gate
  (`max < 0.7`) first. The MCP `submit_alpha` enforces this and also requires
  `confirm=true`.
- Credentials come from the `BRAINAPI_USER` / `BRAINAPI_PASS` environment
  variables — never hardcode them or commit them.
