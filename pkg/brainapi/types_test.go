package brainapi

import (
	"encoding/json"
	"math"
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
