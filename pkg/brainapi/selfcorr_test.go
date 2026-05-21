package brainapi

import (
	"math"
	"testing"
	"time"
)

// cumPnL builds a cumulative-PnL series whose successive diffs equal the given
// daily returns. The first point is the base (0); each subsequent date is one
// day later. So PnLToReturns(cumPnL(start, rs)) reproduces rs exactly.
func cumPnL(start time.Time, returns []float64) []PnLPoint {
	pts := make([]PnLPoint, len(returns)+1)
	pts[0] = PnLPoint{Date: start.Format("2006-01-02"), Value: 0}
	cum := 0.0
	for i, r := range returns {
		cum += r
		pts[i+1] = PnLPoint{Date: start.AddDate(0, 0, i+1).Format("2006-01-02"), Value: cum}
	}
	return pts
}

// varyingReturns is a non-constant daily-return pattern (non-zero variance, so
// Pearson is defined).
func varyingReturns(n int) []float64 {
	rs := make([]float64, n)
	for i := range rs {
		rs[i] = float64(i%7) - 3
	}
	return rs
}

func negate(rs []float64) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = -r
	}
	return out
}

func TestPearson(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	if got := pearson(a, a); math.Abs(got-1) > 1e-12 {
		t.Errorf("identical series: want 1, got %v", got)
	}
	b := []float64{5, 4, 3, 2, 1}
	if got := pearson(a, b); math.Abs(got+1) > 1e-12 {
		t.Errorf("reversed series: want -1, got %v", got)
	}
	flat := []float64{2, 2, 2, 2, 2}
	if got := pearson(a, flat); got != 0 {
		t.Errorf("zero-variance: want 0 (np.isnan guard), got %v", got)
	}
	if got := pearson(a, []float64{1, 2}); got != 0 {
		t.Errorf("length mismatch: want 0, got %v", got)
	}
}

func TestPnLToReturns(t *testing.T) {
	pts := []PnLPoint{
		{Date: "2020-01-03", Value: 30}, // out of order on purpose
		{Date: "2020-01-01", Value: 10},
		{Date: "2020-01-02", Value: 25},
	}
	rs := PnLToReturns(pts)
	// diffs: 25-10=15, 30-25=5 (first point dropped, sorted ascending)
	if len(rs) != 2 || rs[0].Date != "2020-01-02" || rs[0].Ret != 15 || rs[1].Ret != 5 {
		t.Fatalf("unexpected returns: %+v", rs)
	}

	// Forward-fill over a non-finite point: it is skipped without advancing
	// prev, so the next finite point diffs against the last good value.
	tainted := []PnLPoint{
		{Date: "2020-01-01", Value: 10},
		{Date: "2020-01-02", Value: math.Inf(1)},
		{Date: "2020-01-03", Value: 40},
	}
	rs = PnLToReturns(tainted)
	if len(rs) != 1 || rs[0].Date != "2020-01-03" || rs[0].Ret != 30 {
		t.Fatalf("forward-fill failed: %+v", rs)
	}

	if PnLToReturns([]PnLPoint{{Date: "2020-01-01", Value: 1}}) != nil {
		t.Error("single point should yield nil")
	}
}

func TestTrimPnLToYears(t *testing.T) {
	pts := []PnLPoint{
		{Date: "2018-01-01", Value: 1}, // > 4y before last → trimmed
		{Date: "2019-06-01", Value: 2},
		{Date: "2021-06-01", Value: 3},
		{Date: "2023-06-01", Value: 4},          // last
		{Date: "2022-01-01", Value: math.NaN()}, // dropped (non-finite)
	}
	out := TrimPnLToYears(pts, 4)
	// cutoff = 2023-06-01 minus 4y = 2019-06-01; keep dates >= cutoff.
	if len(out) != 3 || out[0].Date != "2019-06-01" || out[2].Date != "2023-06-01" {
		t.Fatalf("unexpected trim: %+v", out)
	}
}

func TestAlignReturns(t *testing.T) {
	a := make([]ReturnPoint, 0, 40)
	b := make([]ReturnPoint, 0, 40)
	for i := 0; i < 40; i++ {
		d := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02")
		a = append(a, ReturnPoint{Date: d, Ret: float64(i)})
		if i%2 == 0 { // b only covers even days → 20 overlap
			b = append(b, ReturnPoint{Date: d, Ret: float64(i)})
		}
	}
	if _, _, _, ok := alignReturns(a, b); ok {
		t.Error("20-day overlap is below min (30): want ok=false")
	}
	if _, _, overlap, ok := alignReturns(a, a); !ok || overlap != 40 {
		t.Errorf("full overlap: want ok=true overlap=40, got ok=%v overlap=%d", ok, overlap)
	}
}

func TestSelfCorrLocalSignedMaxAndExclude(t *testing.T) {
	start := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	base := varyingReturns(40)

	in := SelfCorrLocalInput{
		Candidate: AlphaPnL{ID: "CAND", Records: cumPnL(start, base)},
		Neighbours: []AlphaPnL{
			{ID: "POS", Records: cumPnL(start, base)},         // corr +1
			{ID: "NEG", Records: cumPnL(start, negate(base))}, // corr -1
			{ID: "CAND", Records: cumPnL(start, base)},        // excluded (== candidate id)
			{ID: "SHORT", Records: cumPnL(start, base[:10])},  // <30 overlap → skipped
		},
	}
	res := SelfCorrLocal(in)

	// excludeId drops the self entry: 3 neighbours considered.
	if res.Considered != 3 {
		t.Errorf("considered: want 3 (CAND excluded), got %d", res.Considered)
	}
	if res.Skipped != 1 {
		t.Errorf("skipped: want 1 (SHORT), got %d", res.Skipped)
	}
	// Signed max: |−1| ties |+1|; SliceStable keeps POS first only if it sorts
	// first by |corr|. Both are magnitude 1, so corrMax must be magnitude 1.
	if math.Abs(math.Abs(res.CorrMax)-1) > 1e-9 {
		t.Errorf("corrMax magnitude: want 1, got %v", res.CorrMax)
	}
	if len(res.Neighbours) != 2 {
		t.Fatalf("want 2 scored neighbours, got %d", len(res.Neighbours))
	}
	// Both scored neighbours should be at magnitude 1, one +1 one -1.
	var sawPos, sawNeg bool
	for _, n := range res.Neighbours {
		if n.Overlap != 40 {
			t.Errorf("neighbour %s overlap: want 40, got %d", n.ID, n.Overlap)
		}
		if math.Abs(n.Corr-1) < 1e-9 {
			sawPos = true
		}
		if math.Abs(n.Corr+1) < 1e-9 {
			sawNeg = true
		}
	}
	if !sawPos || !sawNeg {
		t.Errorf("want both a +1 and a -1 neighbour, got %+v", res.Neighbours)
	}
}

func TestSelfCorrLocalEmptyNeighbours(t *testing.T) {
	start := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	in := SelfCorrLocalInput{
		Candidate:  AlphaPnL{ID: "CAND", Records: cumPnL(start, varyingReturns(40))},
		Neighbours: nil,
	}
	res := SelfCorrLocal(in)
	if res.CorrMax != 0 || len(res.Neighbours) != 0 || res.Considered != 0 {
		t.Errorf("empty neighbours: want zero result, got %+v", res)
	}
}
