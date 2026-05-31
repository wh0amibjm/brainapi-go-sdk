# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Notification feed.** `Client.Messages(ctx, opts)` / `Client.MessagesAll(ctx, opts)`
  and the `brainapi messages list` command wrap `GET /users/self/messages` — the
  feed behind the BRAIN notification center
  (`platform.worldquantbrain.com/messages/notifications`). Options mirror the
  alpha list: `Type` (filter), `Order`, `Limit` (default 50), `Offset`; `--all` /
  `MessagesAll` drain every DRF page. New `Message` type
  (`id, type, title, description, dateCreated, tags, read`) and TS wrapper
  `client.listMessages`. Endpoint + schema **live-confirmed 2026-05-27**:
  `type` is a closed set of `ANNOUNCEMENT` (platform-wide, **where new-dataset
  notices live** — title `📢 Launching a new dataset …`; no dedicated type/tag,
  filter on title) and `NOTIFICATION` (per-user events, e.g. achievements);
  omitting `type` returns all types. `description` is HTML that may embed
  multi-MB base64 images — transported verbatim; strip before size-sensitive
  use. See `docs/protocol.md`.

## [0.3.0] - 2026-05-21

### Added
- **Offline self-correlation.** `brainapi.SelfCorrLocal(in)` and the
  `brainapi alphas corr-local --json <file|->` command compute self-correlation
  with NO BRAIN call — a pure-Go reimplementation of BRAIN's
  `/alphas/{id}/correlations/self` semantics (Pearson over the trailing-4y
  daily-PnL-return window, on the date-intersection of each pair; signed max,
  ranked by `|corr|`; neighbours below 30-day overlap counted as `skipped`).
  Input is `{candidate, neighbours}` where each is `{id, records:[[date,pnl]…]}`
  (same tuple shape as `alphas pnl`); the candidate's own id is excluded from
  the neighbour set. Constants (4-year window, 30-day min overlap, top-5) are
  hardcoded to match BRAIN's validated semantics. Unlike `alphas corr`, this
  works on PnL that is NOT YET a main-account alpha and serves as a drift
  cross-check / offline fallback for the server-side endpoint.

## [0.2.0] - 2026-05-19

### Added
- **Pre-submit correlation gate.** `Client.AlphaSelfCorrelation(ctx, id)` calls
  `GET /alphas/{id}/correlations/self` and returns the same `correlation` value
  that BRAIN's post-submit `SELF_CORRELATION` check uses — but BEFORE a daily
  submit slot has been burned. Gate `SubmitAlpha` on `*block.Max < 0.7` to
  prevent guaranteed-fail submissions. New CLI: `brainapi alphas corr <id>`.
  New type `SelfCorrelationBlock` embeds `RecordSetBlock` with `*float64`
  `Min`/`Max` aggregates (nil for fresh accounts with no submitted peers).
  Chrome-verified 2026-05-19; SDK contract documented in `docs/protocol.md`.
- **`brainapi describe`** — machine-readable JSON spec of the SDK protocol
  (envelope shapes, exit-code map, every subcommand's path/args/flags, the
  non-obvious schema contracts). Lets non-Go consumers codegen typed
  client wrappers; the command tree is auto-walked from cobra so it cannot
  drift from the binary's actual surface.
- **`@wh0amibjm/brainapi`** — npm-publishable TypeScript client under
  `clients/typescript/` wrapping the CLI. Same `{ok,data}` / `{ok,error}`
  envelope, typed errors, exit-code aware. Built from the `describe` spec.
- **Scheduled live-smoke CI** with 9-endpoint coverage for schema-drift
  detection (auth/users/operators/data-fields/alphas-list — read-only,
  no daily-budget consumption).

### Fixed
- **Cold-cache long-poll on `/correlations/self`.** The original Chrome
  capture was taken from a Conditional-Consultant tier account and only
  surfaced BRAIN's `503 + Retry-After` signaling. Live verification against
  a TUTORIAL-tier account on 2026-05-19 revealed BRAIN also signals
  "still computing" via `200 + empty body + Retry-After` on that tier.
  `AlphaSelfCorrelation` now opts into both `longPoll503` and
  `longPoll200Empty` so the transport retries either signal. Without the
  fix, first-call-on-fresh-alpha returned a misleading `long_poll_exceeded`
  after ~2.9s; only the second call (warm cache) succeeded. Regression
  test `TestAlphaSelfCorrelation_LongPoll200EmptyThenTerminal` pins the
  new path.
