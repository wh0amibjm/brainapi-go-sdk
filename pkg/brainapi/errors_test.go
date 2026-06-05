package brainapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// fakeNetErr is a minimal net.Error so Classify's transport branch is reachable
// without opening a real socket.
type fakeNetErr struct{}

func (fakeNetErr) Error() string   { return "dial tcp: refused" }
func (fakeNetErr) Timeout() bool   { return false }
func (fakeNetErr) Temporary() bool { return false }

// TestClassify locks the SDK error taxonomy shared by the CLI envelope and the
// brainapi-mcp structured errors: each typed/sentinel error must map to its
// stable kind, and the mapping must survive error wrapping (errors.As/Is).
func TestClassify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"api", &brainapi.APIError{Status: 500, Method: "GET", URL: "/x", Body: []byte("boom")}, "api"},
		{"rate_limit", &brainapi.RateLimitError{Status: 429, RetryAfter: 5 * time.Second}, "rate_limit"},
		{"banned", &brainapi.BannedError{Streak: 3}, "banned"},
		{"not_verified", &brainapi.NotVerifiedError{Email: "x@y.com", Status: 403}, "not_verified"},
		{"drf_validation", &brainapi.DRFError{Status: 400, Fields: map[string][]string{"email": {"required"}}}, "drf_validation"},
		{"persona_inquiry", &brainapi.PersonaInquiryError{Inquiry: "inq_abc"}, "persona_inquiry"},
		{"budget", brainapi.ErrDailyBudgetExhausted, "budget"},
		{"not_authenticated", brainapi.ErrNotAuthenticated, "not_authenticated"},
		{"cooldown", brainapi.ErrCooldown, "cooldown"},
		{"long_poll_exceeded", brainapi.ErrLongPollExceeded, "long_poll_exceeded"},
		{"context", context.Canceled, "context"},
		{"invalid_argument", brainapi.ErrInvalidArgument, "invalid_argument"},
		{"network", fakeNetErr{}, "network"},
		{"wrapped-api", fmt.Errorf("submit failed: %w", &brainapi.APIError{Status: 500}), "api"},
		{"plain", errors.New("something else"), "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := brainapi.Classify(tc.err)
			if got != tc.want {
				t.Errorf("Classify kind = %q, want %q", got, tc.want)
			}
		})
	}

	if kind, details := brainapi.Classify(nil); kind != "" || details != nil {
		t.Errorf("Classify(nil) = (%q, %v), want (\"\", nil)", kind, details)
	}
}

// TestClassifyDetails verifies the structured details that agents and shell
// callers branch on are populated for the kinds that carry them.
func TestClassifyDetails(t *testing.T) {
	t.Parallel()

	_, d := brainapi.Classify(&brainapi.RateLimitError{Status: 429, RetryAfter: 5 * time.Second, Cooldown: true})
	if d["retry_after_ms"] != int64(5000) || d["cooldown"] != true || d["status"] != 429 {
		t.Errorf("rate_limit details = %v", d)
	}

	_, d = brainapi.Classify(&brainapi.DRFError{Status: 400, Fields: map[string][]string{"email": {"required"}}})
	fields, ok := d["fields"].(map[string][]string)
	if !ok || len(fields["email"]) != 1 {
		t.Errorf("drf_validation details.fields = %v", d["fields"])
	}

	// A JSON body is surfaced verbatim as raw JSON; a non-JSON body as a string.
	_, d = brainapi.Classify(&brainapi.APIError{Status: 400, Body: []byte(`{"detail":"THROTTLED"}`)})
	if _, ok := d["body"].(json.RawMessage); !ok {
		t.Errorf("api details.body for JSON body = %T, want json.RawMessage", d["body"])
	}
	_, d = brainapi.Classify(&brainapi.APIError{Status: 400, Body: []byte("plain text")})
	if d["body"] != "plain text" {
		t.Errorf("api details.body for non-JSON body = %v, want \"plain text\"", d["body"])
	}
}

// TestErrorStrings exercises every typed error's Error() method. They're
// trivial format strings but lint rules expect them to be reachable from
// tests so future formatting changes don't go unnoticed.
func TestErrorStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"APIError", &brainapi.APIError{Status: 500, Method: "GET", URL: "/x", Body: []byte("boom")}, "GET /x -> HTTP 500"},
		{"APIError truncates body", &brainapi.APIError{Status: 400, Body: []byte(strings.Repeat("a", 300))}, "HTTP 400"},
		{"RateLimitError", &brainapi.RateLimitError{Status: 429, RetryAfter: 5 * time.Second}, "rate-limited"},
		{"BannedError-no-reason", &brainapi.BannedError{Streak: 3}, "banned after 3"},
		{"BannedError-with-reason", &brainapi.BannedError{Streak: 5, Reason: "auth fail"}, "auth fail"},
		{"NotVerifiedError-no-email", &brainapi.NotVerifiedError{}, "not verified"},
		{"NotVerifiedError-with-email", &brainapi.NotVerifiedError{Email: "x@y.com"}, "x@y.com"},
		{"DRFError", &brainapi.DRFError{Status: 400, Fields: map[string][]string{"email": {"required"}}}, "fields=[email]"},
		{"PersonaInquiryError", &brainapi.PersonaInquiryError{Inquiry: "inq_abc"}, "inq_abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.err.Error()
			if !strings.Contains(got, tc.want) {
				t.Errorf("Error()=%q does not contain %q", got, tc.want)
			}
		})
	}
}

func TestErrorHelpers_UnwrapTypedErrors(t *testing.T) {
	t.Parallel()
	apiErr := &brainapi.APIError{Status: 500}
	wrapped := errors.Join(errors.New("outer"), apiErr)
	if got, ok := brainapi.AsAPIError(wrapped); !ok || got.Status != 500 {
		t.Errorf("AsAPIError: got=%v ok=%v", got, ok)
	}
	if _, ok := brainapi.AsAPIError(errors.New("plain")); ok {
		t.Error("AsAPIError should reject plain error")
	}

	rlErr := &brainapi.RateLimitError{Status: 429, RetryAfter: 1 * time.Second}
	wrapped2 := errors.Join(errors.New("outer"), rlErr)
	if got, ok := brainapi.AsRateLimitError(wrapped2); !ok || got.Status != 429 {
		t.Errorf("AsRateLimitError: got=%v ok=%v", got, ok)
	}
	if _, ok := brainapi.AsRateLimitError(errors.New("plain")); ok {
		t.Error("AsRateLimitError should reject plain error")
	}

	drfErr := &brainapi.DRFError{Status: 400, Fields: map[string][]string{"email": {"required"}}}
	wrapped3 := errors.Join(errors.New("outer"), drfErr)
	if got, ok := brainapi.AsDRFError(wrapped3); !ok || len(got.Fields) != 1 {
		t.Errorf("AsDRFError: got=%v ok=%v", got, ok)
	}
	if _, ok := brainapi.AsDRFError(errors.New("plain")); ok {
		t.Error("AsDRFError should reject plain error")
	}
}
