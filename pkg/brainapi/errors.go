package brainapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Callers can use errors.Is to match.
var (
	// ErrNotAuthenticated means the client has no usable session and no
	// credentials cached for auto-relogin.
	ErrNotAuthenticated = errors.New("brainapi: not authenticated")

	// ErrDailyBudgetExhausted means the in-process daily budget gate refused
	// the call. The counter resets at the next ET-midnight BRAIN day boundary.
	ErrDailyBudgetExhausted = errors.New("brainapi: daily budget exhausted")

	// ErrCooldown means a recent rate-limit or "concurrent simulation" response
	// put this Client into a temporary cooldown; the call was refused locally
	// without hitting the network.
	ErrCooldown = errors.New("brainapi: client in cooldown")

	// ErrLongPollExceeded means a long-poll (submit / simulation / pnl /
	// check-alpha) didn't reach a terminal state within MaxLongPolls.
	ErrLongPollExceeded = errors.New("brainapi: long-poll cap exceeded")
)

// APIError is the catch-all wrapper for any unexpected non-2xx response.
type APIError struct {
	Status int
	Method string
	URL    string
	Body   []byte
}

func (e *APIError) Error() string {
	body := string(e.Body)
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	return fmt.Sprintf("brainapi: %s %s -> HTTP %d: %s", e.Method, e.URL, e.Status, body)
}

// RateLimitError is returned when a 429 (or 503 outside a long-poll context)
// trips the retry policy and the caller must back off.
type RateLimitError struct {
	Status     int
	RetryAfter time.Duration
	Cooldown   bool // true when the server hinted "concurrent" / "previous to finish"
	URL        string
	Body       []byte
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("brainapi: rate-limited (HTTP %d, retry-after %s)", e.Status, e.RetryAfter)
}

// BannedError means the client (typically a secondary account) hit the configured
// consecutive-403 threshold and is now considered banned.
type BannedError struct {
	Streak int
	Reason string
}

func (e *BannedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("brainapi: account banned after %d consecutive 403s: %s", e.Streak, e.Reason)
	}
	return fmt.Sprintf("brainapi: account banned after %d consecutive 403s", e.Streak)
}

// NotVerifiedError is returned when login or a probe shows the account is not
// yet email-verified. BRAIN exposes this as 403 with body containing
// "NOT_VERIFIED".
type NotVerifiedError struct {
	Email  string
	Status int
	Body   []byte
}

func (e *NotVerifiedError) Error() string {
	if e.Email != "" {
		return fmt.Sprintf("brainapi: account %s not verified", e.Email)
	}
	return "brainapi: account not verified"
}

// DRFError carries the Django REST framework field-validation envelope:
//
//	{"<field>": ["<msg>", ...], ...}
//
// Locale-aware — strings come back in whatever language the BRAIN edge
// chose (often zh-CN). Match on field names, never on message strings.
type DRFError struct {
	Status int
	Fields map[string][]string
	URL    string
}

func (e *DRFError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	return fmt.Sprintf("brainapi: HTTP %d validation: fields=%v", e.Status, keys)
}

// PersonaInquiryError is returned when POST /authentication produces the
// 2FA-like persona envelope. Callers who want to drive the persona challenge
// must capture Inquiry and call the (currently un-exercised) persona path.
// In steady-state production this branch is dead — kept as a safety net.
type PersonaInquiryError struct {
	Inquiry string
}

func (e *PersonaInquiryError) Error() string {
	return fmt.Sprintf("brainapi: persona inquiry required (id=%s)", e.Inquiry)
}

// AsAPIError unwraps to *APIError if err carries one.
func AsAPIError(err error) (*APIError, bool) {
	var e *APIError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// AsRateLimitError unwraps to *RateLimitError if err carries one.
func AsRateLimitError(err error) (*RateLimitError, bool) {
	var e *RateLimitError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// AsDRFError unwraps to *DRFError if err carries one.
func AsDRFError(err error) (*DRFError, bool) {
	var e *DRFError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// decodeBody unmarshals a response body into a fresh T, wrapping any parse
// error as "brainapi: parse <desc>: %w".
func decodeBody[T any](body []byte, desc string) (*T, error) {
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("brainapi: parse "+desc+": %w", err)
	}
	return &v, nil
}

// checkStatus returns an *APIError when resp is not a 2xx, mirroring the inline
// guard used by the auth/email/password endpoints — all POST mutations, so the
// method is fixed rather than threaded through every caller.
func (c *Client) checkStatus(resp *rawResponse, path string) error {
	if resp.status < 200 || resp.status >= 300 {
		return &APIError{Status: resp.status, Method: "POST", URL: c.joinURL(path, nil), Body: resp.body}
	}
	return nil
}

// requireNonEmpty rejects an empty argument with "<name> required" wrapped in
// ErrInvalidArgument.
func requireNonEmpty(val, name string) error {
	if val == "" {
		return fmt.Errorf("%w: %s required", ErrInvalidArgument, name)
	}
	return nil
}
