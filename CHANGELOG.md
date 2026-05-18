# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-05-18

Initial public release. Requires Go 1.26+.

### Added
- Initial cross-platform Go SDK + CLI for the WorldQuant BRAIN HTTP API.
- 21 library methods covering every documented BRAIN endpoint (auth, alphas,
  simulations, users, schema, register, email, password).
- Cobra CLI mirroring each endpoint with stable `{ok,data}`/`{ok,error}` JSON
  envelope and 8 documented exit codes.
- TLS impersonation via `bogdanfinn/tls-client` with deterministic
  per-account browser-profile rotation (`MD5(email)` bucket).
- Altcha PoW captcha solver (parallel SHA-256, `runtime.NumCPU` workers) with
  pluggable `CaptchaSolver` interface and zero-import-cycle adapter.
- File-backed cookie jar with atomic save + 0o600 permissions.
- Retry policy: float-parsed `Retry-After`, 401 auto-relogin, 403 ban-counter
  with configurable threshold, 429 cooldown propagation, 503-as-queued for
  `/alphas/{id}/submit`, long-poll for `/check`, `/submit`, `/simulations`,
  `/recordsets/pnl`.
- In-process daily budget gate (sims/submits) keyed to BRAIN's 3 AM ET
  challenge-day boundary.
- Typed errors: `APIError`, `RateLimitError`, `BannedError`, `NotVerifiedError`,
  `DRFError`, `PersonaInquiryError`; sentinels `ErrNotAuthenticated`,
  `ErrDailyBudgetExhausted`, `ErrCooldown`, `ErrLongPollExceeded`.
- Pluggable `Observer` metrics interface (`ObserveRequest`, `ObserveRetry`)
  for Prometheus/OTel/structured-log instrumentation.
- Engineering scaffolding: golangci-lint v2, pre-commit hooks
  (`gofumpt`, vet, lint, mod tidy, `go test -race -short`), Makefile with
  cross-platform `clean` target, GitHub Actions for CI + release.
- Live BRAIN smoke script under `scripts/live-smoke` for manual production
  validation.
- Benchmarks: Altcha PoW solver, `Retry-After` parsing, transport hot path.
- `Client.Logout` clears the persisted cookie jar file and cached
  credentials on a successful DELETE /authentication; on DELETE failure
  local state is left intact so the caller can retry.
- `Client.ClearCredentials` API for explicit credential-cache wipe in
  long-lived services (mirrors `SetCredentials`).
- pre-push git hook backstop: `go build`, full `go test -race` (no
  `-short`), and cross-compile smoke for all five release targets.

[Unreleased]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.0
