package brainapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

func TestGetSuperAlphaPreservesExpressionLegs(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/QPVEedzK" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "alpha_super_detail.json"))
	})
	a, err := cl.GetAlpha(context.Background(), "QPVEedzK")
	if err != nil {
		t.Fatalf("GetAlpha: %v", err)
	}
	for leg, raw := range map[string]json.RawMessage{
		"selection": a.Selection,
		"combo":     a.Combo,
	} {
		var expression struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &expression); err != nil {
			t.Fatalf("decode %s: %v (%s)", leg, err, raw)
		}
		if expression.Code == "" || len(expression.Description) < 100 {
			t.Errorf("%s expression not preserved: %+v", leg, expression)
		}
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

	// A WARNING check (REVERSION_COMPONENT) carries a human-readable Message and
	// no numeric limit/value — make sure both decode rather than getting dropped.
	var sawWarning bool
	for _, c := range is.Checks {
		if c.Result != "WARNING" {
			continue
		}
		sawWarning = true
		if c.Name != "REVERSION_COMPONENT" || c.Message == "" {
			t.Errorf("WARNING check = %+v, want REVERSION_COMPONENT with non-empty Message", c)
		}
		if c.Limit != nil || c.Value != nil {
			t.Errorf("WARNING check should have nil limit/value, got %+v", c)
		}
	}
	if !sawWarning {
		t.Error("expected a WARNING check in the fixture")
	}
}

func TestCheckSuperAlphaDescriptionsPass(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "check_super_descriptions_terminal.json"))
	})
	is, err := cl.CheckAlpha(context.Background(), "QPVEedzK")
	if err != nil {
		t.Fatalf("CheckAlpha: %v", err)
	}
	var superSubmissionPass bool
	for _, check := range is.Checks {
		if strings.HasSuffix(check.Name, "DESCRIPTION_LENGTH") {
			t.Errorf("post-PATCH description failure still present: %+v", check)
		}
		if check.Name == "SUPER_SUBMISSION" && check.Result == "PASS" {
			superSubmissionPass = true
		}
	}
	if !superSubmissionPass {
		t.Errorf("SUPER_SUBMISSION PASS missing from %+v", is.Checks)
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

func TestSubmitAlpha_Verified(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_verified.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "verified" {
		t.Errorf("expected verified, got %s (reason=%s)", v.Status, v.Reason)
	}
}

// BRAIN signals "submission still processing" with a 303 See Other back to the
// submit URL. Redirect-following is disabled (WithNotFollowRedirects) precisely
// so this surfaces as a keep-polling tick rather than an attempted http://:443
// follow. This exercises both the parseSubmitVerdict 3xx branch and evaluate's
// accept503+3xx branch end to end; the err==nil + verified assertions guard
// against a regression that re-enabled redirect-following or reclassified the
// 303 as terminal-failure.
func TestSubmitAlpha_303KeepsPolling(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		// POST submit and the first GET poll both 303 "still processing".
		if n < 3 {
			w.Header().Set("Retry-After", "0.01")
			w.Header().Set("Location", r.URL.String()) // decorative; must NOT be followed
			w.WriteHeader(http.StatusSeeOther)         // 303
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_verified.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("a 303 must keep polling, never surface as an error: %v", err)
	}
	if v.Status != "verified" {
		t.Errorf("expected verified after 303 keep-polling, got %s (reason=%s)", v.Status, v.Reason)
	}
}

// A 2xx whose SELF_CORRELATION check is not yet attached (the deterministic
// gates land in the body before corr is computed) must NOT be reported verified
// — corr is the whole point of the submit long-poll. Keep polling → pending_corr.
func TestSubmitAlpha_CorrAbsentKeepsPolling(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_corr_absent.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "pending_corr" {
		t.Errorf("expected pending_corr (corr absent → keep polling), got %s", v.Status)
	}
}

// A SELF_CORRELATION result of ERROR is not a pass and must not be reported verified.
func TestSubmitAlpha_CorrErrorKeepsPolling(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "submit_200_corr_error.json"))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "pending_corr" {
		t.Errorf("expected pending_corr (corr ERROR → keep polling), got %s", v.Status)
	}
}

