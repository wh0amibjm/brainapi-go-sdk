# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **`SetAlphaProperties` + `alphas set-properties <id>`** — PATCH `/alphas/{id}`
  to set an alpha's mutable PROPERTIES (`description`, `name`, `color`,
  `category`, `tags`). Feeds the pure Power Pool submit flow: a Power Pool alpha
  is not eligible until a >=100-char Idea+Rationale `description` sits in its
  Properties section (BRAIN docs: getting-started-power-pool-alphas.md L54-73),
  where the `PowerPoolSelected` tag also lives. `AlphaProperties` uses
  pointer/slice `omitempty` fields so an unset field is OMITTED from the PATCH
  body (byte-for-byte "not set => not sent"); the CLI only threads flags the user
  explicitly passed. NOT a submission — does NOT consume a `DailyBudget.Submits`
  slot. **Wire contract VERIFIED live 2026-07-02** against PATCH `/alphas/{id}`:
  `description` nests under `regular` (`{"regular":{"description":"…"}}`) — a
  TOP-LEVEL `description` is REJECTED 400 `{"description":["Unexpected property."]}`,
  so it lives on the new `AlphaRegularProperties`; `tags` is a top-level JSON
  string array (`{"tags":["PowerPoolSelected"]}`, echoed back verbatim);
  `name`/`color`/`category` are top-level scalars.
- **`AlphaPowerPoolCorrelation` + `alphas corr-power-pool <id>`** — GET
  `/alphas/{id}/correlations/power-pool`. Live-probe-confirmed (2026-07-02): same
  long-poll handshake as `/correlations/self` and the SAME body shape
  (`schema.name = "selfCorrelation"`, per-alpha records, top-level min/max — NOT
  the prod-corr histogram), so it reuses the self-corr decode path. A fresh
  Power-Pool account returns `records: []` with `max: null`; `PowerPoolCorrelationBlock.Max`
  therefore decodes as nil, and consumers MUST fail-OPEN on nil (empty pool = no
  constraint), gating on `*Max < 0.5` only when non-nil.
- **`DataCategories` + `schema data-categories`** — GET `/data-categories`,
  live-probed (2026-07-02) as a BARE JSON array of category descriptors carrying
  a category-level `valueScore` (float), a `region` array, dataset/field/alpha/user
  counts, and a `children` subcategory array. Takes no query params (global tree);
  complements `/data-sets` (which has the per-dataset pyramid multiplier).

### Changed
- **`Themes()` / `schema themes` doc corrected to reflect the probed 404.** The
  2026-07-02 live probe confirmed there is NO independent `/themes` endpoint (404).
  The theme calendar is the Learn page `themes/consgrpdefault`; the API-authoritative
  "multiplier in effect" signal is `Dataset.PyramidMultiplier` off `/data-sets`.
  The method is retained ONLY for its already-fail-open behavior (a 404 degrades to
  "themes unavailable"); the CLI short-help and type docs now say so.
- **`SimSettings.ComponentActivation` doc marks it unverified.** The OPTIONS
  `/simulations` schema (2026-07-02) does not list `componentActivation` in any
  casing, and it is absent from a REGULAR alpha's `settings`. Its real home is
  unconfirmed (possibly an alpha-level PATCH attribute, not a sim setting). The
  field stays — `omitempty` means "not set => not sent" — but must not be assumed
  honored by a SUPER sim until the first SUPER simulation verifies it.

### Fixed
- **`Check.Limit` / `Check.Value` tolerate a string scalar (no more whole-Alpha
  decode failure).** BRAIN returns a check's `limit`/`value` as a number for
  threshold checks but as a STRING for categorical ones — verified live
  2026-07-02, `HT_ORTHOGONAL_RAM_NEUTRALIZATION` reports
  `{"limit":"RAM","value":"Subindustry"}`. Because both fields were `*float64`, a
  single string scalar failed the decode of the ENTIRE `Alpha` — so `GetAlpha`,
  `ListAlphas`, AND the new `SetAlphaProperties` response (all decode an Alpha
  carrying `is.checks`) errored with `cannot unmarshal string into … Check…limit
  of type float64` for any alpha holding such a check. A new `Check.UnmarshalJSON`
  now accepts number-or-string: a number (or numeric string) parses into the
  `*float64`, a non-numeric category label leaves it nil.

