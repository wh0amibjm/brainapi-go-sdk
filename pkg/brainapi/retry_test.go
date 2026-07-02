package brainapi_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestRetryAfter_FloatString(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	start := time.Now()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1.0")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})
	_, err := cl.Self(context.Background())
	if err != nil {
		t.Fatalf("Self: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 950*time.Millisecond {
		t.Errorf("expected to sleep ~1s after Retry-After: elapsed=%s", elapsed)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", calls.Load())
	}
}

// TestLongPoll_DoesNotConsumeErrorBudget guards the fix that decoupled the
// error-retry budget (st.errAttempt) from the long-poll counter
// (st.longPollSeen). Before the fix the shared `attempt` counter meant that
// once an endpoint had long-polled past MaxRetries, the very next transient
// error was surfaced as terminal instead of retried. Here MaxRetries=1: one
// long-poll tick precedes a transient 429; the 429 MUST still be retried and
// CheckAlpha must ultimately succeed on the terminal body.
func TestLongPoll_DoesNotConsumeErrorBudget(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			// long-poll tick: 200 + empty body + Retry-After (does NOT count
			// against the error budget).
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(200)
		case 2:
			// transient, non-cooldown rate-limit mid-poll: must be retried even
			// though a long-poll tick already happened.
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"slow down"}`))
		default:
			w.WriteHeader(200)
			_, _ = w.Write(loadFixture(t, "check_alpha_terminal.json"))
		}
	})
	if _, err := cl.CheckAlpha(context.Background(), "abc"); err != nil {
		t.Fatalf("CheckAlpha should retry the mid-poll 429 and succeed, got: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls (long-poll, retried 429, terminal), got %d", calls.Load())
	}
}

// TestBanThreshold_HitsAfterStreak uses an OPAQUE 403 (no DRF "detail" envelope
// — an edge block / real ban looks like this) so it still feeds ban detection.
// A structured permission 403 ({"detail":...}) deliberately does NOT — see
// TestPermissionDenied403_DoesNotBan.
func TestBanThreshold_HitsAfterStreak(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		_, _ = w.Write([]byte("Forbidden"))
	})
	_ = srv

	var be *brainapi.BannedError
	for i := 0; i < 10; i++ {
		_, err := cl.Self(context.Background())
		if errors.As(err, &be) {
			break
		}
	}
	if be == nil {
		t.Fatalf("expected BannedError, calls=%d", calls.Load())
	}
	if be.Streak < 3 {
		t.Errorf("expected streak >= 3, got %d", be.Streak)
	}
}

// TestPermissionDenied403_DoesNotBan locks the fix that a 403 carrying the DRF
// permission envelope ({"detail": "..."}) is a terminal, non-retryable
// PermissionDeniedError that does NOT feed the ban streak. Before the fix, a
// single call to a permission-gated endpoint retried the same 403 three times
// and self-tripped the ban flag, mislabelling a healthy account as banned.
func TestPermissionDenied403_DoesNotBan(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"detail":"You do not have permission to perform this action."}`))
	})

	_, err := cl.Self(context.Background())
	pd, ok := brainapi.AsPermissionDeniedError(err)
	if !ok {
		t.Fatalf("expected PermissionDeniedError, got %T %v", err, err)
	}
	if pd.Status != 403 || pd.Detail == "" {
		t.Errorf("PermissionDeniedError = %+v, want status 403 + non-empty detail", pd)
	}
	// Terminal on the first response: no futile retries against a permission wall.
	if n := calls.Load(); n != 1 {
		t.Errorf("expected exactly 1 call (no retry), got %d", n)
	}
	// And it must never be classified as a ban.
	var be *brainapi.BannedError
	if errors.As(err, &be) {
		t.Errorf("permission 403 must not surface as BannedError, got streak=%d", be.Streak)
	}
}

func TestDRFError_Maps400Body(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write(loadFixture(t, "drf_validation_400.json"))
	})
	err := cl.ReverifyEmail(context.Background(), "x@y.com", "captcha-stub")
	if err == nil {
		t.Fatal("expected DRFError")
	}
	d, ok := brainapi.AsDRFError(err)
	if !ok {
		t.Fatalf("expected DRFError, got %T %v", err, err)
	}
	if _, has := d.Fields["email"]; !has {
		t.Errorf("expected email in fields: %+v", d.Fields)
	}
}

