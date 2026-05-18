package brainapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestClientGetters(t *testing.T) {
	t.Parallel()
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       "https://example.test",
		Profile:       brainapi.ProfileChrome133,
		CookieJarPath: "/tmp/jar.json",
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if cl.BaseURL().Host != "example.test" {
		t.Errorf("BaseURL: %v", cl.BaseURL())
	}
	if cl.Profile() != brainapi.ProfileChrome133 {
		t.Errorf("Profile: %v", cl.Profile())
	}
	if cl.CookieJarPath() != "/tmp/jar.json" {
		t.Errorf("CookieJarPath: %v", cl.CookieJarPath())
	}
	if cl.Logger() == nil {
		t.Error("Logger nil")
	}
	if cl.IsBanned() {
		t.Error("fresh client should not be banned")
	}
	if cl.Cooldown() != 0 {
		t.Error("fresh client should have zero cooldown")
	}
}

func TestParseProfile(t *testing.T) {
	t.Parallel()
	// gocritic flags whitespace in map keys; build the trim-test case at runtime.
	cases := map[string]brainapi.BrowserProfile{
		"":          brainapi.DefaultProfile,
		"chrome131": brainapi.ProfileChrome131,
		"CHROME133": brainapi.ProfileChrome133,
		"nonsense":  brainapi.DefaultProfile,
	}
	cases[" safari16 "] = brainapi.ProfileSafari16
	for in, want := range cases {
		if got := brainapi.ParseProfile(in); got != want {
			t.Errorf("ParseProfile(%q)=%q, want %q", in, got, want)
		}
	}
	auto := brainapi.ParseProfile("auto:user@x.com")
	auto2 := brainapi.ParseProfile("auto:user@x.com")
	if auto != auto2 {
		t.Errorf("auto: non-deterministic %q vs %q", auto, auto2)
	}
}

func TestSetDefaultCaptchaSolver(t *testing.T) {
	t.Parallel()
	// Saving a default solver lets callers omit Options.CaptchaSolver and
	// still get a working Register flow.
	saved := stubCaptcha{payload: "default-payload"}
	brainapi.SetDefaultCaptchaSolver(saved)
	t.Cleanup(func() { brainapi.SetDefaultCaptchaSolver(nil) })
	// We can't assert directly; instead verify Register dispatches through it.
	var captured map[string]any
	srv, _ := newTestServerAndClientFromMux(t, muxRegister(t, &captured))
	cl, _ := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL})
	_, _ = cl.Register(context.Background(), brainapi.RegisterInput{
		Email: "x@y.com",
		Auxiliary: brainapi.Auxiliary{
			Password: "p",
		},
	})
	aux, _ := captured["auxiliary"].(map[string]any)
	if aux == nil || aux["captcha"] != "default-payload" {
		t.Errorf("expected default solver to populate captcha, got %v", captured)
	}
}

type stubCaptcha struct{ payload string }

func (s stubCaptcha) Solve(_ context.Context, _ func(context.Context) ([]byte, error)) (string, error) {
	return s.payload, nil
}

func muxRegister(t *testing.T, captured *map[string]any) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		b := drainBodyReq(t, r)
		_ = json.Unmarshal(b, captured)
		w.WriteHeader(201)
	})
	mux.HandleFunc("/captcha", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"algorithm":"SHA-256","challenge":"ff","salt":"s","signature":"sig","maxNumber":1}`))
	})
	return mux
}

func TestLongPoll_ContextCancellation(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0.5")
		w.WriteHeader(200) // empty body + Retry-After = long-poll
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := cl.CheckAlpha(ctx, "abc")
	if err == nil {
		t.Fatal("expected ctx cancel to surface as error")
	}
	// Don't pin the error text — it can be a wrapped context.DeadlineExceeded
	// or our own ErrLongPollExceeded; we only care that we returned promptly
	// instead of hanging until MaxLongPolls.
}

func TestConcurrent_ClientSafety(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := cl.Self(context.Background()); err != nil {
				t.Errorf("Self: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := hits.Load(); got != N {
		t.Errorf("expected %d hits, got %d", N, got)
	}
}

func TestPnLPoint_RoundTrip(t *testing.T) {
	t.Parallel()
	// Decode and re-encode the captured pnl fixture; the [date, value] tuple
	// shape must survive.
	raw := []byte(`[["2019-01-02",30026.0],["2019-01-03",35174.0]]`)
	var pts []brainapi.PnLPoint
	if err := json.Unmarshal(raw, &pts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pts) != 2 || pts[0].Date != "2019-01-02" || pts[0].Value != 30026.0 {
		t.Fatalf("decoded wrong: %+v", pts)
	}
	out, err := json.Marshal(pts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `["2019-01-02",30026]`) && !strings.Contains(string(out), `["2019-01-02",30026.0]`) {
		t.Errorf("round-trip failed: %s", out)
	}
}

func TestActivities_TupleDecoder_EdgeCases(t *testing.T) {
	t.Parallel()
	// Missing schema: returns (nil, nil) — caller can treat as empty.
	recs, err := brainapi.DecodeActivities(&brainapi.ActivityStream{})
	if err != nil || recs != nil {
		t.Errorf("nil-records case: recs=%v err=%v", recs, err)
	}

	// Empty properties: nil records.
	recs, err = brainapi.DecodeActivities(&brainapi.ActivityStream{
		Records: &brainapi.RecordSetBlock{Schema: &brainapi.RecordSchema{}},
	})
	if err != nil || recs != nil {
		t.Errorf("empty-props case: recs=%v err=%v", recs, err)
	}

	// Row longer than schema: extra columns silently dropped.
	stream := &brainapi.ActivityStream{
		Records: &brainapi.RecordSetBlock{
			Schema: &brainapi.RecordSchema{
				Properties: []brainapi.SchemaProperty{{Name: "date"}, {Name: "value"}},
			},
			Records: []json.RawMessage{
				json.RawMessage(`["2026-01-01",42,"extra"]`),
			},
		},
	}
	recs, err = brainapi.DecodeActivities(stream)
	if err != nil {
		t.Fatalf("oversized row: %v", err)
	}
	if len(recs) != 1 || len(recs[0]) != 2 {
		t.Errorf("expected one row of 2 fields, got %+v", recs)
	}

	// Malformed row JSON: surfaces error.
	bad := &brainapi.ActivityStream{
		Records: &brainapi.RecordSetBlock{
			Schema:  &brainapi.RecordSchema{Properties: []brainapi.SchemaProperty{{Name: "x"}}},
			Records: []json.RawMessage{json.RawMessage(`not-an-array`)},
		},
	}
	if _, err := brainapi.DecodeActivities(bad); err == nil {
		t.Error("expected decode error for malformed row")
	}
}
