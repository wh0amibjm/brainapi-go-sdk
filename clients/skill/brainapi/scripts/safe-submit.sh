#!/usr/bin/env bash
# safe-submit.sh — gated BRAIN alpha submission.
#
# Encodes the corr-gate -> budget-context -> confirm -> submit flow as one
# command so an agent doesn't orchestrate a fragile multi-step sequence (and
# can't accidentally burn a scarce daily submit slot).
#
# Usage:
#   safe-submit.sh <alpha-id>            # DRY-RUN: read-only corr gate + context, then STOP
#   safe-submit.sh <alpha-id> --confirm  # after the gate passes, actually submit
#
# Env: BRAINAPI_BIN (default: brainapi); BRAINAPI_USER/BRAINAPI_PASS for auth.
#
# Exit: 0 = dry-run gate passed OR submit succeeded; 2 = usage/precondition;
#       3 = corr REJECT (not submitted); otherwise the brainapi submit exit code.
set -euo pipefail

BIN="${BRAINAPI_BIN:-brainapi}"
THRESHOLD=0.7

die() { echo "safe-submit: $*" >&2; exit 2; }

[ $# -ge 1 ] || die "usage: safe-submit.sh <alpha-id> [--confirm]"
ALPHA="$1"; shift
CONFIRM=0
[ "${1:-}" = "--confirm" ] && CONFIRM=1

command -v "$BIN" >/dev/null 2>&1 || die "brainapi not found (set BRAINAPI_BIN or add to PATH)"
command -v jq    >/dev/null 2>&1 || die "jq is required"

# 1. Read-only self-correlation gate (costs no submit budget).
echo "==> corr gate: $BIN alphas corr $ALPHA" >&2
corr_out=$("$BIN" alphas corr "$ALPHA" 2>/dev/null) || {
  code=$?
  die "corr check failed (exit $code) — still computing, or auth/network. Do NOT submit blindly; retry later."
}
max=$(printf '%s' "$corr_out" | jq -r '.data.max // empty')
[ -n "$max" ] || die "corr response had no .data.max — refusing to submit on unknown correlation."
echo "    self-corr max = $max (threshold $THRESHOLD)" >&2
if awk -v m="$max" -v t="$THRESHOLD" 'BEGIN{exit !(m+0 >= t+0)}'; then
  echo "REJECT: max $max >= $THRESHOLD — would fail SELF_CORRELATION. Not submitting (slot saved)." >&2
  exit 3
fi

# 2. Submission context (informational — BRAIN exposes no clean "remaining today").
echo "==> context: $BIN users activities submissions --decode" >&2
"$BIN" users activities submissions --decode 2>/dev/null \
  | jq -r '.data | "    submitted: yesterday=\(.yesterday.value // "?") month-to-date=\(.current.value // "?")"' >&2 \
  || echo "    (submission activity unavailable)" >&2

# 3. Gate on explicit confirmation.
if [ "$CONFIRM" -eq 0 ]; then
  echo "DRY-RUN: corr gate PASSED (max $max < $THRESHOLD). Re-run with --confirm to submit $ALPHA." >&2
  exit 0
fi

# 4. Confirmed submit — consumes a daily slot; brainapi long-polls to verdict
#    and its exit code propagates as ours.
echo "==> submit: $BIN alphas submit $ALPHA" >&2
exec "$BIN" alphas submit "$ALPHA"
