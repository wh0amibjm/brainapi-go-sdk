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
	for _, k := range []string{"testPeriod", "maxTrade", "maxPosition", "selectionHandling", "selectionLimit", "selection", "super"} {
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
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in %s", want, s)
		}
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
