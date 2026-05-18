package brainapi_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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

func TestBanThreshold_HitsAfterStreak(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"detail":"forbidden"}`))
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

func TestCooldown_PropagatesToSubsequentCalls(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0.1")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"detail":"concurrent simulation, please wait for previous to finish"}`))
	})
	// First call trips cooldown then surfaces RateLimitError once retries exhaust.
	_, _ = cl.Self(context.Background())
	cooldown := cl.Cooldown()
	if cooldown <= 0 {
		t.Errorf("expected cooldown > 0, got %s", cooldown)
	}

	// Second call should refuse immediately without a network hit.
	preCalls := calls.Load()
	_, err := cl.Self(context.Background())
	if !errors.Is(err, brainapi.ErrCooldown) {
		t.Errorf("expected ErrCooldown, got %v", err)
	}
	if calls.Load() != preCalls {
		t.Errorf("cooldown didn't block dispatch: pre=%d post=%d", preCalls, calls.Load())
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
