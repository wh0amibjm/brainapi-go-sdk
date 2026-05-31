# BRAIN protocol notes

These are the BRAIN HTTP behaviors that the SDK's transport / parsers depend on. They are captured — via Chrome DevTools auditing against `platform.worldquantbrain.com` — and pinned by the response fixtures in [`testdata/`](../testdata). If anything here disagrees with a fresh live capture, the capture wins.

Source of truth: the response fixtures in [`testdata/`](../testdata), Chrome-DevTools-verified against `platform.worldquantbrain.com` (current as of 2026-05-06).

## Endpoints used

| Method | Path | SDK method |
|---|---|---|
| POST | `/authentication` | `Client.Login` |
| GET | `/authentication` | `Client.Probe` |
| DELETE | `/authentication` | `Client.Logout` |
| POST | `/authentication/persona` | `Client.CompletePersona` (dead-code safety net) |
| POST | `/users` | `Client.Register` |
| POST | `/user/email/reverify` | `Client.ReverifyEmail` |
| POST | `/user/email/verify` | `Client.VerifyEmail` |
| POST | `/user/password/forgot` | `Client.ForgotPassword` |
| POST | `/user/password/reset` | `Client.ResetPassword` |
| GET | `/captcha` | `Client.FetchCaptchaChallenge` |
| GET | `/users/self` | `Client.Self` |
| GET | `/users/self/competitions` | `Client.Competitions` |
| GET | `/users/self/alphas` | `Client.ListAlphas` / `ListAlphasAll` |
| GET | `/users/self/activities/{kind}` | `Client.Activities` |
| GET | `/users/self/messages` | `Client.Messages` / `MessagesAll` |
| GET | `/alphas/{id}` | `Client.GetAlpha` |
| GET | `/alphas/{id}/check` | `Client.CheckAlpha` |
| POST + GET | `/alphas/{id}/submit` | `Client.SubmitAlpha` |
| GET | `/alphas/{id}/recordsets/pnl` | `Client.AlphaPnL` |
| GET | `/alphas/{id}/correlations/self` | `Client.AlphaSelfCorrelation` |
| GET | `/competitions/{cid}/alphas/{id}/before-and-after-performance` | `Client.BeforeAndAfterPerformance` |
| POST | `/simulations` | `Client.CreateSimulation` |
| GET | `/simulations/{id}` | `Client.GetSimulation` / `WaitForSimulation` |
| GET | `/operators` | `Client.Operators` |
| GET | `/data-fields` | `Client.DataFields` / `DataFieldsAll` |

## Critical caveats baked into the SDK

### `Retry-After` is a **float** string

BRAIN sends `"5.0"`, not `"5"`. `strconv.Atoi` errors out and drops the hint silently if you use it. The SDK uses `strconv.ParseFloat` and clamps the result to:

| Class | Floor | Ceiling |
|---|---|---|
| 429 rate-limit | 1s | 120s |
| 503 long-poll | 0.5s | 30s |
| 5xx server error backoff | 5s | 15s |
| Network error backoff | 2s | 15s |

### SELF_CORRELATION verdict lives ONLY in `/alphas/{id}/submit`

`GET /alphas/{id}` returns `result: "PENDING"` indefinitely for UNSUBMITTED alphas. The verdict only appears via the POST + GET long-poll on `/alphas/{id}/submit`. Polling the wrong endpoint here silently drops verdicts — a subtle trap, so the SDK only reads the verdict off the submit long-poll.

`Client.SubmitAlpha` does the POST, parses status (200/201/403/503/empty-body), and long-polls GET on the same path until terminal.

### `POST /alphas/{id}/check` is **dead** (returns 405)

Migrated to GET-only some time before 2026-05-05. The TS code silently swallowed the 405 in an except branch and `/check` had been a no-op for months. The SDK uses GET only.

### `POST /alphas/{id}/submit` returns 503 to mean "queued"