- **`RegisterInput` schema now matches BRAIN's current shape.** Live POST
  `/users` (2026-05-19) rejected the SDK's payload with `address.zip:
  Unexpected property` and `education.gradYear: Unexpected property`.
  `Education.GradYear int json:"gradYear"` is now
  `Education.GraduationYear int json:"graduationYear"`, and `Address.Zip`
  is gone. `TestRegister_HappyPath` now asserts the posted body MUST NOT
  contain `address.zip` or `education.gradYear` and MUST contain
  `education.graduationYear` — catches any future field-name regression
  without needing a live BRAIN call. **Breaking change** for callers
  constructing `Education{GradYear: 2023}` directly (rename to
  `GraduationYear`); JSON consumers via the CLI unaffected since the
  wire shape just gets closer to what BRAIN already required.

### Docs
- `docs/protocol.md` — added `/correlations/self` endpoint contract, BRAIN's
  dual signaling pattern, the 0.7 threshold, and the `/correlations/prod`
  IQC-tier 403 caveat. `sdk-protocol.md` integrator contract for non-Go
  consumers. `scripts/live-smoke/README.md` now recommends a dedicated test
  account and warns against main.

### Changed
- `chore(ci): bump golangci-lint-action v8 → v9` (Node 20 → Node 24 runner).

### Known limitations
- The SDK's `Client.VerifyEmail` / `brainapi email verify --jwt` requires
  the verification JWT, but BRAIN delivers verification emails through
  SendGrid link-tracking wrappers (`mail.alerts.worldquantbrain.com/ls/click`).
  Resolving the wrapper to the final `?token=` URL requires a US residential
  IP — SendGrid 400s requests from datacenter IPs. This is upstream behavior,
  not an SDK bug. End users clicking the email button in a browser are not
  affected. Programmatic register→verify pipelines should
  use a residential proxy for the wrapper-resolution hop, then hand the JWT
  to the SDK. Accounts are auto-approved at registration for TUTORIAL-tier
  API access without the verify click, so the SDK is fully usable without
  this step.

## [0.1.2] - 2026-05-18

### Fixed
- `WaitForSimulation` only treated `COMPLETE` / `FAIL` / `ERROR` as
  terminal, hanging forever on `WARNING` (e.g. reversion-component
  advisory) and any future status BRAIN might add. BRAIN populates
  `alpha` whenever a sim produced one regardless of the status string,
  so the wait loop now terminates on `s.Alpha != ""` (success path) or
  explicit `FAIL` / `ERROR` (failure path). Matches the long-tested
  `'alpha' in body` check; eliminates a real `long_poll_
  exceeded` we hit live on `ts_zscore(close, 20)` against a main
  account where BRAIN returned `status: "WARNING", alpha: "..."`.
  Caller-visible effect: `simulations wait` (and the `simulate-and-
  fetch` composite path that builds on it) no longer dead-locks on
  warning-only verdicts.

## [0.1.1] - 2026-05-18

### Fixed
- BRAIN silently upgraded several response fields from `string` to
  structured objects, breaking `users competitions` and `alphas list/get`
  with `json: cannot unmarshal object into Go struct field ... of type
  string`. Caught by live integration testing against an account
  with active competition entries. Switched five metadata fields to
  `json.RawMessage` so the SDK no longer dictates their shape (it never
  needed to — none had typed callers inside the repo):
  - `Alpha.Team` (BRAIN now returns `{id, type, name, university}`)
  - `Competition.Team` (same drift)
  - `Alpha.Color`, `Alpha.Category` (defensive — same `*string` metadata pattern as Team)
  - `Leaderboard.University` (defensive — peer of Team in the same struct family)

  Callers that need typed access can `json.Unmarshal(field, &dst)`.
  Mirrors the existing convention (`Alpha.Settings`, `User.Address`,
  `Competition.Countries`, etc. were already `json.RawMessage`).

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

[Unreleased]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.3.0
[0.2.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.2.0
[0.1.2]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.2
[0.1.1]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.1
[0.1.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.0
