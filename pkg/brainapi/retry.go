package brainapi

import (
	"strconv"
	"strings"
	"time"
)

// parseRetryAfter parses the Retry-After header. BRAIN sends FLOAT-second
// strings like "5.0" — int parsing alone would error and drop the hint.
// We accept either an HTTP-date or a float-second.
//
// Returns ok=false when the header is absent or unparsable.
func parseRetryAfter(h string) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(h, 64); err == nil && f >= 0 {
		return time.Duration(f * float64(time.Second)), true
	}
	if t, err := time.Parse(time.RFC1123, h); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// clamp returns d bounded to [lo, hi]. Used to keep server-suggested
// Retry-After values inside a sane range so a buggy edge can't make us
// sleep forever or hammer immediately.
func clamp(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// rateLimitFloor / rateLimitCeiling bound the 429 Retry-After we'll honor.
// Matches _brain-api.ts behavior — server hint capped to [1s, 120s].
const (
	rateLimitFloor   = 1 * time.Second
	rateLimitCeiling = 120 * time.Second

	longPollFloor   = 500 * time.Millisecond
	longPollCeiling = 30 * time.Second

	serverErrFloor   = 5 * time.Second
	serverErrCeiling = 15 * time.Second

	networkErrFloor   = 2 * time.Second
	networkErrCeiling = 15 * time.Second
)

// isCooldownBody returns true when a 429 body carries text that, per
// production observation, indicates a "concurrent simulation" / "previous
// to finish" semaphore-style server-side limit. The Client treats those
// hits as a global cooldown rather than per-request backoff.
func isCooldownBody(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "concurrent") || strings.Contains(s, "previous to finish")
}

// retryHints lets a caller override the default error/long-poll handling for
// endpoints with BRAIN-specific semantics.
type retryHints struct {
	// longPoll503 == true: 503 means "still computing", sleep Retry-After
	// then retry. longPoll503 == false: 503 is just a server hiccup, retry
	// with the standard server-error backoff.
	longPoll503 bool

	// accept503 == true: 503 is a terminal-success ("accepted, queued").
	// The response is returned to the caller as if it were 2xx. Used by
	// POST /alphas/{id}/submit where 503 means BRAIN queued the submit.
	accept503 bool

	// longPoll200Empty == true: a 200 response with empty body and a
	// Retry-After header means "still computing" too (check-alpha and
	// recordsets/pnl). Sleep + retry within MaxLongPolls.
	longPoll200Empty bool

	// noAutoRelogin == true: a 401 must NOT trigger an auto-relogin.
	// Login itself sets this to avoid infinite recursion on bad creds.
	noAutoRelogin bool

	// maxLongPolls overrides Client.maxLongPolls for this call.
	maxLongPolls int
}