503 here is **not** an error. It's BRAIN's "I've queued your submit, please poll the same URL with GET" signal. The SDK treats POST 503 as a terminal success (with empty body) and immediately starts the GET polling loop.

### `GET /alphas/{id}/correlations/self` is the **pre-submit** corr gate

The submit verdict-bearing `SELF_CORRELATION` check (§ above) only fires after you've already burned a daily submit slot. The standalone correlation endpoint lets you check the same value cheaply before calling `/submit`:

- **Long-poll:** "still computing" is signaled two ways depending on account tier — `503 + Retry-After` (observed on Conditional-Consultant tier, Chrome 2026-05-19) or `200 + empty body + Retry-After` (observed on a TUTORIAL-tier account, live SDK capture 2026-05-19). 200 with a non-empty body is terminal. The SDK sets both `longPoll503` and `longPoll200Empty` so the transport retries either signal; ~3 retries to terminal in practice. Cached server-side per alpha — re-running the same alpha returns 200 immediately on the second call.
- **Response body:** `{schema, records, min, max}`. `records` are positional tuples (top N most-correlated already-submitted alphas) per `schema.properties[*].name`: `id, name, instrumentType, region, universe, correlation, sharpe, returns, turnover, fitness, margin`. `min`/`max` are the aggregate correlation values across the record set.
- **Threshold:** BRAIN rejects submissions on `correlation >= 0.7` (per `testdata/submit_403_corr_fail.json`). Gate `SubmitAlpha` on `block.Max < 0.7` to avoid wasted `corr_rejected` verdicts and preserve daily submit budget.
- **Chrome-verified:** 2026-05-19 against `platform.worldquantbrain.com/alphas/{id}` side panel → green "refresh" icon on the "Self Correlation" row triggers this endpoint; the panel's displayed Maximum/Minimum are the body's `max`/`min` verbatim.
- **Sibling `/correlations/prod`** is 403 on the IQC consultant tier (already noted in "does NOT cover" below).

### `/users/self/activities/{kind}` records are positional tuples

The `records.records` array is `[][col0, col1, col2]`, NOT `[]{col0: ..., col1: ...}`. Field names live in `records.schema.properties[*].name`. The SDK exposes `DecodeActivities()` which converts to `map[string]json.RawMessage` keyed by column name.

There are two envelope types:

- `"DAILY"`: includes `yesterday`, `current`, `previous`, `ytd`, `total` summary blocks (used by base-payment, simulations, submissions).
- `"LIST"`: only `total`; records can have heterogeneous types (used by other-payment).

### `/users/self/messages` is the notification-center feed

This is the endpoint behind `platform.worldquantbrain.com/messages/notifications`. The SDK exposes it as `Client.Messages` (first page) and `Client.MessagesAll` (paginate all). Live-captured 2026-05-27 via the platform's own XHR (account `DH52706`).

- **Query params:** `type` (filter), `order` (e.g. `-dateCreated`), `limit` (web client uses 10), `offset`. The SDK omits any param left empty; **omitting `type` returns all types in one feed** (verified 200, 43 rows).
- **Envelope:** standard DRF `{count, next, previous, results}` (decoded as `Page[Message]`), same shape as `/users/self/alphas`.
- **`Message` fields (complete):** `id`, `type`, `title`, `description`, `dateCreated`, `tags` (`[]string`, empty in practice), `read` (`bool`).
- **`type` is a closed 2-value set** — the web UI's two tabs map 1:1 to it:
  - `ANNOUNCEMENT` — platform-wide announcements (36 of 43 on the captured account). **This is where dataset releases live**, e.g. title `📢 Launching a new dataset for IQC 2026 participants`. There is **no dedicated `type` or `tag`** for dataset updates — filter on `Title` client-side.
  - `NOTIFICATION` — per-user events (7 of 43), e.g. `Achievement Unlocked!`.
- **`description` is short in practice but may embed base64 images.** On the captured account it was rendered HTML ≤ ~2 KB with no images. BRAIN's own clients defensively strip `<img src="data:image/...;base64,…">` payloads that can run to several MB, so don't assume it's small. The SDK transports it verbatim; strip/summarize before size-sensitive sinks (LLM prompts, logs).

