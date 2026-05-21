// Package uxlog is the shared UX shell for the scripts/ live runners
// (register, live-smoke). It standardises the N/M step framing, the
// end-of-run summary block, and the error-to-hint mapping so the two
// orchestrators stay surface-compatible without copy-paste.
//
// Not part of the public SDK -- script-local on purpose. Stderr output
// only; stdout stays clean for subprocess composition.
package uxlog

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// stderr is the sink for the summary block. Indirected so tests in this
// package can capture it; production always writes to os.Stderr.
var stderr io.Writer = os.Stderr

// Step runs fn as one labelled step. The N/M counter, name, ok/FAILED
// suffix and elapsed time get logged uniformly. Returns fn's error so the
// caller can decide whether to abort or continue.
func Step(logger *slog.Logger, idx, total int, name string, fn func() error) error {
	label := fmt.Sprintf("%d/%d %s", idx, total, name)
	start := time.Now()
	if err := fn(); err != nil {
		logger.Error(label+" FAILED", "err", err.Error(), "elapsed", time.Since(start).Round(time.Millisecond))
		return err
	}
	logger.Info(label+" ok", "elapsed", time.Since(start).Round(time.Millisecond))
	return nil
}

// Summary is the end-of-run block printed to stderr. Plain ASCII so it
// stays readable in CI logs and subprocess pipes. nextHint may be empty.
func Summary(scriptName, accountRedacted string, started time.Time, passed, failed, skipped []string, nextHint string) {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "=== %s summary ===\n", scriptName)
	fmt.Fprintf(&b, "account:   %s\n", accountRedacted)
	fmt.Fprintf(&b, "elapsed:   %s\n", time.Since(started).Round(time.Millisecond))
	fmt.Fprintf(&b, "passed:    %d/%d\n", len(passed), len(passed)+len(failed))
	fmt.Fprintf(&b, "failed:    %d", len(failed))
	if len(failed) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(failed, ", "))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "skipped:   %d", len(skipped))
	if len(skipped) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(skipped, ", "))
	}
	fmt.Fprintln(&b)
	if nextHint != "" {
		fmt.Fprintf(&b, "next:      %s\n", nextHint)
	}
	fmt.Fprint(&b, "\n")
	// Bypass slog so the block lands as one contiguous chunk on stderr.
	fmt.Fprint(stderr, b.String())
}

// HintForError maps a typed SDK error to a one-line operator hint. Empty
// string when no specific guidance applies; callers should fall back to
// the raw error text in that case rather than synthesising fake advice.
func HintForError(err error) string {
	if err == nil {
		return ""
	}
	var banned *brainapi.BannedError
	if errors.As(err, &banned) {
		return "Account tripped 403 ban-counter; pick another secondary account from testdata/test-accounts/accounts.json."
	}
	var persona *brainapi.PersonaInquiryError
	if errors.As(err, &persona) {
		return "BRAIN issued a persona challenge; this account needs a manual login through the web UI first."
	}
	var rate *brainapi.RateLimitError
	if errors.As(err, &rate) {
		return "Cooldown active; back off until Retry-After clears, then retry."
	}
	var notVerified *brainapi.NotVerifiedError
	if errors.As(err, &notVerified) {
		return "BRAIN sends verify links via SendGrid wrapper; resolution needs a US residential IP. Account is already TUTORIAL-approved without this step."
	}
	if errors.Is(err, brainapi.ErrDailyBudgetExhausted) {
		return "Daily budget exhausted; resets at the next ET-midnight (US/Eastern) BRAIN day boundary."
	}
	if errors.Is(err, brainapi.ErrLongPollExceeded) {
		return "Long-poll cap exceeded; raise MaxLongPolls or re-run once BRAIN has warmed its cache."
	}
	if errors.Is(err, brainapi.ErrNotAuthenticated) {
		return "Session missing; set BRAINAPI_USER/BRAINAPI_PASS or pass --user/--pass."
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return "Network or TLS-impersonation failure; check proxy/BRAINAPI_PROXY and that the browser profile still clears Cloudflare today."
	}
	return ""
}

// Redact keeps just the @domain part of an email for log breadcrumbs.
// Same shape as scripts/live-smoke's local helper, lifted here so both
// orchestrators share one implementation.
func Redact(email string) string {
	if i := strings.IndexByte(email, '@'); i >= 0 {
		return "***" + email[i:]
	}
	return "***"
}
