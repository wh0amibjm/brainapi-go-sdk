package brainapi_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

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
