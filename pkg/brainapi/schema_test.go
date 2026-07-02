package brainapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestOperators_BareArray(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "operators.json"))
	})
	ops, err := cl.Operators(context.Background())
	if err != nil {
		t.Fatalf("Operators: %v", err)
	}
	if len(ops) != 2 || ops[0].Name != "add" || ops[1].Name != "ts_rank" {
		t.Errorf("unexpected operators: %+v", ops)
	}
}

func TestDataFields_RequiresQueryParams(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit; SDK must reject before dispatch")
	})
	_, err := cl.DataFields(context.Background(), brainapi.DataFieldsQuery{})
	if err == nil {
		t.Fatal("expected pre-dispatch error for missing required params")
	}
}

func TestDataFields_HappyPath(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, k := range []string{"instrumentType", "region", "universe", "delay"} {
			if q.Get(k) == "" {
				t.Errorf("missing required param %q", k)
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "data_fields_page.json"))
	})
	page, err := cl.DataFields(context.Background(), brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY",
		Region:         "USA",
		Universe:       "TOP3000",
		Delay:          1,
	})
	if err != nil {
		t.Fatalf("DataFields: %v", err)
	}
	if page.Count != 5905 || len(page.Results) != 1 {
		t.Errorf("wrong page: %+v", page)
	}
	if page.Results[0].ID != "abnormal_return_earnings_release" {
		t.Errorf("first field wrong: %+v", page.Results[0])
	}
	if page.Results[0].DateCreated != "2026-03-01" {
		t.Errorf("dateCreated not decoded: %+v", page.Results[0])
	}
}

func TestDatasets_RequiresQueryParams(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit; SDK must reject before dispatch")
	})
	_, err := cl.Datasets(context.Background(), brainapi.DataFieldsQuery{})
	if err == nil {
		t.Fatal("expected pre-dispatch error for missing required params")
	}
}

func TestDatasets_HappyPath(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data-sets" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		for _, k := range []string{"instrumentType", "region", "universe", "delay"} {
			if q.Get(k) == "" {
				t.Errorf("missing required param %q", k)
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "data_sets_page.json"))
	})
	page, err := cl.Datasets(context.Background(), brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1,
	})
	if err != nil {
		t.Fatalf("Datasets: %v", err)
	}
	if page.Count != 3 || len(page.Results) != 3 {
		t.Fatalf("wrong page: count=%d results=%d", page.Count, len(page.Results))
	}
	if page.Results[0].ValueScore == nil || *page.Results[0].ValueScore != 8.7 {
		t.Errorf("valueScore not decoded on first dataset: %+v", page.Results[0])
	}
	if page.Results[0].PyramidMultiplier == nil || *page.Results[0].PyramidMultiplier != 3.0 {
		t.Errorf("pyramidMultiplier not decoded: %+v", page.Results[0])
	}
	if page.Results[1].ValueScore == nil || *page.Results[1].ValueScore != 9.5 {
		t.Errorf("value_score alias not decoded: %+v", page.Results[1])
	}
	if page.Results[2].ValueScore != nil {
		t.Errorf("dataset with no valueScore should stay nil: %+v", page.Results[2])
	}
}

func TestThemes_HappyPath(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/themes" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "themes.json"))
	})
	themes, err := cl.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(themes) != 2 {
		t.Fatalf("want 2 themes, got %d", len(themes))
	}
	if themes[0].Multiplier == nil || *themes[0].Multiplier != 3.0 {
		t.Errorf("theme multiplier not decoded: %+v", themes[0])
	}
	if themes[1].Multiplier == nil || *themes[1].Multiplier != 5.0 {
		t.Errorf("qualityFactorMultiplier alias not decoded: %+v", themes[1])
	}
}

// Themes accepts a {results:[...]} envelope as well as a bare array.
func TestThemes_ResultsEnvelope(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"count":1,"results":[{"id":"t","name":"X","multiplier":2}]}`))
	})
	themes, err := cl.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes envelope: %v", err)
	}
	if len(themes) != 1 || themes[0].Multiplier == nil || *themes[0].Multiplier != 2 {
		t.Errorf("envelope form not decoded: %+v", themes)
	}
}
