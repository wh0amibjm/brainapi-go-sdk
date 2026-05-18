package brainapi_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// recordingObserver captures every callback for assertions.
type recordingObserver struct {
	mu        sync.Mutex
	requests  []reqRow
	retries   []retryRow
	longPolls []longPollRow
}

type reqRow struct {
	method, path string
	status       int
	dur          time.Duration
	err          error
}
type retryRow struct {
	method, path string
	status       int
	attempt      int
	kind         brainapi.RetryKind
	sleep        time.Duration
}
type longPollRow struct {
	method, path string
	iter         int
	sleep        time.Duration
}

func (o *recordingObserver) ObserveRequest(method, path string, status int, dur time.Duration, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.requests = append(o.requests, reqRow{method, path, status, dur, err})
}

func (o *recordingObserver) ObserveRetry(method, path string, status, attempt int, kind brainapi.RetryKind, sleep time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.retries = append(o.retries, retryRow{method, path, status, attempt, kind, sleep})
}

func (o *recordingObserver) ObserveLongPoll(method, path string, iter int, sleep time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.longPolls = append(o.longPolls, longPollRow{method, path, iter, sleep})
}

func TestObserver_HappyPath(t *testing.T) {
	t.Parallel()
	obs := &recordingObserver{}
	_, cl := newTestServerAndClientWithObs(t, obs, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})
	if _, err := cl.Self(context.Background()); err != nil {
		t.Fatalf("Self: %v", err)
	}
	if got := len(obs.requests); got != 1 {
		t.Errorf("expected 1 ObserveRequest, got %d", got)
	}
	if obs.requests[0].status != 200 || obs.requests[0].path != "/users/self" {
		t.Errorf("unexpected row: %+v", obs.requests[0])
	}
	if len(obs.retries) != 0 {
		t.Errorf("expected no retries, got %+v", obs.retries)
	}
}

func TestObserver_FiresOnRetryAndLongPoll(t *testing.T) {
	t.Parallel()
	obs := &recordingObserver{}
	var calls atomic.Int32
	_, cl := newTestServerAndClientWithObs(t, obs, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"slow"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})
	if _, err := cl.Self(context.Background()); err != nil {
		t.Fatalf("Self: %v", err)
	}
	if len(obs.retries) != 1 || obs.retries[0].kind != brainapi.RetryKindRateLimit {
		t.Errorf("expected one rate-limit retry observation: %+v", obs.retries)
	}

	// Reset and trigger long-poll on /check.
	obs2 := &recordingObserver{}
	var lpCalls atomic.Int32
	_, cl2 := newTestServerAndClientWithObs(t, obs2, func(w http.ResponseWriter, _ *http.Request) {
		n := lpCalls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "check_alpha_terminal.json"))
	})
	if _, err := cl2.CheckAlpha(context.Background(), "abc"); err != nil {
		t.Fatalf("CheckAlpha: %v", err)
	}
	if got := len(obs2.longPolls); got < 2 {
		t.Errorf("expected long-poll observations, got %d", got)
	}
}

func newTestServerAndClientWithObs(t *testing.T, obs brainapi.Observer, handler http.HandlerFunc) (interface{ Close() }, *brainapi.Client) {
	t.Helper()
	srv, _ := newTestServerAndClient(t, handler)
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:      srv.URL,
		Timeout:      5 * time.Second,
		MaxRetries:   2,
		MaxLongPolls: 4,
		Observer:     obs,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return srv, cl
}
