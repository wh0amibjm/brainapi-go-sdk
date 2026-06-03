package brainapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// simReqFixture is a minimal valid SimulationRequest for tests that only care
// about the transport/concurrency behavior, not the body shape.
func simReqFixture() brainapi.SimulationRequest {
	return brainapi.SimulationRequest{
		Type: "REGULAR", Regular: "close",
		Settings: brainapi.SimSettings{InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1},
	}
}

func TestCreateSimulation_ReadsLocation(t *testing.T) {
	t.Parallel()
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/simulations" {
			t.Errorf("wrong: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Location", "https://api.worldquantbrain.com/simulations/R8sL22iY4nxcgCxslACWlI")
		w.Header().Set("Retry-After", "5.0")
		w.WriteHeader(201)
	})
	_ = srv
	id, err := cl.CreateSimulation(context.Background(), brainapi.SimulationRequest{
		Type:    "REGULAR",
		Regular: "close",
		Settings: brainapi.SimSettings{
			InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000",
			Delay: 1, Decay: 12, Neutralization: "SUBINDUSTRY",
			Truncation: 0.02, Pasteurization: "ON", UnitHandling: "VERIFY",
			NanHandling: "OFF", Language: "FASTEXPR",
		},
	})
	if err != nil {
		t.Fatalf("CreateSimulation: %v", err)
	}
	if id != "R8sL22iY4nxcgCxslACWlI" {
		t.Errorf("got id=%q", id)
	}
}

func TestWaitForSimulation_PollsUntilComplete(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.WriteHeader(200)
		if n < 3 {
			w.Header().Set("Retry-After", "0.01")
			_, _ = w.Write(loadFixture(t, "simulation_in_progress.json"))
			return
		}
		_, _ = w.Write(loadFixture(t, "simulation_complete.json"))
	})
	s, err := cl.WaitForSimulation(context.Background(), "abc")
	if err != nil {
		t.Fatalf("WaitForSimulation: %v", err)
	}
	if s.Status != "COMPLETE" || s.Alpha != "qMPjAxnO" {
		t.Errorf("wrong terminal: %+v", s)
	}
}

// TestWaitForSimulation_LongPollExceeded covers the cap-exceeded branch
// (returns ErrLongPollExceeded) that the happy-path test never reaches.
// MaxLongPolls=1 means a single in-progress poll then a fixed 5s sleep before
// the loop exits — kept tolerable by being the only iteration.
func TestWaitForSimulation_LongPollExceeded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "simulation_in_progress.json")) // never terminal
	}))
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, Timeout: 5 * time.Second, MaxLongPolls: 1})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = cl.WaitForSimulation(context.Background(), "abc")
	if !errors.Is(err, brainapi.ErrLongPollExceeded) {
		t.Fatalf("want ErrLongPollExceeded, got %v", err)
	}
}

// TestReserveSimSlot_BoundsConcurrentPosts verifies the MaxConcurrentSims
// semaphore actually caps in-flight POST /simulations: 5 concurrent creates
// against a cap of 2 must never let more than 2 handlers run at once.
func TestReserveSimSlot_BoundsConcurrentPosts(t *testing.T) {
	t.Parallel()
	var inflight, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := inflight.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		<-release // hold the slot until the test unblocks all at once
		inflight.Add(-1)
		w.Header().Set("Location", "/simulations/abc")
		w.WriteHeader(201)
	}))
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, Timeout: 5 * time.Second, MaxConcurrentSims: 2})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = cl.CreateSimulation(context.Background(), simReqFixture())
		}(i)
	}
	time.Sleep(250 * time.Millisecond) // let all 5 contend for the 2 slots
	close(release)
	wg.Wait()

	if p := peak.Load(); p > 2 {
		t.Errorf("peak concurrent POSTs = %d, want <= 2 (MaxConcurrentSims)", p)
	}
	for i, e := range errs {
		if e != nil {
			t.Errorf("CreateSimulation[%d]: %v", i, e)
		}
	}
}

// TestReserveSimSlot_CancelWhileQueued covers the ctx.Done() branch in
// reserveSimSlot: with the single slot occupied, a call whose context is
// already cancelled must return ctx.Err() WITHOUT dispatching to the server.
func TestReserveSimSlot_CancelWhileQueued(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	occupied := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		once.Do(func() { close(occupied) })
		<-release
		w.Header().Set("Location", "/simulations/abc")
		w.WriteHeader(201)
	}))
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, Timeout: 5 * time.Second, MaxConcurrentSims: 1})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cl.CreateSimulation(context.Background(), simReqFixture())
	}()
	<-occupied // the only slot is now genuinely held

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cl.CreateSimulation(ctx, simReqFixture())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled while queued for a slot, got %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("cancelled call must not reach the server; hits=%d", got)
	}

	close(release)
	<-done
}

func TestCreateSimulation_MissingLocation(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
	})
	_, err := cl.CreateSimulation(context.Background(), brainapi.SimulationRequest{
		Type: "REGULAR", Regular: "close",
		Settings: brainapi.SimSettings{InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "Location") {
		t.Fatalf("expected Location error, got %v", err)
	}
}
