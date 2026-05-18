package brainapi_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestListAlphasAll_DrainAllPages(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.WriteHeader(200)
		// Page 1: 2 results with non-empty next; page 2: 1 result with null next.
		if n == 1 {
			if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "0" {
				t.Errorf("expected offset=0 on first page, got %q", r.URL.Query().Get("offset"))
			}
			_, _ = w.Write([]byte(`{"count":3,"next":"http://x/?offset=2","previous":null,"results":[{"id":"a"},{"id":"b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"count":3,"next":null,"previous":null,"results":[{"id":"c"}]}`))
	})

	out, errs := cl.ListAlphasAll(context.Background(), brainapi.ListAlphasOptions{Limit: 2})
	var seen []string
	for out != nil || errs != nil {
		select {
		case a, ok := <-out:
			if !ok {
				out = nil
				continue
			}
			seen = append(seen, a.ID)
		case e, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if e != nil {
				t.Fatalf("paginate: %v", e)
			}
		}
	}
	if len(seen) != 3 || seen[0] != "a" || seen[2] != "c" {
		t.Errorf("expected [a b c], got %v", seen)
	}
}

func TestListAlphasAll_PropagatesError(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	})
	_, errs := cl.ListAlphasAll(context.Background(), brainapi.ListAlphasOptions{})
	var got error
	for e := range errs {
		if e != nil {
			got = e
		}
	}
	if got == nil {
		t.Fatal("expected error to surface")
	}
}

func TestDataFieldsAll_DrainsUntilCount(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.WriteHeader(200)
		if n == 1 {
			_, _ = w.Write([]byte(`{"count":3,"results":[{"id":"f1"},{"id":"f2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"count":3,"results":[{"id":"f3"}]}`))
	})

	out, errs := cl.DataFieldsAll(context.Background(), brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1, Limit: 2,
	})
	var seen []string
	for out != nil || errs != nil {
		select {
		case f, ok := <-out:
			if !ok {
				out = nil
				continue
			}
			seen = append(seen, f.ID)
		case e, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if e != nil {
				t.Fatalf("paginate: %v", e)
			}
		}
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 fields, got %d (%v)", len(seen), seen)
	}
}

func TestAlphaCheckBody_DerivesFailList(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "check_alpha_terminal.json"))
	})
	pass, fails, err := cl.AlphaCheckBody(context.Background(), "abc")
	if err != nil {
		t.Fatalf("AlphaCheckBody: %v", err)
	}
	if pass {
		t.Error("expected pass=false")
	}
	if len(fails) < 2 {
		t.Errorf("expected at least LOW_SHARPE+LOW_FITNESS in fails, got %v", fails)
	}
}
