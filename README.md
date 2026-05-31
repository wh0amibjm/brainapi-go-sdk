# brainapi-go-sdk

A typed Go SDK + cross-platform CLI for the [WorldQuant BRAIN](https://platform.worldquantbrain.com) HTTP API.

- **Single-binary CLI** with stable JSON-in / JSON-out, scriptable from any language
- **Importable library** (`pkg/brainapi`) for embedding in other Go programs
- **TLS impersonation** via [`bogdanfinn/tls-client`](https://github.com/bogdanfinn/tls-client) (Chrome 131 default; Safari / Firefox profiles available)
- **Altcha PoW captcha** solver for `POST /users` (parallel SHA-256, runtime.NumCPU workers)
- **Production-grade retry policy**: float-parsed `Retry-After`, 401 auto-relogin, 403 ban-detection, 503 long-poll for submit/simulations, cooldown on concurrent-sim hints
- **No CGO**, builds for `linux/{amd64,arm64}`, `windows/amd64`, `darwin/{amd64,arm64}` from a single source tree

## Install

```bash
# Library
go get github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi

# CLI (Go 1.26+)
go install github.com/wh0amibjm/brainapi-go-sdk/cmd/brainapi@latest
```

Or grab a pre-built binary from the `bin/` directory after `make release`.

## CLI quick-start

```bash
# Set credentials once (or pass --user / --pass per command)
export BRAINAPI_USER=me@example.com
export BRAINAPI_PASS=hunter2

# Probe the live session (auto-logs in on 401)
brainapi auth probe

# Fetch an alpha
brainapi alphas get qMPjAxnO

# Pre-submit correlation gate (free of submit-budget cost; threshold 0.7)
brainapi alphas corr qMPjAxnO | jq '.data.max'

# Submit and wait for verdict (long-polls until terminal)
brainapi alphas submit qMPjAxnO | jq .data

# Enumerate the operator catalog
brainapi schema operators | jq 'length'

# Tier-required data-fields query (4 params are mandatory)
brainapi schema data-fields --region USA --universe TOP3000 --delay 1

# Drain every active alpha across all pages
brainapi alphas list --status ACTIVE --all | jq '.data.count'

# Get yesterday's submit count
brainapi users activities submissions --decode | jq '.data.yesterday.value'
```

## Library quick-start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "runtime"

    "github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
    "github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

func main() {
    cl, err := brainapi.NewClient(brainapi.Options{
        Email:         "me@example.com",
        Password:      "hunter2",
        Profile:       brainapi.ProfileChrome131,
        CookieJarPath: "data/cookies.json",
        CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    info, err := cl.Probe(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("logged in as %s (%v)\n", info.User.ID, info.Permissions)
}
```

## Integrating from another language

If you're calling `brainapi` as a subprocess from Node / Python / shell rather
than embedding `pkg/brainapi` in a Go program, read
[**`docs/sdk-protocol.md`**](docs/sdk-protocol.md) first. It defines the
stable JSON envelope, the exit-code → `error.kind` map, the stdin / `--decode`
conventions, and the non-obvious schema traps (`activities.current` is
month-to-date not today; BRAIN day rolls at 3 AM ET; etc.).

The bundled TypeScript client under [`clients/typescript/`](clients/typescript) is
the reference wrapper — see its envelope parser, typed-exception hierarchy, and execa plumbing.

## Endpoint coverage

Every documented BRAIN endpoint is wrapped 1:1 as both a library method and a CLI subcommand. The **Example** column links to the canonical response fixture in `testdata/` — wrappers can use these to bootstrap typed models without reading Go source.

| Endpoint | Library method | CLI command | Example |
|---|---|---|---|
| `POST /authentication` | `Client.Login` | `auth login` | [201 normal](testdata/auth_login_201_normal.json) / [persona](testdata/auth_login_201_persona.json) / [401 invalid](testdata/auth_401_invalid.json) |
| `GET /authentication` | `Client.Probe` | `auth probe` | [secondary account](testdata/auth_get_secondary.json) / [401 no-creds](testdata/auth_401_no_creds.json) |
| `DELETE /authentication` | `Client.Logout` | `auth logout` | _(204, no body)_ |
| `POST /authentication/persona` | `Client.CompletePersona` | _(dead-code safety net)_ | — |
| `GET /alphas/{id}` | `Client.GetAlpha` | `alphas get` | [alpha](testdata/alpha_detail.json) |
| `GET /alphas/{id}/check` | `Client.CheckAlpha` | `alphas check` | [terminal](testdata/check_alpha_terminal.json) |
| `POST + GET /alphas/{id}/submit` | `Client.SubmitAlpha` | `alphas submit` | [200 pending](testdata/submit_200_pending.json) / [403 corr-fail](testdata/submit_403_corr_fail.json) |
| `GET /alphas/{id}/recordsets/pnl` | `Client.AlphaPnL` | `alphas pnl` | [pnl](testdata/recordsets_pnl.json) |
| `GET /users/self/alphas` | `Client.ListAlphas` / `ListAlphasAll` | `alphas list [--all]` | [page](testdata/users_alphas_page.json) |
| `POST /simulations` | `Client.CreateSimulation` | `simulations create` | _(returns 201 with `Location` header only)_ |
| `GET /simulations/{id}` | `Client.GetSimulation` / `WaitForSimulation` | `simulations get` / `wait` | [in-progress](testdata/simulation_in_progress.json) / [complete](testdata/simulation_complete.json) |
| `GET /users/self` | `Client.Self` | `users self` | [self](testdata/users_self.json) |
| `GET /users/self/competitions` | `Client.Competitions` | `users competitions` | [competitions](testdata/competitions.json) |
| `GET /users/self/activities/{kind}` | `Client.Activities` + `DecodeActivities` | `users activities` | [simulations (DAILY)](testdata/activities_simulations.json) / [other-payment empty (LIST)](testdata/activities_other_payment_empty.json) |
| `GET /users/self/messages` | `Client.Messages` / `MessagesAll` | `messages list [--all]` | [page](testdata/messages.json) |
| `GET /operators` | `Client.Operators` | `schema operators` | [operators](testdata/operators.json) |
| `GET /data-fields` | `Client.DataFields` / `DataFieldsAll` | `schema data-fields [--all]` | [page](testdata/data_fields_page.json) |
| `POST /users` | `Client.Register` | `register` | _(register success path varies — see captcha leg)_ |
| `POST /user/email/reverify` | `Client.ReverifyEmail` | `email reverify` | _(2xx status, body shape unstable)_ |
| `POST /user/email/verify` | `Client.VerifyEmail` | `email verify` | _(2xx status, body shape unstable)_ |
| `POST /user/password/forgot` | `Client.ForgotPassword` | `password forgot` | _(2xx status, body shape unstable)_ |
| `POST /user/password/reset` | `Client.ResetPassword` | `password reset` | _(2xx status, body shape unstable)_ |
| `GET /captcha` | `Client.FetchCaptchaChallenge` | _(used internally by Register)_ | [challenge](testdata/captcha_challenge.json) |

Generic error-envelope examples: [DRF 400](testdata/drf_validation_400.json) for `kind=drf_validation` shape.

## Configuration

| Flag | Env | Default | Notes |
|---|---|---|---|
| `--base-url` | `BRAINAPI_BASE_URL` | `https://api.worldquantbrain.com` | Override for staging / proxy |
| `--user` | `BRAINAPI_USER` | _(none)_ | Required for auth and any endpoint behind login |
| `--pass` | `BRAINAPI_PASS` | _(none)_ | Auto-relogin needs this cached on the Client |
| `--profile` | _(none)_ | `chrome131` | TLS impersonation; `auto:<email>` rotates by hash |
| `--proxy` | `BRAINAPI_PROXY` | _(none)_ | `http://`, `https://`, or `socks5://` |
| `--cookie-jar` | _(none)_ | `${UserCacheDir}/brainapi/cookies-<email>.json` | File-backed jar with atomic save |
| `--timeout` | _(none)_ | `15s` | Per-request HTTP timeout |
| `--log-level` | _(none)_ | `warn` | `error\|warn\|info\|debug` — log on stderr, JSON on stdout |

## Exit codes (CLI)

Designed for shell pipelines. Every code is stable across SDK releases.

| Code | Meaning |
|---|---|
| 0 | Success |
| 2 | Usage error (bad flag, missing required arg) |
| 3 | Rate-limited or cooldown |
| 4 | Account banned or unverified |
| 5 | DRF field-validation failure (e.g. registration body missing fields) |
| 6 | Generic API error (4xx without DRF envelope, or 5xx after retries) |
| 7 | Daily budget exhausted (in-process gate) |
| 8 | Network / transport error |
| 10 | Persona 2FA inquiry envelope returned by login (rare; dead-code in current BRAIN production) |

## BRAIN protocol gotchas (mirrored in code)

These caveats — captured by Chrome DevTools auditing against `platform.worldquantbrain.com` — are baked into the SDK so callers never have to think about them:

- **`Retry-After` is a float-second string** (`"5.0"`), not an int. Parsed via `strconv.ParseFloat`; clamped to `[1s, 120s]` for 429 and `[0.5s, 30s]` for 503 long-polls.
- **`POST /alphas/{id}/check` returns 405** — GET-only, long-polls on 200 + empty body + Retry-After.
- **SELF_CORRELATION verdict only comes from `/alphas/{id}/submit` long-poll**, never from `GET /alphas/{id}` (which stays `result: "PENDING"` indefinitely for unsubmitted alphas).
- **`GET /alphas/{id}/correlations/self` is the pre-submit corr gate** and signals "still computing" two ways depending on account tier: `503 + Retry-After` (Conditional-Consultant) or `200 + empty body + Retry-After` (TUTORIAL). The SDK opts into both `longPoll503` and `longPoll200Empty` so the transport retries either signal — without this dual path, fresh-alpha first calls misleadingly return `long_poll_exceeded`. Gate `SubmitAlpha` on `*block.Max < 0.7` (same threshold BRAIN's post-submit `SELF_CORRELATION` check uses) to avoid wasting daily submit slots on guaranteed-fail submissions.
- **`POST /alphas/{id}/submit` returns 503** for the *acceptance*, not the failure — the SDK treats it as "queued" and proceeds to GET-poll.
- **`POST /alphas/{id}/submit` on an already in-flight submission returns a `303`** back to the same URL (BRAIN's "still processing, poll again" signal) whose `Location` carries an `http://` scheme (+ `:443`) the HTTP/2 transport rejects (`http2: unsupported scheme`). The SDK disables redirect-following and treats the 3xx as a keep-polling tick, so re-submits / poll-resumes don't crash.
- **A `429` with body `{"detail":"THROTTLED"}`** means BRAIN's submission/correlation subsystem is hung — in-flight `SELF_CORRELATION` never completes, so the ~3 concurrent slots never free and every submit 429s — distinct from a routine rate limit. The SDK surfaces the 429 body under the `rate_limit` error's `details.body`, so callers can tell "platform is down, retry later" from a per-request cap.
- **`GET /operators`** returns a bare JSON array; **`GET /data-fields`** uses `{count, results}` (no next/previous); **`GET /users/self/alphas`** uses the full Django REST envelope. Three different shapes — handled per endpoint.
- **Activity `records.records` are positional tuples**, not dicts. Use `DecodeActivities` to convert via the `schema.properties` column map.
- **Persona inquiry envelope** at login (`{"inquiry":"..."}`) is operationally dead-code in current BRAIN production, but kept as a safety net per `docs/brain-api-spec/authentication.md`.
- **Altcha PoW captcha** is mandatory for `POST /users`. SDK fetches `/captcha`, parallel-solves SHA-256 brute-force across `runtime.NumCPU()` workers (typically <60ms), and injects the base64 payload into `auxiliary.captcha`.
- **BRAIN day rolls at 3 AM US/Eastern**, not midnight. In-process daily-budget counter is keyed accordingly.
- **secondary account 403 ban detection** counts consecutive 403s after a re-login attempt. Main accounts disable the gate by setting `BanThreshold: 0`.

## Build

```bash
make all          # lint, test, build for the current platform
make release      # cross-compile all five (linux x{amd64,arm64}, windows amd64, darwin x{amd64,arm64})
make cover        # coverage report
make install-hooks  # git pre-commit via .githooks/
pre-commit run --all-files   # equivalent, using the pre-commit framework
```

Single-source-tree, no CGO, no platform-specific build tags — the same `./cmd/brainapi` package compiles cleanly for every target.

## Engineering standards

- **Lint**: `golangci-lint` with 15 enabled linters including `errorlint`, `bodyclose`, `gocritic`, `gofumpt`.
- **Pre-commit hooks**: `gofumpt`, `go vet`, `golangci-lint`, `go mod tidy`, `go test -race -short` — all run before every commit.
- **Test coverage**: every endpoint method has a unit test driven by `httptest.Server` replaying real BRAIN-captured payloads from `testdata/`. Retry-policy tests cover float-`Retry-After`, ban-after-streak, cooldown, DRF envelope decoding, long-poll cap.
- **Integration tests**: gated by `BRAINAPI_INTEGRATION=1` env so CI doesn't accidentally hit production.

## Protocol truth

The endpoint shapes encoded in `pkg/brainapi/types.go` mirror the Chrome-DevTools-verified response captures pinned under [`testdata/`](testdata) and documented in [`docs/protocol.md`](docs/protocol.md). When BRAIN's protocol changes, re-capture the live shape, update the fixture and protocol notes, then port the change into the typed structs.

## License

MIT — see [`LICENSE`](LICENSE).
