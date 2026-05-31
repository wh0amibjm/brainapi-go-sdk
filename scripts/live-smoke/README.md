# Live BRAIN smoke

Fourteen-call read-only validation against the real `api.worldquantbrain.com`. Proves the SDK's TLS impersonation plus 14-endpoint decoding still match what BRAIN returns today.

| Step | Endpoint | What it proves |
|---|---|---|
| 1/14 | `POST /authentication` (`Login`) | Cloudflare/edge accepts our JA3 + Basic auth lands a session cookie. |
| 2/14 | `GET /authentication` (`Probe`) | Session cookie is honored on subsequent calls. |
| 3/14 | `GET /operators` (`Operators`) | Bare-array decode into `[]Operator`. |
| 4/14 | `GET /users/self` (`Self`) | `User` struct still matches BRAIN's 21-key body. |
| 5/14 | `GET /users/self/competitions` (`Competitions`) | `Competition` + `Leaderboard.University` decode after the v0.1.1 string→object drift. |
| 6/14 | `GET /users/self/activities/SUBMISSION` (`Activities`) | `ActivityStream` + positional-tuple `RecordSetBlock` decode. |
| 7/14 | `GET /users/self/alphas` (`ListAlphas`) | `Page[Alpha]` + `Alpha.Team` / `Alpha.Color` / `Alpha.Category` decode. |
| 8/14 | `GET /alphas/{first}` (`GetAlpha`) | Single-alpha detail decode (steps 8-11 skipped with WARN when the account has zero alphas — common on fresh accounts, not a failure). |
| 9/14 | `GET /alphas/{first}/check` (`CheckAlpha`) | `IsBlock` decode of the pre-submit deterministic gates. |
| 10/14 | `GET /alphas/{first}/recordsets/pnl` (`AlphaPnL`) | `PnLSeries` positional-tuple decode. |
| 11/14 | `GET /alphas/{first}/correlations/self` (`AlphaSelfCorrelation`) | `SelfCorrelationBlock` — the pre-submit corr gate. |
| 12/14 | `GET /data-fields` (`DataFields`) | `DataFieldsPage` + `NamedRef` decode. |
| 13/14 | `GET /users/self/messages` (`Messages`) | `Page[Message]` notification feed (`type`/`tags`/`read`), where new-dataset announcements surface. |
| 14/14 | `POST /authentication/logout` (`Logout`) | Session-teardown path didn't regress. |

**No submits, no simulations, no register.** Daily-budget endpoints stay untouched.

## Usage

```powershell
$env:BRAINAPI_USER = "test-account@example.com"
$env:BRAINAPI_PASS = "..."
make test-live-smoke
```

```bash
BRAINAPI_USER=test-account@example.com BRAINAPI_PASS=... make test-live-smoke
```

Both forms shell out to `go run ./scripts/live-smoke`. Supply credentials for a dedicated test account via the env vars above.

## Account selection

**Use a dedicated test account, NOT the main account.** Live-smoke is a canary — it runs weekly on CI and ad-hoc on demand, so quota/rate-limit pressure adds up. A separate account also exercises a different permission envelope (fewer perms, fewer data-fields), making the test slightly more representative. NEVER point this at the main account — one bad week of CI and you burn through the precious daily budgets.

The script picks the deterministic browser profile for the supplied email (`ProfileForEmail`), so a profile/fingerprint regression will surface here.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | All fourteen calls succeeded — SDK is production-validated for this account. |
| 1 | Some call failed — the end-of-run summary's `next:` line points at the most likely cause; the `step=` field in the structured stderr log says which call. |
| 2 | Missing `BRAINAPI_USER` or `BRAINAPI_PASS` env. |

## What this proves

- `bogdanfinn/tls-client` Chrome 131 profile clears BRAIN's edge (no 403 from Cloudflare/WAF).
- Basic-auth login lands a session cookie (proves cookie jar wiring against real `Set-Cookie` headers, not just `httptest`).
- Fourteen real BRAIN bodies decode into typed structs without unknown-field errors — catches schema drift if BRAIN adds a required field we don't decode, or silently retypes an existing one (the v0.1.1 class of bugs).

## What this does NOT prove

- Daily-budget gates (no submit/simulation/correlations calls).
- Captcha solver against the real `/captcha` endpoint (unit-tested only).
- Long-poll behavior on real BRAIN (no submit/check/pnl/simulation/corr call).
- Account permission/tier handling (depends on which account you supply).

To exercise the register leg, use `brainapi register --json <file>`. To exercise the pre-submit correlation gate against an existing alpha, build it ad-hoc with `brainapi alphas corr <id>`.