## [0.8.0] - 2026-06-16

### Fixed
- **Permission-denied 403s no longer self-trip ban detection.** A 403 carrying
  the Django REST framework permission/auth envelope (`{"detail": "..."}` —
  e.g. `before-and-after-performance` for a TUTORIAL-tier account) was retried
  up to the consecutive-403 threshold and then classified as `banned`. A single
  call to a permission-gated endpoint thus mislabelled a perfectly healthy
  account as banned — and an upstream pool monitor acting on that signal could
  retire a good account. Such 403s are now terminal on the first response
  (no futile retries against an authorization wall) and never feed the ban
  streak. Only **opaque** 403s (no DRF `detail` body — edge blocks, real bans)
  still drive ban detection.

### Added
- **New error kind `permission_denied`** (exit code 6, `API`) with public
  `PermissionDeniedError{Status, Detail, Body}` type and `AsPermissionDeniedError`
  unwrap helper. `Classify` maps it with `details = {status, detail, body}`. The
  MCP error guidance and `describe` / `sdk-protocol.md` taxonomy were updated to
  list it. Agents should branch: `permission_denied` ⇒ this account lacks access
  to that endpoint (not a ban) — stop calling it, other endpoints still work.

### Changed
- **Wire-affecting:** a DRF-`detail` 403 now surfaces as kind `permission_denied`
  (exit 6) instead of `banned` (exit 4). Consumers branching on the kind/exit
  code for that response class must update — hence the MINOR bump while on 0.x.

## [0.7.0] - 2026-06-11

### Added
- **`DataField.DateCreated` — retain BRAIN's "Date added" field metadata.**
  GET /data-fields rows now carry a month-granularity `dateCreated` key (e.g.
  `"2026-03-01"`; platform announcement 2026-06-11). The struct previously
  dropped it on decode; it is now retained and passes through the CLI's
  `schema data-fields` JSON output unchanged. Decodes to `""` for responses
  that predate the metadata.

## [0.6.1] - 2026-06-11

### Changed
- **Captcha solver: removed the per-iteration heap allocation in the
  proof-of-work search.** The hot loop called `h.Sum(nil)` every iteration,
  allocating a fresh 32-byte digest each time; it now reuses a stack-local
  `[sha256.Size]byte` via `h.Sum(sum[:0])` and compares with `bytes.Equal`.
  Allocations drop from ~50k per solve to a fixed constant (~1.6 MB → <1 KB per
  op), trimming GC pressure and ~13–18% off the solve benchmarks. No behavior
  change.
- **Consolidated three duplicated CLI pagination drains** (`alphas list`,
  `messages list`, `schema data-fields --all`) into a shared generic `drainAll`
  helper. `schema data-fields` now wraps drain errors with the same `paginate:`
  prefix the other two already used; the output protocol is unchanged (an empty
  result still serializes as `"results": null`).
- Replaced the hand-rolled `joinStrings` with the standard library's
  `strings.Join` (identical behavior).
- CI: bumped `softprops/action-gh-release` v2 → v3 for the Node 24 runtime.

## [0.6.0] - 2026-06-07

