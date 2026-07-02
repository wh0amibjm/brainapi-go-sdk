package brainapi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// A null value (BRAIN's gap-day marker) must decode to a non-finite sentinel
// without erroring, and re-encode back to null (json.Marshal rejects NaN, so a
// naive round-trip would break the `alphas pnl` output).
func TestPnLPoint_NullValueRoundTrip(t *testing.T) {
	var p PnLPoint
	if err := json.Unmarshal([]byte(`["2024-01-02",null]`), &p); err != nil {
		t.Fatalf("unmarshal null value: %v", err)
	}
	if p.Date != "2024-01-02" || isFinite(p.Value) {
		t.Fatalf("expected date set + non-finite value, got %q / %v", p.Date, p.Value)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal non-finite value: %v", err)
	}
	if string(out) != `["2024-01-02",null]` {
		t.Errorf("expected null re-encode, got %s", out)
	}
}

// The new CONSULTANT-era SimSettings fields are all omitempty: a
// SimulationRequest that predates them must still marshal to the SAME bytes it
// did before (no stray "testPeriod":"" / "maxTrade":"" keys), preserving the
// byte-for-byte wire contract older callers depend on.
func TestSimSettings_NewFieldsOmitemptyRoundTrip(t *testing.T) {
	req := SimulationRequest{
		Type:    "REGULAR",
		Regular: "close",
		Settings: SimSettings{
			InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000",
			Delay: 1, Decay: 12, Neutralization: "SUBINDUSTRY",
			Truncation: 0.02, Pasteurization: "ON", UnitHandling: "VERIFY",
			NanHandling: "OFF", Language: "FASTEXPR",
		},
	}
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	for _, k := range []string{"testPeriod", "maxTrade", "maxPosition", "selectionHandling", "selectionLimit", "selection", "super", "componentActivation"} {
		if strings.Contains(s, `"`+k+`"`) {
			t.Errorf("omitted field %q leaked into wire form: %s", k, s)
		}
	}
	// A full round-trip must reproduce the same struct.
	var back SimulationRequest
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out2, _ := json.Marshal(back)
	if string(out2) != s {
		t.Errorf("round-trip changed bytes:\n  %s\n  %s", s, out2)
	}
}

