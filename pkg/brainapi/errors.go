package brainapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
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

// Classify maps any error produced by this package into a stable, machine-
// matchable kind string and a structured details map. It is the single source
// of truth for the SDK's error taxonomy: the CLI renders it as its
// {ok:false,error:{kind,...}} envelope and the brainapi-mcp server returns it
// as a structured tool error, so the two consumption modes classify identically.
//
// Branch on the returned kind, never on the human-readable message — DRF
// validation messages are locale-dependent. The kinds are: api, rate_limit,
// banned, not_verified, drf_validation, persona_inquiry, budget,
// not_authenticated, cooldown, long_poll_exceeded, context, invalid_argument,
// network, error. A nil error yields ("", nil).
func Classify(err error) (kind string, details map[string]any) {
	if err == nil {
		return "", nil
	}
	var (
		apiErr     *APIError
		rlErr      *RateLimitError
		banErr     *BannedError
		drfErr     *DRFError
		nvErr      *NotVerifiedError
		personaErr *PersonaInquiryError
		netErr     net.Error
	)
	switch {
	case errors.As(err, &apiErr):
		return "api", map[string]any{
			"status": apiErr.Status, "method": apiErr.Method,
			"url": apiErr.URL, "body": jsonOrString(apiErr.Body),
		}
	case errors.As(err, &rlErr):
		return "rate_limit", map[string]any{
			"status": rlErr.Status, "retry_after_ms": rlErr.RetryAfter.Milliseconds(),
			"cooldown": rlErr.Cooldown, "body": jsonOrString(rlErr.Body),
		}
	case errors.As(err, &banErr):
		return "banned", map[string]any{"streak": banErr.Streak, "reason": banErr.Reason}
	case errors.As(err, &drfErr):
		return "drf_validation", map[string]any{"status": drfErr.Status, "url": drfErr.URL, "fields": drfErr.Fields}
	case errors.As(err, &nvErr):
		return "not_verified", map[string]any{"status": nvErr.Status, "body": jsonOrString(nvErr.Body)}
	case errors.As(err, &personaErr):
		return "persona_inquiry", map[string]any{"inquiry": personaErr.Inquiry}
	case errors.Is(err, ErrDailyBudgetExhausted):
		return "budget", nil
	case errors.Is(err, ErrNotAuthenticated):
		return "not_authenticated", nil
	case errors.Is(err, ErrCooldown):
		return "cooldown", nil
	case errors.Is(err, ErrLongPollExceeded):
		return "long_poll_exceeded", nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "context", nil
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument", nil
	case errors.As(err, &netErr):
		return "network", nil
	default:
		return "error", nil
	}
}

// jsonOrString returns b parsed as raw JSON when it looks like a JSON object or
// array, the trimmed string for any other non-empty body, and "" for an empty
// body — keeping error details readable whether or not BRAIN returned JSON.
func jsonOrString(b []byte) any {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return ""
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return json.RawMessage(b)
	}
	return trimmed
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
