package brainapi_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestGetAlpha(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "alpha_detail.json"))
	})
	a, err := cl.GetAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("GetAlpha: %v", err)
	}
	if a.ID != "qMPjAxnO" || a.Status != "UNSUBMITTED" {
		t.Errorf("got %+v", a)
	}
	if a.Is == nil || a.Is.Sharpe != 2.25 {
		t.Errorf("is.sharpe wrong: %+v", a.Is)
	}
}

func TestCheckAlpha_LongPollThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(200)
			return // empty body = "still computing"
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "check_alpha_terminal.json"))
	})
	is, err := cl.CheckAlpha(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("CheckAlpha: %v", err)
	}
	if is == nil || len(is.Checks) == 0 {
		t.Fatalf("empty checks: %+v", is)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 polls, got %d", calls.Load())
	}
}

func TestSubmitAlpha_CorrRejected(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.Method == http.MethodPost {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(503)
			return
		}
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(403)
		_, _ = w.Write(loadFixture(t, "submit_403_corr_fail.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "corr_rejected" {
		t.Errorf("expected corr_rejected, got %s (reason=%s)", v.Status, v.Reason)
	}
}

func TestSubmitAlpha_PendingThenLongPollExceeded(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_pending.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "pending_corr" {
		t.Errorf("expected pending_corr, got %s", v.Status)
	}
}

func TestAlphaSelfCorrelation_LongPollThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/self" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "correlations_self.json"))
	})
	b, err := cl.AlphaSelfCorrelation(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaSelfCorrelation: %v", err)
	}
	if b.Max == nil || *b.Max != 0.6022 {
		t.Errorf("max wrong: %v", b.Max)
	}
	if b.Min == nil || *b.Min != -0.0206 {
		t.Errorf("min wrong: %v", b.Min)
	}
	if len(b.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(b.Records))
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 polls, got %d", calls.Load())
	}
}

func TestAlphaPnL_Warm(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "recordsets_pnl.json"))
	})
	s, err := cl.AlphaPnL(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaPnL: %v", err)
	}
	if len(s.Records) != 3 {
		t.Errorf("expected 3 points, got %d", len(s.Records))
	}
	if s.Records[0].Date != "2019-01-02" {
		t.Errorf("first date wrong: %v", s.Records[0])
	}
	if s.Records[0].Value != 30026.0 {
		t.Errorf("first value wrong: %v", s.Records[0])
	}
}

func TestAlphaPnL_ColdThenWarm(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 2 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "recordsets_pnl.json"))
	})
	s, err := cl.AlphaPnL(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaPnL: %v", err)
	}
	if len(s.Records) != 3 {
		t.Errorf("expected 3 points, got %d", len(s.Records))
	}
}

func TestListAlphas(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "UNSUBMITTED" {
			t.Errorf("status param missing: %v", q)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_alphas_page.json"))
	})
	page, err := cl.ListAlphas(context.Background(), brainapi.ListAlphasOptions{Status: "UNSUBMITTED"})
	if err != nil {
		t.Fatalf("ListAlphas: %v", err)
	}
	if page.Count != 2 || len(page.Results) != 2 {
		t.Errorf("wrong page: %+v", page)
	}
}

func TestSubmit_BudgetGate(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_pending.json"))
	})
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:      srv.URL,
		Timeout:      2 * time.Second,
		MaxLongPolls: 1,
		DailyBudget:  brainapi.DailyBudget{Submits: 1},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, _ = cl.SubmitAlpha(context.Background(), "a")
	_, err = cl.SubmitAlpha(context.Background(), "b")
	if err == nil {
		t.Fatal("expected ErrDailyBudgetExhausted on second call")
	}
}