// A 503 carrying a non-empty body (no `is`) is still BRAIN's "queued" signal —
// keep polling, not submit_failed.
func TestSubmitAlpha_503WithBodyKeepsPolling(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"detail":"queued"}`))
	})
	v, err := cl.SubmitAlpha(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("SubmitAlpha: %v", err)
	}
	if v.Status != "pending_corr" {
		t.Errorf("expected pending_corr (503+body → keep polling), got %s (reason=%s)", v.Status, v.Reason)
	}
}

// SetAlphaProperties issues a PATCH /alphas/{id} whose body contains ONLY the
// fields the caller set (omitempty), and parses the 200 response as *Alpha. This
// pins both the wire shape (method, path, selective body) and the decode.
func TestSetAlphaProperties(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	var gotBody []byte
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody = drainBodyReq(t, r)
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "alpha_detail.json"))
	})
	desc := strings.Repeat("Idea and rationale for this power pool alpha. ", 3) // >100 chars
	selectionDesc := strings.Repeat("Select differentiated active alphas. ", 4)
	comboDesc := strings.Repeat("Combine every selected alpha with equal weight. ", 3)
	a, err := cl.SetAlphaProperties(context.Background(), "qMPjAxnO", brainapi.AlphaProperties{
		Regular:   &brainapi.AlphaRegularProperties{Description: &desc},
		Selection: &brainapi.AlphaExpressionProperties{Description: &selectionDesc},
		Combo:     &brainapi.AlphaExpressionProperties{Description: &comboDesc},
		Tags:      []string{"PowerPoolSelected"},
	})
	if err != nil {
		t.Fatalf("SetAlphaProperties: %v", err)
	}
	// (a) PATCH method + (b) path.
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/alphas/qMPjAxnO" {
		t.Errorf("path = %s, want /alphas/qMPjAxnO", gotPath)
	}
	// (e) 200 body parsed into *Alpha.
	if a == nil || a.ID != "qMPjAxnO" {
		t.Errorf("response decode wrong: %+v", a)
	}

	// (b) body nests description under `regular` (VERIFIED live: a top-level
	// "description" is rejected 400 "Unexpected property."); (c) tags serialize
	// as a top-level JSON array; unset fields (name/color/category) must be
	// ABSENT (omitempty).
	var m map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &m); err != nil {
		t.Fatalf("body not JSON object: %v (%s)", err, gotBody)
	}
	if _, present := m["description"]; present {
		t.Errorf("description must NOT be top-level (rejected 400 by BRAIN), body: %s", gotBody)
	}
	regularRaw, ok := m["regular"]
	if !ok {
		t.Fatalf("body missing regular (description home): %s", gotBody)
	}
	var regular struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(regularRaw, &regular); err != nil {
		t.Fatalf("regular not an object: %v (%s)", err, regularRaw)
	}
	if regular.Description != desc {
		t.Errorf("regular.description = %q, want %q", regular.Description, desc)
	}
	for leg, want := range map[string]string{
		"selection": selectionDesc,
		"combo":     comboDesc,
	} {
		raw, ok := m[leg]
		if !ok {
			t.Fatalf("body missing %s description: %s", leg, gotBody)
		}
		var got struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s not an object: %v (%s)", leg, err, raw)
		}
		if got.Description != want {
			t.Errorf("%s.description = %q, want %q", leg, got.Description, want)
		}
	}
	tagsRaw, ok := m["tags"]
	if !ok {
		t.Fatalf("body missing tags: %s", gotBody)
	}
	var tags []string
	if err := json.Unmarshal(tagsRaw, &tags); err != nil {
		t.Fatalf("tags not a JSON array: %v (%s)", err, tagsRaw)
	}
	if len(tags) != 1 || tags[0] != "PowerPoolSelected" {
		t.Errorf("tags = %v, want [PowerPoolSelected]", tags)
	}
	for _, unset := range []string{"name", "color", "category"} {
		if _, present := m[unset]; present {
			t.Errorf("unset field %q must be omitted, but body has it: %s", unset, gotBody)
		}
	}
}

// (d) An empty alpha id is rejected locally without hitting the server.
func TestSetAlphaProperties_EmptyID(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit for empty id")
		w.WriteHeader(500)
	})
	if _, err := cl.SetAlphaProperties(context.Background(), "", brainapi.AlphaProperties{}); err == nil {
		t.Error("empty alpha id: want error")
	}
}

// An empty AlphaProperties (nothing set) marshals to `{}` — a no-op partial
// PATCH — with no phantom fields leaking in from zero values.
func TestSetAlphaProperties_OmitsAllUnset(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = drainBodyReq(t, r)
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "alpha_detail.json"))
	})
	if _, err := cl.SetAlphaProperties(context.Background(), "qMPjAxnO", brainapi.AlphaProperties{}); err != nil {
		t.Fatalf("SetAlphaProperties: %v", err)
	}
	if strings.TrimSpace(string(gotBody)) != "{}" {
		t.Errorf("empty props must marshal to {}, got %s", gotBody)
	}
}

func TestSetAlphaOsmosisPoints_SetAndClear(t *testing.T) {
	t.Parallel()
	var bodies [][]byte
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/alphas/qMPjAxnO" {
			t.Errorf("request = %s %s, want PATCH /alphas/qMPjAxnO", r.Method, r.URL.Path)
		}
		bodies = append(bodies, drainBodyReq(t, r))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(loadFixture(t, "alpha_osmosis_detail.json"))
	})
	points := 10000
	a, err := cl.SetAlphaOsmosisPoints(context.Background(), "qMPjAxnO", &points)
	if err != nil {
		t.Fatalf("set Osmosis points: %v", err)
	}
	if a.OsmosisPoints == nil || *a.OsmosisPoints != 10000 {
		t.Fatalf("decoded Osmosis points = %v, want 10000", a.OsmosisPoints)
	}
	if _, err := cl.SetAlphaOsmosisPoints(context.Background(), "qMPjAxnO", nil); err != nil {
		t.Fatalf("clear Osmosis points: %v", err)
	}
	if got := strings.TrimSpace(string(bodies[0])); got != `{"osmosisPoints":10000}` {
		t.Errorf("set body = %s", got)
	}
	if got := strings.TrimSpace(string(bodies[1])); got != `{"osmosisPoints":null}` {
		t.Errorf("clear body = %s", got)
	}
}

func TestSetAlphaOsmosisPoints_RejectsRangeLocally(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit for invalid Osmosis points")
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, points := range []int{0, 100001} {
		if _, err := cl.SetAlphaOsmosisPoints(context.Background(), "qMPjAxnO", &points); err == nil {
			t.Errorf("points=%d: want validation error", points)
		}
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

func TestAlphaSelfCorrelation_LongPoll200EmptyThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/self" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(200) // 200 + empty body = "still computing" (TUTORIAL-tier signal)
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

func TestAlphaProdCorrelation_LongPollThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/prod" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(503) // "still computing" (Consultant-tier signal)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "correlations_prod.json"))
	})
	b, err := cl.AlphaProdCorrelation(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaProdCorrelation: %v", err)
	}
	if b.Max == nil || *b.Max != 0.8849 {
		t.Errorf("max wrong: %v", b.Max)
	}
	if b.Min == nil || *b.Min != -0.8745 {
		t.Errorf("min wrong: %v", b.Min)
	}
	if len(b.Records) != 3 {
		t.Errorf("expected 3 histogram records, got %d", len(b.Records))
	}
	if b.Schema == nil || b.Schema.Name != "prodCorrelation" {
		t.Errorf("schema name wrong: %+v", b.Schema)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 polls, got %d", calls.Load())
	}
}

func TestAlphaProdCorrelation_LongPoll200EmptyThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/prod" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(200) // 200 + empty body = "still computing"
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "correlations_prod.json"))
	})
	b, err := cl.AlphaProdCorrelation(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaProdCorrelation: %v", err)
	}
	if b.Max == nil || *b.Max != 0.8849 {
		t.Errorf("max wrong: %v", b.Max)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 polls, got %d", calls.Load())
	}
}

func TestAlphaPowerPoolCorrelation_LongPollThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/power-pool" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(200) // 200 + Retry-After + empty body = "still computing" (same as self/prod)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "correlations_power_pool.json"))
	})
	b, err := cl.AlphaPowerPoolCorrelation(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaPowerPoolCorrelation: %v", err)
	}
	// Live probe (2026-07-02): shape is the selfCorrelation per-alpha form, not
	// the prod histogram.
	if b.Schema == nil || b.Schema.Name != "selfCorrelation" {
		t.Errorf("schema name wrong: %+v", b.Schema)
	}
	if b.Max == nil || *b.Max != 0.4821 {
		t.Errorf("max wrong: %v", b.Max)
	}
	if b.Min == nil || *b.Min != -0.0206 {
		t.Errorf("min wrong: %v", b.Min)
	}
	if len(b.Records) != 2 {
		t.Errorf("expected 2 per-alpha records, got %d", len(b.Records))
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 polls, got %d", calls.Load())
	}
}

// A fresh Power-Pool account has no comparable PP alpha: records=[] and
// max=null. The decode must succeed and leave Max nil (NOT 0) so consumers can
// fail-open on the empty pool.
func TestAlphaPowerPoolCorrelation_EmptyPoolNullMax(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/qMPjAxnO/correlations/power-pool" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "correlations_power_pool_empty.json"))
	})
	b, err := cl.AlphaPowerPoolCorrelation(context.Background(), "qMPjAxnO")
	if err != nil {
		t.Fatalf("AlphaPowerPoolCorrelation: %v", err)
	}
	if b.Max != nil {
		t.Errorf("empty-pool Max must decode as nil (fail-open), got %v", *b.Max)
	}
	if b.Min != nil {
		t.Errorf("empty-pool Min must decode as nil, got %v", *b.Min)
	}
	if len(b.Records) != 0 {
		t.Errorf("expected 0 records on empty pool, got %d", len(b.Records))
	}
}

func TestAlphaRecordSet_LongPollThenTerminal(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/aX/recordsets/yearly-stats" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		n := calls.Add(1)
		if n < 2 {
			w.Header().Set("Retry-After", "0.02")
			w.WriteHeader(200) // still-computing
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"schema":{"name":"yearly-stats","properties":[{"name":"year"},{"name":"sharpe"}]},"records":[[2023,1.5],[2024,1.7]]}`))
	})
	b, err := cl.AlphaRecordSet(context.Background(), "aX", "yearly-stats")
	if err != nil {
		t.Fatalf("AlphaRecordSet: %v", err)
	}
	if b.Schema == nil || b.Schema.Name != "yearly-stats" || len(b.Records) != 2 {
		t.Errorf("recordset decode wrong: %+v", b)
	}
}

