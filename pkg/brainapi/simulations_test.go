package brainapi_test

import (
	"context"
	"errors"
	"io"
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
	res, err := cl.CreateSimulation(context.Background(), brainapi.SimulationRequest{
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
	if res.ID != "R8sL22iY4nxcgCxslACWlI" {
		t.Errorf("got id=%q", res.ID)
	}
	// No X-Ratelimit-* headers set → RateLimit.Present must be false.
	if res.RateLimit.Present {
		t.Errorf("RateLimit.Present should be false when headers absent, got %+v", res.RateLimit)
	}
}

func TestCreateSimulation_ParsesRateLimit(t *testing.T) {
	t.Parallel()
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/simulations/abc")
		w.Header().Set("X-Ratelimit-Limit", "5000")
		w.Header().Set("X-Ratelimit-Remaining", "4978")
		w.Header().Set("X-Ratelimit-Reset", "53848")
		w.WriteHeader(201)
	})
	_ = srv
	res, err := cl.CreateSimulation(context.Background(), simReqFixture())
	if err != nil {
		t.Fatalf("CreateSimulation: %v", err)
	}
	rl := res.RateLimit
	if !rl.Present || rl.Limit != 5000 || rl.Remaining != 4978 || rl.Reset != 53848*time.Second {
		t.Errorf("RateLimit mismatch: %+v", rl)
	}
}

func TestValidateMultiSim_RejectsBadBatches(t *testing.T) {
	t.Parallel()
	mk := func(region string, delay int) brainapi.SimulationRequest {
		return brainapi.SimulationRequest{Type: "REGULAR", Regular: "close",
			Settings: brainapi.SimSettings{InstrumentType: "EQUITY", Region: region, Universe: "TOP3000", Delay: delay, Language: "FASTEXPR"}}
	}
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must NOT be hit for an invalid batch")
		w.WriteHeader(500)
	})
	cases := map[string][]brainapi.SimulationRequest{
		"too few":      {mk("USA", 1)},
		"too many":     make([]brainapi.SimulationRequest, 11),
		"mixed region": {mk("USA", 1), mk("EUR", 1)},
		"mixed delay":  {mk("USA", 1), mk("USA", 0)},
	}
	for name, reqs := range cases {
		if _, err := cl.CreateMultiSimulation(context.Background(), reqs); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestCreateMultiSimulation_PostsArrayReturnsParent(t *testing.T) {
	t.Parallel()
	var gotArray bool
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		gotArray = strings.HasPrefix(strings.TrimSpace(string(buf)), "[")
		w.Header().Set("Location", "/simulations/PARENT123")
		w.Header().Set("X-Ratelimit-Remaining", "4970")
		w.WriteHeader(201)
	})
	_ = srv
	reqs := []brainapi.SimulationRequest{simReqFixture(), simReqFixture()}
	res, err := cl.CreateMultiSimulation(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateMultiSimulation: %v", err)
	}
	if !gotArray {
		t.Error("multi-sim body was not a JSON array")
	}
	if res.ID != "PARENT123" {
		t.Errorf("parent id=%q", res.ID)
	}
	if res.RateLimit.Remaining != 4970 {
		t.Errorf("rate limit remaining=%d", res.RateLimit.Remaining)
	}
}

// TestCreateMultiSimulation_BudgetAllOrNothing verifies the daily-sim gate is
// reserved atomically: an over-cap batch must NOT leak partial units, so a
// later batch that fits still succeeds.
func TestCreateMultiSimulation_BudgetAllOrNothing(t *testing.T) {
	t.Parallel()
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		w.Header().Set("Location", "/simulations/P")
		w.WriteHeader(201)
	}))
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL: srv.URL, Timeout: 5 * time.Second,
		DailyBudget: brainapi.DailyBudget{Sims: 3},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	five := make([]brainapi.SimulationRequest, 5)
	for i := range five {
		five[i] = simReqFixture()
	}
	// 5 > cap 3 → reject WITHOUT consuming or POSTing.
	if _, err := cl.CreateMultiSimulation(context.Background(), five); !errors.Is(err, brainapi.ErrDailyBudgetExhausted) {
		t.Fatalf("over-cap batch: want ErrDailyBudgetExhausted, got %v", err)
	}
	if p := posts.Load(); p != 0 {
		t.Fatalf("over-cap batch must not POST; posts=%d", p)
	}
	// The rejected batch must have leaked 0 units, so a 3-child batch now fits.
	three := five[:3]
	if _, err := cl.CreateMultiSimulation(context.Background(), three); err != nil {
		t.Fatalf("in-budget batch after a rejected one: %v (partial leak?)", err)
	}
	if p := posts.Load(); p != 1 {
		t.Fatalf("in-budget batch should POST exactly once; posts=%d", p)
	}
}