// When the new fields ARE set they emit under their exact BRAIN key names.
func TestSimSettings_NewFieldsEmitWhenSet(t *testing.T) {
	req := SimulationRequest{
		Type:      "SUPER",
		Super:     "expr",
		Selection: "sel_expr",
		Settings: SimSettings{
			InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1,
			TestPeriod: "P2Y", MaxTrade: "ON", MaxPosition: "OFF",
			SelectionHandling: "NON_ZERO", SelectionLimit: 100,
			ComponentActivation: "OS",
		},
	}
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`"testPeriod":"P2Y"`, `"maxTrade":"ON"`, `"maxPosition":"OFF"`,
		`"selectionHandling":"NON_ZERO"`, `"selectionLimit":100`, `"selection":"sel_expr"`,
		`"componentActivation":"OS"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in %s", want, s)
		}
	}
}

// Dataset decodes the consultant Value Score under any of its plausible key
// aliases (valueScore / value_score) into a non-nil pointer, and leaves it nil
// when the payload omits it (non-consultant tier).
func TestDataset_ValueScoreAliasDecode(t *testing.T) {
	var pages DatasetsPage
	body := `{"count":3,"results":[
		{"id":"a","name":"A","valueScore":8.7,"fieldCount":10},
		{"id":"b","name":"B","value_score":9.5,"valueFieldCount":42},
		{"id":"c","name":"C","alphaCount":50000}
	]}`
	if err := json.Unmarshal([]byte(body), &pages); err != nil {
		t.Fatalf("unmarshal datasets: %v", err)
	}
	if len(pages.Results) != 3 {
		t.Fatalf("want 3 datasets, got %d", len(pages.Results))
	}
	if pages.Results[0].ValueScore == nil || *pages.Results[0].ValueScore != 8.7 {
		t.Errorf("valueScore alias not decoded: %+v", pages.Results[0])
	}
	if pages.Results[0].FieldCount == nil || *pages.Results[0].FieldCount != 10 {
		t.Errorf("fieldCount not decoded: %+v", pages.Results[0])
	}
	if pages.Results[1].ValueScore == nil || *pages.Results[1].ValueScore != 9.5 {
		t.Errorf("value_score alias not decoded: %+v", pages.Results[1])
	}
	if pages.Results[1].FieldCount == nil || *pages.Results[1].FieldCount != 42 {
		t.Errorf("valueFieldCount alias not decoded: %+v", pages.Results[1])
	}
	if pages.Results[2].ValueScore != nil {
		t.Errorf("absent valueScore should stay nil, got %v", *pages.Results[2].ValueScore)
	}
}

// Theme decodes the multiplier from its plausible aliases and preserves the raw
// object for later field extraction.
func TestTheme_MultiplierAliasAndRaw(t *testing.T) {
	var themes []Theme
	body := `[{"id":"t1","name":"USA","multiplier":3},{"id":"t2","name":"DS","qualityFactorMultiplier":5}]`
	if err := json.Unmarshal([]byte(body), &themes); err != nil {
		t.Fatalf("unmarshal themes: %v", err)
	}
	if len(themes) != 2 {
		t.Fatalf("want 2 themes, got %d", len(themes))
	}
	if themes[0].Multiplier == nil || *themes[0].Multiplier != 3 {
		t.Errorf("multiplier not decoded: %+v", themes[0])
	}
	if themes[1].Multiplier == nil || *themes[1].Multiplier != 5 {
		t.Errorf("qualityFactorMultiplier alias not decoded: %+v", themes[1])
	}
	if len(themes[1].Raw) == 0 {
		t.Errorf("Raw fallback not populated")
	}
}

// A single null inside a series must not fail the whole decode (the bug: one
// gap day aborted the entire AlphaPnL parse).
func TestPnLSeries_GapDoesNotFailSeries(t *testing.T) {
	var pts []PnLPoint
	body := `[["2024-01-01",100.5],["2024-01-02",null],["2024-01-03",101.0]]`
	if err := json.Unmarshal([]byte(body), &pts); err != nil {
		t.Fatalf("series with a gap should decode, got: %v", err)
	}
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if !math.IsNaN(pts[1].Value) {
		t.Errorf("gap point should be NaN, got %v", pts[1].Value)
	}
	if pts[0].Value != 100.5 || pts[2].Value != 101.0 {
		t.Errorf("finite points corrupted: %v %v", pts[0].Value, pts[2].Value)
	}
}

// BRAIN returns a check's limit/value as a number for threshold checks but as a
// STRING for categorical ones (verified live 2026-07-02:
// HT_ORTHOGONAL_RAM_NEUTRALIZATION → {"limit":"RAM","value":"Subindustry"}). A
// single string scalar must NOT fail the decode of the surrounding checks array
// (which would sink the whole Alpha / GET / list / set-properties response).
func TestCheck_LimitValueNumberOrString(t *testing.T) {
	body := []byte(`[
		{"name":"LOW_SHARPE","result":"FAIL","limit":1.58,"value":-1.7},
		{"name":"HT_ORTHOGONAL_RAM_NEUTRALIZATION","result":"WARNING","limit":"RAM","value":"Subindustry"},
		{"name":"HT_LIQUID_TOP200_SHARPE","result":"WARNING","limit":1,"value":-1.01},
		{"name":"SELF_CORRELATION","result":"PENDING"},
		{"name":"NUMERIC_STRING","result":"PASS","limit":"0.75","value":"2"}
	]`)
	var checks []Check
	if err := json.Unmarshal(body, &checks); err != nil {
		t.Fatalf("string-valued limit/value must not fail the decode: %v", err)
	}
	if len(checks) != 5 {
		t.Fatalf("want 5 checks, got %d", len(checks))
	}
	// (a) numeric limit/value parse through.
	if checks[0].Limit == nil || *checks[0].Limit != 1.58 || checks[0].Value == nil || *checks[0].Value != -1.7 {
		t.Errorf("LOW_SHARPE numeric parse wrong: limit=%v value=%v", checks[0].Limit, checks[0].Value)
	}
	// (b) a non-numeric categorical string leaves the pointers nil (no threshold),
	// while name/result still decode.
	if checks[1].Result != "WARNING" {
		t.Errorf("RAM check result wrong: %q", checks[1].Result)
	}
	if checks[1].Limit != nil || checks[1].Value != nil {
		t.Errorf("categorical string limit/value must be nil, got limit=%v value=%v", checks[1].Limit, checks[1].Value)
	}
	// (c) an integer scalar parses to float.
	if checks[2].Limit == nil || *checks[2].Limit != 1 {
		t.Errorf("integer limit parse wrong: %v", checks[2].Limit)
	}
	// (d) absent limit/value stay nil.
	if checks[3].Limit != nil || checks[3].Value != nil {
		t.Errorf("absent limit/value must be nil, got limit=%v value=%v", checks[3].Limit, checks[3].Value)
	}
	// (e) a NUMERIC string is rescued into the float (e.g. "0.75" → 0.75).
	if checks[4].Limit == nil || *checks[4].Limit != 0.75 || checks[4].Value == nil || *checks[4].Value != 2 {
		t.Errorf("numeric-string parse wrong: limit=%v value=%v", checks[4].Limit, checks[4].Value)
	}
}
