# SDK protocol — the integrator contract

This doc is for **programs that invoke `brainapi` as a subprocess** (or wrap it
as a library client in another language). It captures the things you can rely
on, the things you must NOT assume, and the non-obvious traps that we (or
downstream integrators) have hit in practice.

If you are calling the BRAIN HTTP API directly, see [protocol.md](protocol.md)
instead — that covers BRAIN's over-the-wire behaviour, not brainapi's.

For a **machine-readable** version of everything in this doc, run
`brainapi describe`. The JSON spec it emits is what codegen tools should
target; this Markdown is for humans reading prose. The two are kept in sync
by code review.

---

## Audience

You are a downstream program (Node, Python, shell, another Go service) that
needs to run `brainapi` as a child process and consume its stdout. This doc
tells you the stable contract you can build on without reading Go source.

A worked TypeScript example lives under
[`clients/typescript/`](../clients/typescript) — it implements every convention
listed here.

---

## Channels

| Channel | Content | Format |
|---|---|---|
| stdout | One JSON envelope per invocation | UTF-8, single line OR pretty-printed (whitespace insignificant) |
| stderr | Structured logs (`slog`) | Newline-delimited; gated by `--log-level` (default `warn`) |
| exit code | Stable taxonomy (see below) | Integer in `{0, 2, 3, 4, 5, 6, 7, 8, 10}` |

**stdout is the contract**. stderr is for humans and debugging — never parse it.
The one exception: if stdout is empty and exit code ≠ 0, cobra itself bailed
(usage error before any subcommand ran) and the human-readable reason is on
stderr.

---

## Envelope

Every successful invocation emits this on stdout:

```json
{ "ok": true, "data": <endpoint-specific payload> }
```

Every failed invocation that reached the subcommand emits:

```json
{ "ok": false, "error": { "kind": "<enum>", "message": "<human>", "details": <any> } }
```