func TestAlphaRecordSet_RequiresArgs(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit for empty args")
		w.WriteHeader(500)
	})
	if _, err := cl.AlphaRecordSet(context.Background(), "", "x"); err == nil {
		t.Error("empty alpha id: want error")
	}
	if _, err := cl.AlphaRecordSet(context.Background(), "a", ""); err == nil {
		t.Error("empty name: want error")
	}
}

func TestAlphaRecordSets_PassThrough(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alphas/aX/recordsets" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`[{"name":"pnl"},{"name":"yearly-stats"}]`))
	})
	raw, err := cl.AlphaRecordSets(context.Background(), "aX")
	if err != nil {
		t.Fatalf("AlphaRecordSets: %v", err)
	}
	if !strings.Contains(string(raw), "yearly-stats") {
		t.Errorf("raw pass-through wrong: %s", raw)
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

// ListAlphas threads BRAIN comparison filters onto the query VERBATIM (operator
// embedded in the field token, percent-encoded), AND-combining multiple filters
// alongside status/order. Verified against the live endpoint 2026-06-07: the
// DRF "__gte" form 400s there, so the SDK must emit "is.sharpe%3E%3D1.25", not
// "is.sharpe__gte=1.25". This pins the wire format so a refactor can't regress it.
func TestListAlphas_Filters(t *testing.T) {
	t.Parallel()
	var rawQuery string
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/self/alphas" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		rawQuery = r.URL.RawQuery
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"count":0,"next":null,"previous":null,"results":[]}`))
	})
	_, err := cl.ListAlphas(context.Background(), brainapi.ListAlphasOptions{
		Status:  "ACTIVE",
		Filters: []string{"is.sharpe>=1.25", "is.turnover<=0.7", ""},
	})
	if err != nil {
		t.Fatalf("ListAlphas: %v", err)
	}
	for _, want := range []string{
		"status=ACTIVE",
		"is.sharpe%3E%3D1.25",  // ">=" encoded, no key/value "=" separator
		"is.turnover%3C%3D0.7", // "<=" encoded
	} {
		if !strings.Contains(rawQuery, want) {
			t.Errorf("raw query %q missing %q", rawQuery, want)
		}
	}
	// The empty filter element must be dropped (no stray "&&" / trailing "&").
	if strings.Contains(rawQuery, "&&") {
		t.Errorf("raw query has empty fragment: %q", rawQuery)
	}
}
