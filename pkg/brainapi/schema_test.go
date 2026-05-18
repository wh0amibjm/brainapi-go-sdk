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
}
