---
name: brainapi
description: Drive the WorldQuant BRAIN API via the `brainapi` CLI — fetch/list/check alphas, gate self-correlation, submit, run simulations, query schema/operators/data-fields. Use when the user works with WorldQuant BRAIN alphas, simulations, data-fields, or the brainapi CLI/SDK.
allowed-tools: Read, Bash
---

This skill drives the [`brainapi`](https://github.com/wh0amibjm/brainapi-go-sdk)
CLI — a single-binary client for the WorldQuant BRAIN HTTP API that emits a
stable JSON envelope on stdout and a stable exit code per outcome.

Your job is to map the user's intent onto the right command, **respect the
safety model below**, and interpret the envelope + exit code correctly. Do not
guess flags or response shapes — the CLI is self-describing (see below).

## Setup (check once per session)

- **Binary**: assume `brainapi` is on `PATH`. If not, the user may set
  `BRAINAPI_BIN` to its path (e.g. a vendored `./vendor/brainapi`). Probe with
  `brainapi version`.
- **Auth**: credentials come from `BRAINAPI_USER` / `BRAINAPI_PASS` (env) or
  `--user` / `--pass` flags. The CLI auto-logs-in on 401 and persists a cookie
  jar. **Never echo the password or any JWT** — not in logs, not in summaries.
- If `brainapi version` or `brainapi auth probe` fails, stop and report it;
  don't proceed to other commands.

## Discover the surface — don't hardcode it

Run **`brainapi describe`** to get the authoritative, version-pinned contract:
`commands` (every command's path / endpoint / args), `exitCodes`, `errorKinds`,
and `nonObviousContracts`. When unsure of a command, args, or a response field,
run `describe` or `brainapi <cmd> --help` instead of guessing. This keeps the
skill correct as the SDK evolves.

## Output contract — react to the exit code, not just the text

Every command prints `{"ok":true,"data":...}` or
`{"ok":false,"error":{"kind","message","details"}}`. The **exit code** is the
machine signal — branch on it:

| Code | Meaning | What you do |
|---|---|---|
| 0 | success | parse `.data` |
| 2 | usage error | fix the args; don't retry verbatim |
| 3 | rate-limited / cooldown | back off; do not hammer |
| 4 | account banned / unverified | **stop**, report to user |
| 5 | DRF field-validation failure | surface which field; fix input |
| 6 | generic API error (incl. `long_poll_exceeded`, `not_authenticated`) | report; for long-poll, suggest retry |
| 7 | daily submit budget exhausted | **stop submitting**, report |
| 8 | network / transport | transient — a single retry is reasonable |
| 10 | persona 2FA inquiry (rare) | report; needs manual handling |

## Safety model — this is the whole point of the skill

**Read-only — run freely to answer questions:**
`auth probe`, `alphas get|list|check|corr|corr-local|pnl|performance`,
`schema operators|data-fields`, `users self|competitions|activities`,
`messages list`, `simulations get|wait`, `describe`, `version`.

**Mutating / scarce / account-changing — NEVER run without explicit user
confirmation in this turn, and NEVER loop or auto-retry:**
- `alphas submit` — burns a **scarce daily submit slot**; near-irreversible.
- `simulations create` — consumes simulation quota.
- `register`, `email verify|reverify`, `password forgot|reset` — create an
  account or change account state.

Rules:
1. Before any `alphas submit`, **always** pass the self-correlation gate first
   (`alphas corr`, require `max < 0.7`) — submitting a guaranteed-fail alpha
   wastes a slot.
2. For `alphas submit`, prefer the bundled gated helper (next section) over a
   raw `alphas submit`. Show the user the dry-run summary and get a clear "yes"
   before the confirmed run.
3. If the user asks to "submit everything" or to loop, **push back** — submit
   slots are limited; confirm each one or batch only what the daily budget allows.

## Workflows (judgment-dense — encode these)

**Is this alpha submittable?** (read-only)
`alphas check <id>` for the pre-submit validations, then `alphas corr <id>` and
read `.data.max`. Submittable ≈ checks PASS and `max < 0.7`. Report both; do not
submit.

**Safe submit** (mutating — gated):
Use the bundled helper, which does corr-gate → budget context → dry-run, and
only submits with `--confirm`:
```bash
# dry-run (read-only corr gate + context, no submit):
"<skill-dir>/scripts/safe-submit.sh" <alpha-id>
# after the user confirms:
"<skill-dir>/scripts/safe-submit.sh" <alpha-id> --confirm
```
`<skill-dir>` is the directory this SKILL.md was loaded from. The helper honours
`BRAINAPI_BIN`.

**Run a simulation** (mutating — confirm): `simulations create` returns a
`Location` only; then `simulations wait` long-polls to terminal.

**Schema / data-fields** (read-only): `schema operators`;
`schema data-fields --region USA --universe TOP3000 --delay 1` (those four
params are mandatory; add `--all` to drain pages).

**Found a bug in the SDK?** (feedback channel): if a command returns a shape
that contradicts `describe`, mis-classifies an error/exit code, or a doc is
stale, report it upstream with `brainapi feedback`:
```bash
# dry-run — prints a click-to-file GitHub draft URL, no network, no token:
brainapi feedback --title "<one-line>" --body "<what you did / expected / saw>"
# only with a token (BRAINAPI_FEEDBACK_TOKEN/GITHUB_TOKEN/GH_TOKEN) AND user confirmation:
brainapi feedback --title "…" --body "…" --confirm
```
This is for defects in the **SDK itself**, not BRAIN platform questions or the
user's alpha work. `--confirm` opens a public GitHub issue (outward-facing) —
treat it like the mutating commands: show the user the dry-run first, get a "yes".

## BRAIN protocol gotchas (baked into the SDK, but know them)

- **`alphas corr` is the pre-submit gate**; gate `submit` on `.data.max < 0.7`.
  Fresh alphas may long-poll (empty body / 503) while corr computes — that's
  normal; retry, don't treat as failure.
- **`alphas check` is GET-only and long-polls** (200 + empty + Retry-After).
- A **`429` with `{"detail":"THROTTLED"}`** (in `error.details.body`) means
  BRAIN's submit/corr subsystem is hung platform-wide — *not* a per-request cap.
  Tell the user to retry later; don't keep retrying.
- **`activities.current` is month-to-date, not today**; the BRAIN day rolls at
  **3 AM US/Eastern**, not midnight. Account for this when reasoning about
  "today's" counts.
- `operators` returns a bare array; `data-fields` returns `{count,results}`;
  `users/self/alphas` uses the full Django REST envelope — three shapes.

## Don't

- Don't submit, register, create simulations, or reset passwords without an
  explicit, in-turn "yes" from the user.
- Don't retry a `4` (banned) or `7` (budget) — stop and report.
- Don't print credentials or JWTs.
- Don't invent flags or response fields — `describe` / `--help` are authoritative.