### Forward-compatibility: five metadata fields are `json.RawMessage`

BRAIN has a history of silently retyping metadata fields from `string` to structured objects (caught live during v0.1.1 integration testing). The SDK pre-emptively types these as `json.RawMessage` so the wire shape never breaks the parser:

| Field | First-observed reshape |
|---|---|
| `Alpha.Team` | string → `{id, type, name, university}` (2026-05-18) |
| `Competition.Team` | same drift, same date |
| `Alpha.Color` | defensive — same `*string` metadata pattern as Team |
| `Alpha.Category` | defensive — same |
| `Leaderboard.University` | defensive — peer of Team in the same struct family |

Callers that need typed access can `json.Unmarshal(field, &dst)` against whatever shape BRAIN is currently returning. Wrapper authors in other languages should type these as `unknown` / `any` and decode lazily.

### Schema endpoint shapes diverge

- `GET /operators` → bare JSON array (no envelope).
- `GET /data-fields` → `{count, results}` (no `next`/`previous`).
- `GET /users/self/alphas` → full Django REST `{count, next, previous, results}`.

The SDK uses three different decoded types: `[]Operator`, `DataFieldsPage`, `Page[Alpha]`.

### DRF validation envelope on 400

All `/users/*` and `/user/*` 400 responses use:

```json
{"<field>": ["<error message>", ...], ...}
```

Locale-aware — strings come back in the locale of the calling session (often zh-CN). The SDK exposes this as `DRFError` with `Fields map[string][]string`. **Match on field names, never error strings.**

### Altcha PoW captcha (mandatory for `POST /users`)

BRAIN migrated off reCAPTCHA v2 to Altcha-style SHA-256 PoW some time before 2026-05-17.

```
GET /captcha
  -> {algorithm: "SHA-256", challenge: <hex>, salt: <str>, signature: <str>, maxNumber: <int>}

# Find n in [0, maxNumber] s.t. hex(sha256(salt + str(n))) == challenge
# Payload = base64(JSON({...challenge, number: n, took: <ms>}))
# Inject into auxiliary.captcha on POST /users.
```

The SDK includes a parallel SHA-256 solver (`pkg/captcha/altcha`) wired by default. Median solve at `maxNumber=1_000_000` on an 8-core machine: ~10 ms parallel, ~250 ms single-threaded.

### Persona inquiry envelope (operationally dead)

Login at `POST /authentication` may return 201 with body `{"inquiry": "<id>"}` instead of `{"user": ..., "permissions": [...]}`. This indicates the persona 2FA-like challenge fired. **In current BRAIN production this is dead code** — zero matches in any rotated production log since the 2026-05-06 audit; both verified test accounts that captured login responses got `permissions:["TUTORIAL"]` directly.

The SDK keeps `Client.CompletePersona` as a safety net but does not exercise it. If BRAIN starts firing inquiries again, the spec README has the captured 400/404 negative paths to anchor a test; the 200 success body remains TBD.

### BRAIN day rolls at 3 AM US/Eastern

Both daily submit quota and competition Challenge score roll at 3 AM ET, not midnight. The SDK's `DailyBudget` counter is keyed by a `challengeDayStr()` helper that subtracts 3 hours from `America/New_York` time before formatting.

## What the SDK does NOT cover

- `GET /themes` — returns 404 for everyone now (migrated, theme data is inlined in alpha records).
- `GET /alphas/{id}/correlations/prod` — 403 for IQC consultant tier through July 2026.
- Persona inquiry success-path body — TBD pending a live capture.
- Success-path 2xx bodies for `POST /users`, `/user/email/{verify,reverify}`, `/user/password/{forgot,reset}` — production code (and this SDK) only inspects status; body shape isn't pinned in tests yet.

These tier limits were observed live against the Conditional-Consultant / TUTORIAL accounts used during protocol capture.