### Added
- **Value-comparison filters on `alphas list` / `ListAlphas`.** `GET
  /users/self/alphas` accepts BRAIN's comparison filters, where the operator is
  embedded in the field token — `is.sharpe>=1.25`, `is.fitness>=1`,
  `is.turnover<=0.7` (operators `>`, `>=`, `<`, `<=`) — and multiple filters AND
  together. Exposed as a repeatable `--filter` flag and `ListAlphasOptions.Filters`;
  the SDK percent-encodes each fragment and appends it raw to the query
  (`url.Values` can't express a token with no `key=value` separator). The Django
  `field__gte=` form is rejected by BRAIN with HTTP 400, so the embedded-operator
  form is the supported one. Verified against the live endpoint 2026-06-07.
- **Agent feedback channel.** When an agent driving the SDK hits a defect in the
  SDK itself — a `data` shape that diverges from `describe`, a mis-classified
  `error.kind` / exit code, a stale doc, or a tool that errors unexpectedly — it
  can now report it upstream instead of losing the finding. Two symmetric
  surfaces over one shared `pkg/feedback`: a new `brainapi feedback` CLI
  subcommand and an always-on `report_issue` MCP tool (registered independent of
  `--enable-writes`). Both render the report plus an auto-collected environment
  block (SDK version/commit, surface, OS/arch, Go version) into a GitHub issue.
  Filing is outward-facing, so — like `submit_alpha` — it is gated: it only
  `POST`s when a token is configured (`BRAINAPI_FEEDBACK_TOKEN`, else
  `GITHUB_TOKEN` / `GH_TOKEN`) **and** the caller confirms (`--confirm` /
  `confirm:true`); otherwise it returns a prefilled click-to-file draft URL (no
  token, no network). A GitHub-side failure degrades to a draft URL rather than
  dropping the report. Target repo defaults to the SDK upstream; override with
  `BRAINAPI_FEEDBACK_REPO=owner/repo`. Documented in `docs/sdk-protocol.md`.
- **Agent-friendly MCP & Skill consumption.** MCP tools now return failures as a
  structured `{kind, message, details}` JSON payload (an `isError` result) so an
  agent can branch on the stable `kind` instead of parsing the message, and the
  server advertises operating instructions (auth, error-handling, safety) at
  `initialize`. The `simulations_create` / `register` tools take typed request
  bodies with a real input schema instead of an opaque JSON string. A new public
  `brainapi.Classify` is the single source of truth for the error taxonomy,
  shared by the CLI envelope and the MCP server. Added a canonical `AGENTS.md`
  entry doc and a "which integration should I use?" guide (MCP / Skill / CLI /
  library) to the README.

### Changed
- **`not_authenticated` is now consistent across every endpoint.** A 401 with no
  configured credentials surfaces `ErrNotAuthenticated` (`kind:
  not_authenticated`, exit 6) on any call, not just `auth probe` — so a
  first-time, not-logged-in caller gets the same "set `BRAINAPI_USER` /
  `BRAINAPI_PASS`" signal regardless of which command or MCP tool it runs first.
  (Previously a non-probe call returned a generic `api` 401.) Explicit `Login`
  with bad credentials still returns the `api` 401 as before.

### Fixed
- **Cookie-jar persistence is now safe across concurrent processes.** `saveJar`
  wrote to a fixed `<jar>.tmp` before its atomic rename, so two `brainapi`
  processes sharing one cookie-jar path could clobber that temp mid-write. It now
  writes to a per-write unique temp via `os.CreateTemp` before renaming, so
  concurrent writers can no longer corrupt the jar (the in-process path was
  already serialized by a mutex; this closes the cross-process gap).

## [0.5.1] - 2026-06-03

### Security
- **Bumped `golang.org/x/net` to v0.55.0**, resolving a reachable advisory
  (`GO-2026-5026`, idna) reached through the TLS client's request path. Surfaced
  by a new `govulncheck` CI gate (plus a `make vuln` target).
- **npm postinstall integrity.** The downloaded binary is now hashed in a temp
  file and atomically promoted (`renameSync`) only after SHA256 verification, so
  a mismatch can no longer leave an unverified binary at the bundled path where
  the next install's skip-guard / the runtime resolver would execute it.

### Fixed
- **Long-poll no longer consumes the error-retry budget.** The transport retry
  loop shared a single `attempt` counter between long-poll iterations and error
  retries, so once an endpoint had long-polled past `MaxRetries` (`CheckAlpha`
  caps at 30, `AlphaSelfCorrelation` at 60), the next transient 500 / 429 /
  network error mid-poll was surfaced as terminal instead of retried — the SDK's
  retry resilience silently evaporated partway through every multi-minute poll.
  Error retries and long-poll ticks now use independent counters.
- **`RetryKindUnauthorized` is now emitted on the 401 auto-relogin path** — it
  was the only retry branch the loop took without an `ObserveRetry` callback (a
  defined-but-unused constant), so metrics/tracing missed every transparent
  re-login.
- **TypeScript client: a large stdin payload plus an early child exit no longer
  crashes the host process.** `spawnCapture` now attaches a benign `child.stdin`
  error handler, so an EPIPE surfaces as the child's real exit outcome via the
  `close` event instead of an unhandled stream error. The 2s SIGKILL grace timer
  is also cleared and `unref()`'d so it no longer keeps the event loop alive
  ~2s after a timeout.

### Changed
- CI hardening: `gofumpt` is pinned (was `@latest`, a non-reproducible gate),
  and the coverage step is now a hard 78% floor on `pkg/...` instead of
  print-only.
- Removed dead code in `CompletePersona` (a no-op `errors.As` branch) and
  `WaitForSimulation` (a `parseRetryAfter("")` that always returned the zero
  default). No behavior change.
- Added regression coverage for the long-poll/error interaction, daily-budget
  day rollover (with `challengeDayStr` boundary cases and a small injectable
  clock seam), the `WaitForSimulation` long-poll cap, the `MaxConcurrentSims`
  semaphore bound and cancel-while-queued path, auto-relogin observability, and
  the submit 303 "still processing" keep-polling path.

### Docs
- Corrected the daily-budget day-boundary note in `docs/architecture.md`: the
  attribution boundary is a fixed UTC-4 midnight (04:00 UTC year-round), not the
  3 AM ET `time.LoadLocation` behavior the prose previously described.

## [0.5.0] - 2026-06-01

### Added
- **MCP server.** New `cmd/brainapi-mcp` binary exposes the SDK as a
  [Model Context Protocol](https://modelcontextprotocol.io) server over stdio,
  embedding `pkg/brainapi` directly (no subprocess-per-call). Built on the official
  `github.com/modelcontextprotocol/go-sdk` v1.6.1. Registers **20 read-only (GET)
  tools** by default; the **10 mutating tools** (`submit_alpha`, `simulations_create`,
  `register`, `login`, `logout`, `email_verify`, `email_reverify`, `password_forgot`,
  `password_reset`, `persona_complete`) are gated behind `--enable-writes`.
  `submit_alpha` is doubly gated: it runs the self-correlation gate (`max < 0.7`)
  and requires `confirm=true`, else returns a dry-run. Credentials via
  `BRAINAPI_USER` / `BRAINAPI_PASS`; logs to stderr, JSON-RPC on stdout. The
  `make release` / release workflow now cross-compiles and ships `brainapi-mcp`
  alongside `brainapi` for all five targets.

## [0.4.0] - 2026-05-31

### Added
- **Before/after performance projection.** `Client.BeforeAndAfterPerformance(ctx,
  competitionID, alphaID)` and the `brainapi alphas performance <id> --competition
  <cid>` command wrap `GET /competitions/{cid}/alphas/{id}/before-and-after-performance`
  — the "Performance Comparison" panel on the unsubmitted-alpha page. Returns the
  competition score (e.g. Delay-1) before vs after submission, plus aggregate stats,
  per-year stats, and a daily before/after PnL series — each side a before-vs-after
  pair. Cold-cache long-polled (503 / 200-empty + Retry-After) like the recordset
  endpoints; free of submit-budget cost. New types `BeforeAndAfterPerformance` and
  `PerformanceStats`. Endpoint + schema **live-confirmed 2026-05-31** via Chrome
  DevTools against an unsubmitted IQC2026S2 alpha.
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

### Fixed
- **`check` WARNING checks now fully decode.** `Check.Message` was missing, so the
  human-readable note on non-numeric checks (e.g. the `REVERSION_COMPONENT`
  WARNING — "Alpha expression includes a reversion component…") was silently
  dropped. Added the `Message` field and documented `WARNING` as a valid
  `Check.Result` value (alongside PASS/FAIL/PENDING/ERROR). Confirmed live
  2026-05-31 against an unsubmitted alpha whose `check` returned 8 PASS + 1 WARNING.

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

[0.8.0]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/wh0amibjm/brainapi-go-sdk/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.3.0
[0.2.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.2.0
[0.1.2]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.2
[0.1.1]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.1
[0.1.0]: https://github.com/wh0amibjm/brainapi-go-sdk/releases/tag/v0.1.0
