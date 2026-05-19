# Live BRAIN smoke

Nine-call read-only validation against the real `api.worldquantbrain.com`. Proves the SDK's TLS impersonation plus 9-endpoint decoding still match what BRAIN returns today.

| Step | Endpoint | What it proves |
|---|---|---|
| 1/9 | `POST /authentication` (`Login`) | Cloudflare/edge accepts our JA3 + Basic auth lands a session cookie. |
| 2/9 | `GET /authentication` (`Probe`) | Session cookie is honored on subsequent calls. |
| 3/9 | `GET /operators` (`Operators`) | Bare-array decode into `[]Operator`. |
| 4/9 | `GET /users/self` (`Self`) | `User` struct still matches BRAIN's 21-key body. |
| 5/9 | `GET /users/self/competitions` (`Competitions`) | `Competition` + `Leaderboard.University` decode after the v0.1.1 string→object drift. |
| 6/9 | `GET /users/self/activities/SUBMISSION` (`Activities`) | `ActivityStream` + positional-tuple `RecordSetBlock` decode. |
| 7/9 | `GET /users/self/alphas` (`ListAlphas`) | `Page[Alpha]` + `Alpha.Team` / `Alpha.Color` / `Alpha.Category` decode. |
| 8/9 | `GET /alphas/{first}` (`GetAlpha`) | Single-alpha detail decode (skipped with WARN when the account has zero alphas — common on fresh accounts, not a failure). |
| 9/9 | `GET /data-fields` (`DataFields`) | `DataFieldsPage` + `NamedRef` decode. |

**No submits, no simulations, no register.** Daily-budget endpoints stay untouched; the registration leg has its own canary (`scripts/register`).

## Usage

```powershell
$env:BRAINAPI_USER = "secondary account@example.com"
$env:BRAINAPI_PASS = "..."
make test-live-smoke
```

```bash
BRAINAPI_USER=secondary account@example.com BRAINAPI_PASS=... make test-live-smoke
```

Both forms shell out to `go run ./scripts/live-smoke`. Credentials for the five rotated secondary accounts live in `testdata/test-accounts/accounts.json` (gitignored).

## Account selection

**Use a secondary account, NOT the main account.** Live-smoke is a canary — it runs weekly on CI and ad-hoc on demand, so quota/rate-limit pressure adds up. secondary accounts also exercise a different permission envelope (fewer perms, fewer data-fields), making the test slightly more representative of production secondary account workers. A dedicated test account is ideal; pulling one from `testdata/test-accounts/accounts.json` works too. NEVER point this at the main account — one bad week of CI and you burn through the precious daily budgets.

The script picks the deterministic browser profile for the supplied email (`ProfileForEmail`), so a fingerprint mismatch from the production bridge will surface here.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | All nine calls succeeded — SDK is production-validated for this account. |
| 1 | Some call failed — the end-of-run summary's `next:` line points at the most likely cause; the `step=` field in the structured stderr log says which call. |
| 2 | Missing `BRAINAPI_USER` or `BRAINAPI_PASS` env. |

## What this proves

- `bogdanfinn/tls-client` Chrome 131 profile clears BRAIN's edge (no 403 from Cloudflare/WAF).
- Basic-auth login lands a session cookie (proves cookie jar wiring against real `Set-Cookie` headers, not just `httptest`).
- Nine real BRAIN bodies decode into typed structs without unknown-field errors — catches schema drift if BRAIN adds a required field we don't decode, or silently retypes an existing one (the v0.1.1 class of bugs).

## What this does NOT prove

- Daily-budget gates (no submit/simulation/correlations calls).
- Captcha solver against the real `/captcha` endpoint (`register` covers this).
- Long-poll behavior on real BRAIN (no submit/check/pnl/simulation/corr call).
- secondary account permission/tier handling (depends on which account you supply).

To exercise the register leg, run `make test-register`. To exercise the pre-submit correlation gate against an existing alpha, build it ad-hoc with `brainapi alphas corr <id>`.