Both are valid stand-alone JSON values (no trailing newline guarantee, but
parsers don't care). `data` shape is the endpoint's response model; `details`
shape is `kind`-specific (table below).

**Both branches always exit 0 if `ok=true` and a nonzero code if `ok=false`.**
You can rely on `ok` alone, on the exit code alone, or on both — they are
guaranteed in lock-step.

### Synthesising an envelope when stdout is empty

cobra parse errors (unknown flag, missing required arg) exit 2 with **no JSON
on stdout** — the human message goes to stderr. Wrappers should fall back to:

```json
{ "ok": false, "error": { "kind": "no_output", "message": "<stderr or generic>" } }
```

so callers always get the same shape. See the bundled `clients/typescript/`
client's envelope parser for a reference.

---

## Exit codes

Stable across the entire `0.1.x` line and beyond. Adding a new code is
backwards-compatible; renumbering an existing one is a SemVer-major change.

| Code | Constant       | When                                                              | `error.kind` values                                          |
|------|----------------|-------------------------------------------------------------------|--------------------------------------------------------------|
| 0    | OK             | `data` payload produced                                           | n/a                                                          |
| 2    | USAGE          | Bad flag, missing required arg, cobra parse failure, bad caller input | `no_output` (stdout empty), `invalid_argument`          |
| 3    | RATE_LIMIT     | 429 from BRAIN, or in-process cooldown active                     | `rate_limit`, `cooldown`                                     |
| 4    | BANNED         | 403-streak gate tripped, or account not verified                  | `banned`, `not_verified`                                     |
| 5    | DRF_VALIDATION | 400 from BRAIN with DRF field-error envelope                      | `drf_validation`                                             |
| 6    | API            | Any other 4xx/5xx; also catch-all for non-categorised errors      | `api`, `error`, `not_authenticated`, `long_poll_exceeded`   |
| 7    | BUDGET         | In-process daily-budget counter is full                           | `budget`                                                     |
| 8    | NETWORK        | Transport failure (conn/TLS/DNS/proxy/read); context cancel / deadline | `context`, `network`                                    |
| 10   | PERSONA        | Login returned an inquiry envelope (dead-code safety net)         | `persona_inquiry`                                            |

### `error.kind` enum (complete)

```
api                  4xx/5xx with body. details = {status, method, url, body}
rate_limit           429. details = {status, retry_after_ms, cooldown, body}
                       body = BRAIN's 429 payload; {"detail":"THROTTLED"} ⇒ submission subsystem hung (not a routine cap)
banned               403-streak. details = {streak, reason}
not_verified         403 NOT_VERIFIED. details = {status, body}
drf_validation       400 + DRF. details = {status, url, fields: {<field>: [<msg>, ...]}}
invalid_argument     Bad caller input (empty id, missing creds, ...). details = nil   [exit 2]
persona_inquiry      Login inquiry. details = {inquiry: <id>}
budget               Local daily-budget gate full. details = nil
not_authenticated    No creds AND no usable cookie. details = nil
cooldown             In-process cooldown (concurrent-sim hint). details = nil
long_poll_exceeded   Long-poll cap reached (no terminal verdict yet). details = nil
context              ctx.Done() fired (caller cancelled, deadline exceeded)
network              Transport failure (conn/TLS/DNS/proxy/read). details = nil    [exit 8]
error                catch-all; bug if you see this in production
no_output            Synthesised by your wrapper when stdout is empty
```

**Match on `kind`, never on `message`.** `message` is `err.Error()` — composed
ad-hoc and may change without a SemVer bump. The kind taxonomy is part of the
stable contract.

The 400/DRF `details.fields` map carries field-error strings in the **session
locale** (often `zh-CN`). Match on field names, never on those strings.

---

## Input conventions

### Positional args vs flags

| Pattern | Used by |
|---|---|
| `brainapi <noun> <verb> <id>` | `alphas get <id>`, `alphas submit <id>`, `simulations wait <id>`, `users activities <kind>` |
| `brainapi <noun> <verb> --flag value` | Everything else |
| `brainapi --global-flag <noun> <verb>` | Auth, transport, output flags |

### JSON request bodies via `--json`

Two commands take a non-trivial request body: `simulations create` and
`register`. Both expose `--json <source>` where `<source>` is:

| Form | Meaning |
|---|---|
| `--json -` | Read JSON from stdin until EOF |
| `--json @path/to/file` | Read JSON from file (curl style) |
| `--json /literal/path` | Same as `@path/to/file` if `/path` is not `-` and not `@`-prefixed |

Stdin (`-`) is the idiomatic form for programmatic callers — no temp file, no
shell quoting. Example:

```ts
await execa('brainapi', ['simulations', 'create', '--json', '-'], {
  input: JSON.stringify(simRequest),
});
```

### Pagination: `--all`

List endpoints (`alphas list`, `schema data-fields`) expose `--all` to drain
every page. Without it you get one page (default `limit=50` for alphas,
`limit=200` for data-fields). With it, the SDK loops `offset += limit` until
`next === null` and returns the concatenated `results` (`count` reflects the
total).

### Decoded vs raw records: `--decode`

`users activities <kind>` returns `records.records` as an array of **positional
tuples**, not dicts — BRAIN's wire shape. Column names live in
`records.schema.properties[*].name`.

Without `--decode` you get the raw wire shape and must do the column mapping
yourself. With `--decode` the SDK does the mapping and replaces `records.records`
with an array of `{<colName>: <value>}` dicts.

**For any non-Go consumer, pass `--decode`.** Re-implementing the column
mapping in TS/Python is a 50-line exercise we already wrote once
(`pkg/brainapi/users.go::DecodeActivities`) and which we'd have to keep in sync
with every schema drift forever. The `--decode` shape is the supported wrapper
contract; the raw shape is for Go callers that want zero allocation overhead.

---

## Authentication state

### Credentials

Resolved in this order, first non-empty wins:

1. `--user` / `--pass` flag
2. `BRAINAPI_USER` / `BRAINAPI_PASS` env var
3. Cookie jar file (session reuse, no fresh login)

A command that needs auth but finds nothing in any of those slots exits 8 with
`kind=not_authenticated`.

### Cookie jar

Default path: `${UserCacheDir}/brainapi/cookies-<email>.json` (or
platform-specific equivalent). Override via `--cookie-jar /path`. The file is:

- JSON
- Atomically written (temp file + rename)
- `chmod 0600` on POSIX

Two processes sharing one jar will race on writes. Use one jar per logical
session, kept separate from any other tooling sharing the same machine.

### 401 auto-relogin

If a request returns 401 and the Client has cached credentials (set via
`Options.Email` + `Options.Password` or the equivalent flags/env), the SDK
performs one transparent re-login and retries the original request. If
credentials are not cached, 401 propagates as `kind=not_authenticated` /
exit 8.

---

## Schema gotchas (non-obvious; document changes here as they're found)

### `users activities`: `current` is **month-to-date**, not today

`ActivityStream.current` describes `{start: <month-start>, end: <today>,
value: <sum>}`. It is **not** today's count. Today's row lives in
`records[]` where `date === <today's BRAIN day>`.

The five summary windows:

| Field | Window |
|---|---|
| `yesterday` | Previous BRAIN day (single day) |
| `current` | Current month, start-of-month to today |
| `previous` | Previous month |
| `ytd` | Current year, Jan 1 to today |
| `total` | All time |

There is no `today` field. Look up `records.find(r => r.date === brainDay())`
and read its `value`.

This bit us in a doctor-style health cross-check (drift came
out as 65 because `current.value=67` was MTD not today). Worth flagging in
red.

### BRAIN day rolls at 3 AM US/Eastern

Anything keyed to "today" — daily-submit budget, activities `yesterday` /
`current`, Challenge score window — uses the BRAIN day boundary, not local
midnight. To compute today's BRAIN day string in your wrapper:

```ts
// Take America/New_York time minus 3h, format as YYYY-MM-DD
```

`challengeDayStr()` in `pkg/brainapi` is the Go reference implementation of
this offset.

### Forward-compat `json.RawMessage` fields

Five fields in our types are `json.RawMessage` because BRAIN has reshaped
them at least once without warning (and we expect more):

- `Alpha.Team` (was string, now `{id, type, name, university}`)
- `Alpha.Color`, `Alpha.Category` (string-typed metadata, defensive)
- `Competition.Team`
- `Leaderboard.University`

Plus the always-opaque ones: `Alpha.Settings`, `Alpha.Regular`, `User.Address`,
`User.Education`, `User.Employment`, `Competition.Countries`, etc.

**TS/Python wrappers should type these as `unknown` / `any`**, not as a
specific shape. Decode lazily with a second `json.Unmarshal` (Go) or
`JSON.parse` after string-coercion (rare, since they arrive as values) when
you actually need a field, and tolerate the type changing under you.

### `Retry-After` is a float string

`"5.0"` not `"5"`. If your wrapper handles 429 itself instead of letting
brainapi do it, parse with `parseFloat` (clamped to `[1, 120]` for 429,
`[0.5, 30]` for 503).

### `WaitForSimulation` terminates on `alpha != ""`, not status string

The simulation `wait` long-poll returns whenever `s.Alpha` is populated,
regardless of `s.Status`. BRAIN occasionally returns
`{status: "WARNING", alpha: "<id>"}` (e.g. reversion-component advisory) —
the sim succeeded, just with a soft flag. Don't gate on
`status === "COMPLETE"` alone; an alpha id is the success signal.

### Three different list-endpoint envelopes

- `GET /operators` → bare JSON array. `data` is `Operator[]`.
- `GET /data-fields` → `{count, results}`. No `next`/`previous`.
- `GET /users/self/alphas` → full DRF `{count, next, previous, results}`.

Don't write one paginated reader and hope it works for all three.

---

## Long-poll endpoints

These endpoints can block for up to `Options.MaxLongPolls × Retry-After`
seconds before returning. Set the **subprocess timeout** accordingly —
`timeout_ms: 300_000` (5 min) is a generous default.

| Command | Long-polls on | Default max wall-clock (60 polls × ~5s) |
|---|---|---|
| `simulations wait` | 200 + `progress < 1.0`, or empty body | ~5 min |
| `alphas check` | 200 + empty body + `Retry-After` | ~5 min |
| `alphas submit` | POST 503 → GET 200 until verdict | ~5 min |
| `alphas pnl` | 200 + empty body (cold cache) | ~5 min |

If the cap is reached, the command exits with `kind=long_poll_exceeded`
(exit code 6, the generic API exit) and the caller decides whether to retry.

---

## Version

`brainapi --version` prints:

```
brainapi version <semver> (commit <short-sha>, built <ISO8601>)
```

Build provenance is set via `-ldflags '-X .../internal/version.Version=...'`
at release time. A `dev` version means an unstamped local build —
acceptable for development but never for production wrappers.

### SemVer policy

| Change | Severity |
|---|---|
| New subcommand | minor |
| New optional flag on existing subcommand | minor |
| New field in `data` payload | minor |
| New `error.kind` value | minor (wrappers should fall through to a default) |
| Remove or rename a subcommand / flag | **major** |
| Renumber an exit code | **major** |
| Remove a field from `data` payload | **major** |
| Change a stable field's JSON type (string ↔ object) | **major** unless we converted to `json.RawMessage` |
| `error.message` wording change | none (don't match on it) |

Wrappers should pin to a minor range (`^0.1`) until 1.0, then to a major range.

---

## Reference wrapper

The bundled TypeScript client under [`clients/typescript/`](../clients/typescript)
is the canonical worked example — it wraps the CLI behind the envelope contract
above, with a typed exception hierarchy and one method per endpoint.

If you write a wrapper in another language and it makes sense to upstream it,
add a row to this section pointing at your reference impl.

---

## Feedback channel

If, while driving the SDK, you hit a defect **in the SDK itself** — a `data`
shape that diverges from `describe`, a mis-classified `error.kind` / exit code, a
stale entry in this doc, or a command/tool that errors unexpectedly — there is a
first-class way to report it upstream instead of losing the finding. This is for
the SDK, **not** for BRAIN platform questions or your own alpha/strategy work.

Two symmetric surfaces, one shared mechanism:

| Surface | Entry point |
|---|---|
| CLI | `brainapi feedback --title <one-line> --body <details> [--category bug\|docs\|enhancement\|question] [--confirm]` |
| MCP | the `report_issue` tool (always registered, independent of `--enable-writes`) |

Both render the report (plus an auto-collected environment block — SDK version,
commit, surface, OS/arch, Go version) into a GitHub issue and return the standard
envelope:

```json
{ "ok": true, "data": {
  "filed":  false,
  "mode":   "draft_url",
  "url":    "https://github.com/<repo>/issues/new?title=…&body=…",
  "note":   "no GitHub token configured; returning a draft issue URL for a human to open"
} }
```

`number` is omitted on a draft and present only on a filed (`github_api`) result.

**Two modes, picked automatically:**

- `github_api` — a token is configured **and** the caller confirmed
  (`--confirm` / `confirm:true`): the issue is filed via
  `POST /repos/{owner}/{repo}/issues`; `filed:true`, `url` is the new issue, and
  `number` is its id.
- `draft_url` — otherwise: a prefilled "new issue" URL is returned for a human to
  open. No token, no network — the safe default.

**Gating & degradation:**

- Filing opens an issue on a public tracker, so it is **outward-facing** and never
  happens implicitly — treat `--confirm` like a mutating command (show the
  dry-run, get a "yes"). The CLI requires `--confirm`; the MCP tool's `confirm`
  defaults to `false` (a draft is harmless).
- If the GitHub call itself fails, both surfaces **degrade to a draft URL** (with
  the cause in `note`) rather than erroring — the channel always yields *some* way
  to land the feedback. Success is therefore always exit `0`; a missing **or
  empty** `--title` / `--body` trips the usual `no_output` / exit 2 path.

**Configuration (env):**

| Var | Purpose |
|---|---|
| `BRAINAPI_FEEDBACK_TOKEN`, else `GITHUB_TOKEN`, else `GH_TOKEN` | token used to file via the API; absent ⇒ draft-only |
| `BRAINAPI_FEEDBACK_REPO` (`owner/repo`) | override the target repo (defaults to the SDK upstream) — set this when working from a fork |
