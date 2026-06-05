# @wh0amibjm/brainapi

TypeScript client for the [`brainapi`](../..) CLI — typed wrappers around
every BRAIN endpoint, with the JSON envelope, exit-code → typed-error
plumbing, and `users activities --decode` ergonomics handled for you.

## Install

```bash
npm install @wh0amibjm/brainapi
# or pnpm add, yarn add — postinstall fetches the matching binary
```

The postinstall script downloads the platform-specific binary from
[GitHub Releases](https://github.com/wh0amibjm/brainapi-go-sdk/releases) and
verifies its SHA256 against the release's `SHA256SUMS.txt`. Set
`BRAINAPI_SKIP_DOWNLOAD=1` to skip (useful for CI / monorepo dev); the
runtime then expects `BRAINAPI_BIN` or a `binary:` constructor option.

## Quick start

```ts
import { Client } from '@wh0amibjm/brainapi';

const cl = new Client({
  user: process.env.BRAINAPI_USER!,
  pass: process.env.BRAINAPI_PASS!,
});

const info = await cl.probe();
console.log(`logged in as ${info.user.id} (${info.permissions.length} perms)`);

const self = await cl.self();
if (!self.verified || !self.approved) {
  throw new Error(`account not active: verified=${self.verified} approved=${self.approved}`);
}

const today = await cl.activities('submissions');
console.log(`yesterday: ${today.yesterday?.value ?? 0} submissions`);
```

## API surface

All methods return typed payloads from `data` and throw a typed
exception on `error`. The exception hierarchy maps exit codes 1:1:

| Exit | Class | When |
|---|---|---|
| 2 | `UsageError` | Bad flag / missing arg |
| 3 | `RateLimitError` | 429 from BRAIN, or in-process cooldown |
| 4 | `BannedError` | 403 streak, or `verified=false` |
| 5 | `DRFValidationError` | DRF 400 field-error |
| 6 | `APIError` | Other 4xx/5xx, or generic |
| 7 | `BudgetExhaustedError` | Local daily-budget gate full |
| 8 | `NetworkError` | Context cancellation / IO error |
| 10 | `PersonaInquiryError` | Login inquiry envelope |

Methods: `probe` `login` `logout` `self` `competitions` `activities`
`getAlpha` `checkAlpha` `submitAlpha` `alphaPnl` `alphaPerformance`
`alphaCorr` `alphaCorrLocal` `listAlphas` `listMessages`
`createSimulation` `getSimulation` `waitSimulation` `backtest`
`operators` `dataFields` `register` `emailReverify` `emailVerify`
`passwordForgot` `passwordReset` `describe` `version` — plus a
typed `run<T>(args, stdin?)` escape hatch for any command the wrapper
hasn't yet exposed.

`alphaCorr` / `alphaCorrLocal` are the self-correlation gate behind
`submitAlpha` (submittable when `max < 0.7`).

## Schema gotchas

The wrapper inherits brainapi's stable contract — see the parent project's
[`docs/sdk-protocol.md`](../../docs/sdk-protocol.md) for the full list.
Two that bite TS callers most often:

- `activities('submissions').current` is **month-to-date**, not today.
  Today's count lives in `records[]` keyed by `date === <today's BRAIN day>`
  (BRAIN day rolls at 3 AM US/Eastern).
- `users activities` records are tuple-arrays on the wire; the wrapper
  always passes `--decode` so you get `{colName: value}` dicts. If you call
  `run(['users', 'activities', kind])` without `--decode` via the escape
  hatch you'll get the unparsable wire shape — pass `--decode` yourself.

## Binary resolution

Priority order, first match wins:

1. `new Client({ binary: '/path/to/brainapi' })`
2. `process.env.BRAINAPI_BIN`
3. Bundled binary at `<package>/bin/brainapi[.exe]` (from postinstall)
4. `brainapi` on `$PATH`
5. Throws `Error` with actionable instructions

## License

MIT. See the [parent project](../..).
