package brainapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// stubSolver returns a fixed payload and counts invocations.
type stubSolver struct {
	payload string
	calls   int
}

func (s *stubSolver) Solve(_ context.Context, fetch func(context.Context) ([]byte, error)) (string, error) {
	s.calls++
	// Drain the challenge fetch so we exercise the wire-up too.
	_, _ = fetch(context.Background())
	return s.payload, nil
}

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()
	captchaPayload := "deadbeefbase64"
	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/captcha", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"algorithm":"SHA-256","challenge":"ff","salt":"s","signature":"sig","maxNumber":1}`))
	})
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		capturedBody = drainBodyReq(t, r)
		w.WriteHeader(201)
	})
	srv, _ := newTestServerAndClientFromMux(t, mux)
	_ = srv

	ctx := context.Background()
	solver := &stubSolver{payload: captchaPayload}
	cl, _ := brainapi.NewClient(brainapi.Options{
		BaseURL:       srv.URL,
		CaptchaSolver: solver,
		MaxRetries:    1,
	})
	in := brainapi.RegisterInput{
		Email: "newsub@x.com", FirstName: "F", LastName: "L", FullName: "FL", Gender: "MALE",
		Address:   brainapi.Address{Country: "US"},
		Education: brainapi.Education{University: "MIT", Major: "CS", Degree: "BACHELORS", GradYear: 2020},
		Auxiliary: brainapi.Auxiliary{Password: "pw"},
	}
	if _, err := cl.Register(ctx, in); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if solver.calls != 1 {
		t.Errorf("expected captcha solver to be called once, got %d", solver.calls)
	}
	// Verify the captcha payload landed in the request body.
	var posted map[string]any
	if err := json.Unmarshal(capturedBody, &posted); err != nil {
		t.Fatalf("parse posted body: %v", err)
	}
	aux, _ := posted["auxiliary"].(map[string]any)
	if aux == nil || aux["captcha"] != captchaPayload {
		t.Errorf("captcha not injected: posted body=%s", string(capturedBody))
	}
}

func TestRegister_MissingEmail(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit")
	})
	if _, err := cl.Register(context.Background(), brainapi.RegisterInput{}); err == nil {
		t.Fatal("expected ErrInvalidArgument")
	}
}

func TestForbidden_WithChecksBodyIsNotBan(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write(loadFixture(t, "submit_403_corr_fail.json"))
	})
	// CheckAlpha returns the body as a normal Is block on 403 with checks
	// payload; ban counter must NOT increment.
	for i := 0; i < 5; i++ {
		_, _ = cl.CheckAlpha(context.Background(), "abc")
	}
	if cl.IsBanned() {
		t.Error("403 with checks body should not trip ban-detector")
	}
}
