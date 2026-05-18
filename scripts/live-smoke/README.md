# Live BRAIN smoke

Three-call validation against the real `api.worldquantbrain.com`:

1. `POST /authentication` — proves the TLS/JA3 fingerprint clears BRAIN's edge.
2. `GET /authentication` — confirms the session cookie is honored.
3. `GET /operators` — confirms a real BRAIN GET decodes into typed structs.

That's it. **No submits, no simulations** — those consume daily budget against the account.

## Usage

```powershell
$env:BRAINAPI_USER = "you@example.com"
$env:BRAINAPI_PASS = "..."
go run ./scripts/live-smoke
```

```bash
BRAINAPI_USER=you@example.com BRAINAPI_PASS=... go run ./scripts/live-smoke
```

Exit codes:

| Code | Meaning |
|---|---|
| 0 | All three calls succeeded — SDK is production-validated for this account. |
| 1 | Some call failed — read the structured log on stderr; the `step=` field says which one. |
| 2 | Missing `BRAINAPI_USER` or `BRAINAPI_PASS` env. |

## What this proves

- `bogdanfinn/tls-client` Chrome 131 profile clears BRAIN's edge (no 403 from Cloudflare/WAF).
- The Basic-auth login flow lands a session cookie (proves cookie jar wiring works against real Set-Cookie headers, not just `httptest`).
- A real `/operators` body decodes into the typed `[]Operator` slice without hitting unknown-field issues — this catches schema drift if BRAIN adds a required field we don't decode.

## What this does NOT prove

- Daily-budget gates (no submit/simulation calls).
- Captcha solver against the real `/captcha` endpoint (no registration call).
- Long-poll behavior on real BRAIN (no submit/check/pnl/simulation call).
- secondary account permission/tier handling (depends on which account you supply).

To extend coverage, add explicit per-test sub-commands and gate them on a `--allow-budget-burn` flag that the operator must pass intentionally.
