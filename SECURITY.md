# Security Policy

## Reporting a vulnerability

If you discover a security issue, please open a GitHub issue marked **security**
*or* email the maintainer privately. Do not disclose publicly until a fix has
shipped.

## Threat model

`brainapi-go` is a client SDK; it does not run a server. The threats it
defends against are:

- **Credential leakage**: the SDK never writes the email/password pair to
  disk. The cookie jar (when enabled) stores the BRAIN session cookie only,
  with `0o600` file permissions. Caller is responsible for protecting
  `BRAINAPI_USER` / `BRAINAPI_PASS` env vars.
- **Captcha replay**: every `POST /users` requires a freshly-solved Altcha
  challenge. Solutions are not cached; the SDK fetches and solves per call.
- **Cookie jar tampering**: the jar is read-only after `loadJar`; a
  malicious local user can corrupt it but cannot escalate beyond
  account-level access already granted to the user running the CLI.
- **Logging side-channels**: the SDK's `slog` logger emits structured logs
  to stderr. Passwords and JWTs are never logged. Email addresses *are*
  logged at `debug` level.

## Out of scope

- Side-channel attacks against the host machine.
- Compromise of the user's BRAIN account via phishing or social engineering.
- Vulnerabilities in `bogdanfinn/tls-client`, `bogdanfinn/fhttp`, or
  `spf13/cobra` — please report those upstream.
- The Altcha PoW protocol itself; this SDK implements the solver as
  specified.

## Supported versions

The SDK is pre-1.0. Only the latest tagged release is supported.
