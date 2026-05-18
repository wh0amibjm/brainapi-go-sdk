# Architecture

`brainapi-go` is a thin, typed Go wrapper around the WorldQuant BRAIN HTTP API. This doc explains the design choices a reader is most likely to want to challenge.

## Layering

```
+----------------------------------------------------------+
| cmd/brainapi (CLI)                                       |
|   - cobra subcommands, one per documented endpoint       |
|   - stable {ok, data} / {ok, error} JSON envelope        |
|   - error -> exit code mapping (see README)              |
+--------------------|-------------------------------------+
                     |
+--------------------v-------------------------------------+
| pkg/brainapi (library)                                   |
|   client.go    - Client struct, Options, NewClient       |
|   transport.go - tls-client driver, retry loop, hooks    |
|   retry.go     - parseRetryAfter, clamps, hints          |
|   errors.go    - typed errors (APIError, DRFError, ...)  |
|   auth.go      - Login/Probe/Logout/persona              |
|   alphas.go    - GetAlpha/CheckAlpha/SubmitAlpha/PnL/... |
|   simulations.go users.go schema.go register.go ...      |
|   observer.go  - Observer interface for metrics/tracing  |
+--------------------|-------------------------------------+
                     |
+--------------------v-------------------------------------+
| pkg/captcha/altcha (sub-package, no brainapi import)     |
|   solver.go   - parallel SHA-256 PoW                     |
|   encode.go   - base64(JSON) payload                     |
|   adapter.go  - CaptchaAdapter implements the SDK's      |
|                 CaptchaSolver structural interface       |
+----------------------------------------------------------+
```

The library has exactly **one** non-stdlib dependency cluster: `bogdanfinn/tls-client` + `bogdanfinn/fhttp` (for TLS fingerprint impersonation). The CLI adds `spf13/cobra`. That is the entire dep graph.

## Why the CaptchaSolver interface uses a callback, not `*Client`

A naive design would be:

```go
type CaptchaSolver interface { Solve(c *Client) (string, error) }
```

This creates an import cycle: `altcha` needs to call back into `brainapi.Client` to fetch `/captcha`, but the altcha package is meant to be self-contained.

The actual interface:

```go
type CaptchaSolver interface {
    Solve(ctx context.Context, fetch func(context.Context) ([]byte, error)) (string, error)
}
```

The Client passes its own `FetchCaptchaChallenge` method as `fetch`. The altcha package implements `CaptchaAdapter` with that signature — but never imports `brainapi`. Go's structural interface matching does the rest.

## Why all retry policy lives in `do()`

Each endpoint method (Login, GetAlpha, SubmitAlpha, ...) builds a `doRequest{...}` value and calls `client.do(ctx, r)`. `do()` owns:

- 401 auto-relogin (with `noAutoRelogin` escape for Login itself)
- 403 ban-counter with NOT_VERIFIED + checks-body carve-outs
- 429 with float-parsed Retry-After + cooldown-on-concurrent-hint
- 503 long-poll for `/check`, `/submit`, `/simulations`, `/recordsets/pnl`
- 5xx exponential backoff
- Network-error retry
- DRF envelope mapping on 4xx
- Observer hook invocation at every decision point

Each endpoint method just translates the response body to typed shapes. This is intentional — retry semantics are BRAIN-protocol-wide, not endpoint-specific, so they belong in one place.

The two endpoint-specific overrides are surfaced via `retryHints{accept503, longPoll503, longPoll200Empty, noAutoRelogin, maxLongPolls}`. Adding a new endpoint with novel retry behavior should add a hint flag, not branch the do() body further.

## Why the cookie jar is file-backed (not memory-only)

BRAIN sessions are long-lived (cookie `expiry` typically ~4h). A CLI run for one command shouldn't have to log in again on the next command. The jar:

- Loads at `NewClient` time (best-effort; missing file is fine)
- Saves on every successful request (small atomic-rename write)
- Uses 0o600 permissions on POSIX
- Path defaults to `${UserCacheDir}/brainapi/cookies-${email}.json`

Multi-process concurrency on the same jar is **not** safe. The README documents this.

## Why `MaxConcurrentSims` is a semaphore, not a worker pool

BRAIN's `/simulations` endpoint rate-limits per-account based on in-flight count, not requests-per-second. A semaphore models this exactly: each `CreateSimulation` call acquires a slot before dispatch, releases on completion. A worker pool would over-engineer it.

The default `MaxConcurrentSims=2` matches the production bridge's main-account setting. secondary accounts should explicitly pass `1`.

## Why daily-budget is in-process

Multi-process callers (e.g. a fleet of `brainapi alphas submit` invocations from a scheduler) need external coordination. The SDK refuses to fake it. The in-process counter exists for **single-process** programs that submit/simulate in a loop and want a safety net.

The day boundary at 3 AM ET is BRAIN's own; we mirror it via `time.LoadLocation("America/New_York")`. If TZ data isn't installed (rare on Windows), we fall back to UTC and the counter resets at UTC midnight — not perfectly aligned but never *worse* than no gate.

## Why the spec docs are upstream (not duplicated here)

`docs/protocol.md` mirrors the *current* BRAIN HTTP shapes, but the canonical truth lives at `the protocol captures under testdata/` — that's where Chrome-DevTools audits land. If a spec drifts between the two, **the the reference project copy wins**. Don't fix BRAIN protocol bugs in this repo without an upstream spec update first.

## What this design intentionally does NOT do

- **No connection pooling magic.** `tls-client` handles connection reuse; we don't second-guess it.
- **No request signing / HMAC headers.** BRAIN doesn't use them; if it ever does, that's a new feature gate.
- **No automatic Login on first call.** Caller decides when to authenticate. The auto-relogin on 401 is the ONLY implicit auth move, and it only fires if creds are cached on the Client.
- **No streaming response support.** Every BRAIN response observed is < 100 KB; full buffering simplifies retry + logging.
- **No `viper`-style config.** Options struct + env-var lookup in the CLI is enough. Adding a config-file layer multiplies the surface area for divergence between CLI and library.

## ADRs

When a non-trivial design decision changes, record it inline in this file under a new heading. The git history of `docs/architecture.md` is the ADR log.