func TestServerError_RetriesAndSurfacesAPIError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte("oops"))
	})
	_, err := cl.Self(context.Background())
	if err == nil {
		t.Fatal("expected APIError")
	}
	ae, ok := brainapi.AsAPIError(err)
	if !ok || ae.Status != 500 {
		t.Fatalf("expected APIError(500), got %v", err)
	}
	if calls.Load() < 2 {
		t.Errorf("expected retry, calls=%d", calls.Load())
	}
}

// TestConcurrentCollision429_NoGlobalCooldown pins the P1-SDK-1 contract: a
// "concurrent simulation / previous to finish" 429 is a TRANSIENT per-request
// collision and must NOT put the whole Client into a global cooldown. It is
// retried per-request and, once retries exhaust, surfaces as a RateLimitError
// (Cooldown:true for classification) — but a SUBSEQUENT healthy call must still
// dispatch to the network, never short-circuit with ErrCooldown.
func TestConcurrentCollision429_NoGlobalCooldown(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"detail":"concurrent simulation, please wait for previous to finish"}`))
	})
	// First call retries per-request then surfaces a RateLimitError.
	_, err := cl.Self(context.Background())
	var rlErr *brainapi.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if !rlErr.Cooldown {
		t.Errorf("expected RateLimitError.Cooldown=true for a concurrent-collision body")
	}
	// The collision must NOT have armed a global cooldown.
	if d := cl.Cooldown(); d != 0 {
		t.Errorf("concurrent-collision 429 must not set a global cooldown, got %s", d)
	}
	// A subsequent call must still reach the network (not blocked by ErrCooldown).
	preCalls := calls.Load()
	_, err = cl.Self(context.Background())
	if errors.Is(err, brainapi.ErrCooldown) {
		t.Errorf("healthy call was blocked by a stale global cooldown")
	}
	if calls.Load() <= preCalls {
		t.Errorf("subsequent call did not dispatch: pre=%d post=%d", preCalls, calls.Load())
	}
}

// TestConcurrentCollision429_DoesNotStallConcurrentRequests is the regression
// for the multi-sim scenario: one goroutine hits a persistent concurrent-
// collision 429 while a second, independent request must complete unimpeded —
// proving a single collision no longer freezes healthy in-flight parents.
func TestConcurrentCollision429_DoesNotStallConcurrentRequests(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/self" {
			// The "collision" endpoint: always a concurrent-collision 429.
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"concurrent simulation, please wait for previous to finish"}`))
			return
		}
		// The healthy endpoint (/operators is a bare array) must always succeed.
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[]`))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	// Collider goroutine: exhausts retries and returns a RateLimitError.
	go func() {
		defer wg.Done()
		_, _ = cl.Self(context.Background())
	}()
	// Healthy goroutine: a GET /operators must succeed while the collider spins.
	var healthyErr error
	go func() {
		defer wg.Done()
		_, healthyErr = cl.Operators(context.Background())
	}()
	wg.Wait()

	if healthyErr != nil {
		t.Fatalf("healthy concurrent request was stalled/failed by a collision: %v", healthyErr)
	}
	if d := cl.Cooldown(); d != 0 {
		t.Errorf("no global cooldown should be armed, got %s", d)
	}
}

func TestVerifyEmail_BearerHeader(t *testing.T) {
	t.Parallel()
	var seen string
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{}"))
	})
	if err := cl.VerifyEmail(context.Background(), "eyJabc"); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if seen != "Bearer eyJabc" {
		t.Errorf("expected Bearer JWT, got %q", seen)
	}
}

// Sanity for the float Retry-After parser via observable timing.
func TestRetryAfter_ClampedToFloor(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	start := time.Now()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0.001") // below floor; should clamp to 1s
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"slow down"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})
	_, _ = cl.Self(context.Background())
	if time.Since(start) < 900*time.Millisecond {
		t.Error("Retry-After floor of 1s not enforced")
	}
}

func TestProfileForEmail_Deterministic(t *testing.T) {
	t.Parallel()
	p1 := brainapi.ProfileForEmail("alice@x.com")
	p2 := brainapi.ProfileForEmail("alice@x.com")
	if p1 != p2 {
		t.Errorf("non-deterministic: %s vs %s", p1, p2)
	}
	// Different inputs should sometimes pick different buckets — sample several.
	distinct := map[brainapi.BrowserProfile]bool{}
	for i := 0; i < 20; i++ {
		distinct[brainapi.ProfileForEmail("user"+strconv.Itoa(i)+"@x.com")] = true
	}
	if len(distinct) < 2 {
		t.Errorf("expected hashing to spread across buckets, only saw %d", len(distinct))
	}
}