// TestWaitForMultiSimulation_BestEffortChildren verifies one failing child does
// not drop the healthy ones: c2 never terminates (ErrLongPollExceeded) but c1
// and c3 still resolve, and a non-nil error signals "incomplete".
func TestWaitForMultiSimulation_BestEffortChildren(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		switch r.URL.Path {
		case "/simulations/p1":
			_, _ = w.Write([]byte(`{"id":"p1","status":"COMPLETE","children":["c1","c2","c3"]}`))
		case "/simulations/c1":
			_, _ = w.Write([]byte(`{"id":"c1","status":"COMPLETE","alpha":"a1"}`))
		case "/simulations/c2":
			w.Header().Set("Retry-After", "0.01")
			_, _ = w.Write([]byte(`{"id":"c2","progress":0.3}`)) // never terminal → ErrLongPollExceeded
		case "/simulations/c3":
			_, _ = w.Write([]byte(`{"id":"c3","status":"COMPLETE","alpha":"a3"}`))
		}
	}))
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, Timeout: 5 * time.Second, MaxLongPolls: 1})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	parent, children, err := cl.WaitForMultiSimulation(context.Background(), "p1")
	if err == nil {
		t.Fatal("want a non-nil (incomplete) error when a child never terminates")
	}
	if parent == nil || len(children) != 2 {
		t.Fatalf("healthy children dropped: parent=%v children=%d", parent, len(children))
	}
	if children[0].Alpha != "a1" || children[1].Alpha != "a3" {
		t.Errorf("wrong surviving children: %q %q", children[0].Alpha, children[1].Alpha)
	}
}

// TestWaitForSimulation_TerminalCases covers the widened terminal predicate:
// a WARNING child (has alpha), and a completed PARENT (children[], no alpha).
func TestWaitForSimulation_TerminalCases(t *testing.T) {
	t.Parallel()
	t.Run("warning with alpha", func(t *testing.T) {
		_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"c1","parent":"p1","status":"WARNING","alpha":"aX9"}`))
		})
		s, err := cl.WaitForSimulation(context.Background(), "c1")
		if err != nil || s.Status != "WARNING" || s.Alpha != "aX9" {
			t.Fatalf("warning terminal not honored: %+v err=%v", s, err)
		}
	})
	t.Run("parent with children", func(t *testing.T) {
		_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"p1","status":"COMPLETE","children":["c1","c2"]}`))
		})
		s, err := cl.WaitForSimulation(context.Background(), "p1")
		if err != nil || len(s.Children) != 2 {
			t.Fatalf("parent terminal not honored: %+v err=%v", s, err)
		}
	})
}

func TestWaitForMultiSimulation_ResolvesChildren(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		switch r.URL.Path {
		case "/simulations/p1":
			_, _ = w.Write([]byte(`{"id":"p1","status":"COMPLETE","children":["c1","c2"]}`))
		case "/simulations/c1":
			_, _ = w.Write([]byte(`{"id":"c1","parent":"p1","status":"COMPLETE","alpha":"a1"}`))
		case "/simulations/c2":
			_, _ = w.Write([]byte(`{"id":"c2","parent":"p1","status":"WARNING","alpha":"a2"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	parent, children, err := cl.WaitForMultiSimulation(context.Background(), "p1")
	if err != nil {
		t.Fatalf("WaitForMultiSimulation: %v", err)
	}
	if len(parent.Children) != 2 || len(children) != 2 {
		t.Fatalf("want 2 children, got parent=%v children=%d", parent.Children, len(children))
	}
	if children[0].Alpha != "a1" || children[1].Alpha != "a2" {
		t.Errorf("child alphas wrong: %q %q", children[0].Alpha, children[1].Alpha)
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

func TestSimulationOptions_ReturnsRawSchemaMap(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions || r.URL.Path != "/simulations" {
			t.Errorf("wrong: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"name":"Simulation List","actions":{"POST":{"settings":{"children":{"maxTrade":{"choices":["OFF","ON"]}}}}}}`))
	})
	m, err := cl.SimulationOptions(context.Background())
	if err != nil {
		t.Fatalf("SimulationOptions: %v", err)
	}
	if _, ok := m["actions"]; !ok {
		t.Errorf("expected 'actions' key in options map, got keys %v", m)
	}
	if !strings.Contains(string(m["actions"]), "maxTrade") {
		t.Errorf("expected maxTrade in actions, got %s", m["actions"])
	}
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
