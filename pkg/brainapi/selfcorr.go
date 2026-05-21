package brainapi

// Local self-correlation Pearson computation. A pure-Go port of the
// the TypeScript client reference path (src/lib/the reference implementation), so any brainapi-go
// consumer can compute self-correlation OFFLINE — no BRAIN call — and:
//   (a) cross-check BRAIN's /alphas/{id}/correlations/self (catch endpoint drift),
//   (b) fall back when that endpoint is flaky,
//   (c) preview corr for a PnL series that is NOT YET a main-account alpha
//       (BRAIN's endpoint needs a real alphaId; this does not).
//
// Method (per WorldQuant consultant forum, validated to ~0 error vs BRAIN):
// Pearson over the trailing 4 years of daily PnL *returns*, on the
// date-intersection of the two series, within the same region. Constants are
// hardcoded to match the toolkit (NOT configurable, by design).
//
// This is the exact pairwise reference path. The toolkit also has a
// normalized-matrix fast path, but that is an in-process bulk-scoring
// optimization that returns identical values — irrelevant to a stateless
// one-shot computation, so it is intentionally not ported.
//
// Everything here is pure: no I/O, no network. Callers supply the candidate
// PnL and the neighbour set; sourcing/caching the neighbours is the caller's
// job (the SDK holds no state).

import (
	"math"
	"sort"
	"time"
)

const (
	// trailing window BRAIN's SELF_CORRELATION uses (consultant forum
	// post 35129651186967). Both candidate and neighbours are trimmed to it.
	corrTrimYears = 4
	// minimum date-overlap for a non-degenerate Pearson. Below this the
	// correlation is too noisy to trust; the neighbour is skipped rather
	// than risk a false-positive. 30 trading days ~= 1.5 calendar months.
	minOverlapDays = 30
	// default neighbour count reported in MaxSelfCorrResult.Neighbours.
	corrTopK = 5
)

// AlphaPnL is one alpha's id + cumulative-PnL series, as supplied to
// SelfCorrLocal. Records reuse the [date, value] tuple shape of PnLSeries.
type AlphaPnL struct {
	ID      string     `json:"id"`
	Records []PnLPoint `json:"records"`
}

// SelfCorrLocalInput is the JSON-in body for offline self-correlation:
// one candidate scored against a neighbour set.
type SelfCorrLocalInput struct {
	Candidate  AlphaPnL   `json:"candidate"`
	Neighbours []AlphaPnL `json:"neighbours"`
}

// ReturnPoint is a date-aligned daily return: ret = pnl_t - pnl_{t-1}.
type ReturnPoint struct {
	Date string
	Ret  float64
}

// NeighbourReturns is one neighbour's id + its precomputed daily-return
// series, as consumed by MaxSelfCorr.
type NeighbourReturns struct {
	ID      string
	Returns []ReturnPoint
}

// CorrNeighbour is one scored neighbour. Corr is signed; Overlap is the
// date-intersection window size (days) used to compute it.
type CorrNeighbour struct {
	ID      string  `json:"id"`
	Corr    float64 `json:"corr"`
	Overlap int     `json:"overlap"`
}

// MaxSelfCorrResult mirrors the toolkit's result. CorrMax is signed (BRAIN's
// `max` is signed too — a strongly negative corr is just as much of a problem
// as a positive one); Neighbours is ranked by |corr| descending.
type MaxSelfCorrResult struct {
	CorrMax    float64         `json:"corrMax"`
	Neighbours []CorrNeighbour `json:"neighbours"`
	// Considered is the neighbour count after excluding the candidate's own id.
	Considered int `json:"considered"`
	// Skipped is neighbours dropped for insufficient date-overlap (< 30 days).
	Skipped int `json:"skipped"`
}

// TrimPnLToYears trims a cumulative-PnL series to the trailing `years` years,
// measured from the last record's date. Output is sorted ascending by date;
// records with non-finite values are dropped. Mirrors trimPnlToYears: the
// per-record filter only checks the value is finite (the date is compared as
// a string), and the trailing-window cutoff is anchored on the parsed last
// date — an unparseable last date returns the cleaned-but-untrimmed series.
func TrimPnLToYears(records []PnLPoint, years int) []PnLPoint {
	clean := make([]PnLPoint, 0, len(records))
	for _, p := range records {
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		clean = append(clean, p)
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i].Date < clean[j].Date })
	if len(clean) == 0 {
		return clean
	}
	last, err := time.Parse("2006-01-02", clean[len(clean)-1].Date)
	if err != nil {
		return clean
	}
	cutoff := last.AddDate(-years, 0, 0).Format("2006-01-02")
	i := 0
	for i < len(clean) && clean[i].Date < cutoff {
		i++
	}
	return clean[i:]
}

// PnLToReturns converts a cumulative-PnL series to daily returns. Drops the
// first record (no prior to diff against) and forward-fills over non-finite
// values: a non-finite point is skipped WITHOUT advancing the prior, so the
// next finite point diffs against the last good value. Input need not be
// sorted; output is sorted ascending by date. Mirrors pnlSeriesToReturns.
func PnLToReturns(records []PnLPoint) []ReturnPoint {
	if len(records) < 2 {
		return nil
	}
	sorted := make([]PnLPoint, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	// Seed prev from the first finite value, skipping any leading non-finite
	// entries (a NaN-tainted seed would poison every later diff).
	var prev float64
	startIdx := -1
	for i, p := range sorted {
		if isFinite(p.Value) {
			prev = p.Value
			startIdx = i + 1
			break
		}
	}
	if startIdx == -1 {
		return nil
	}
	out := make([]ReturnPoint, 0, len(sorted))
	for i := startIdx; i < len(sorted); i++ {
		cur := sorted[i]
		if !isFinite(cur.Value) {
			continue // forward-fill: skip without advancing prev
		}
		out = append(out, ReturnPoint{Date: cur.Date, Ret: cur.Value - prev})
		prev = cur.Value
	}
	return out
}

// pearson is the Pearson correlation over two equal-length slices. Returns 0
// for empty/mismatched inputs, zero-variance inputs (BRAIN's np.isnan guard),
// or a non-finite result. Mirrors the reference implementation pearson.
func pearson(a, b []float64) float64 {
	n := len(a)
	if n == 0 || n != len(b) {
		return 0
	}
	var sumA, sumB float64
	for i := 0; i < n; i++ {
		sumA += a[i]
		sumB += b[i]
	}
	meanA := sumA / float64(n)
	meanB := sumB / float64(n)
	var num, varA, varB float64
	for i := 0; i < n; i++ {
		da := a[i] - meanA
		db := b[i] - meanB
		num += da * db
		varA += da * da
		varB += db * db
	}
	if varA == 0 || varB == 0 {
		return 0
	}
	r := num / math.Sqrt(varA*varB)
	if !isFinite(r) {
		return 0
	}
	return r
}

// alignReturns inner-joins two date-sorted return series and emits the aligned
// value slices. ok=false when the intersection is shorter than minOverlapDays
// (caller treats it as "neighbour skipped"). Mirrors alignReturns.
func alignReturns(a, b []ReturnPoint) (av, bv []float64, overlap int, ok bool) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i].Date == b[j].Date:
			av = append(av, a[i].Ret)
			bv = append(bv, b[j].Ret)
			i++
			j++
		case a[i].Date < b[j].Date:
			i++
		default:
			j++
		}
	}
	if len(av) < minOverlapDays {
		return nil, nil, 0, false
	}
	return av, bv, len(av), true
}

// MaxSelfCorr scores candidate returns against every neighbour and returns the
// worst (by |corr|) signed correlation plus the top-K neighbours. Neighbours
// with too little date-overlap are counted in Skipped. Mirrors maxSelfCorr.
func MaxSelfCorr(candidate []ReturnPoint, neighbours []NeighbourReturns, topK int) MaxSelfCorrResult {
	if topK <= 0 {
		topK = corrTopK
	}
	if len(candidate) < minOverlapDays || len(neighbours) == 0 {
		return MaxSelfCorrResult{Neighbours: []CorrNeighbour{}, Considered: len(neighbours)}
	}
	skipped := 0
	all := make([]CorrNeighbour, 0, len(neighbours))
	for _, nb := range neighbours {
		av, bv, overlap, ok := alignReturns(candidate, nb.Returns)
		if !ok {
			skipped++
			continue
		}
		all = append(all, CorrNeighbour{ID: nb.ID, Corr: pearson(av, bv), Overlap: overlap})
	}
	if len(all) == 0 {
		return MaxSelfCorrResult{Neighbours: []CorrNeighbour{}, Considered: len(neighbours), Skipped: skipped}
	}
	sort.SliceStable(all, func(i, j int) bool { return math.Abs(all[i].Corr) > math.Abs(all[j].Corr) })
	top := all
	if len(top) > topK {
		top = top[:topK]
	}
	return MaxSelfCorrResult{
		CorrMax:    all[0].Corr,
		Neighbours: top,
		Considered: len(neighbours),
		Skipped:    skipped,
	}
}

// SelfCorrLocal is the high-level entry: trims the candidate and every
// neighbour to the trailing 4y, converts to daily returns, excludes any
// neighbour sharing the candidate's id, and returns the max signed
// correlation. Mirrors the toolkit's scoreLocalCorrFromRecords + excludeId
// path. Pure — no I/O.
func SelfCorrLocal(in SelfCorrLocalInput) MaxSelfCorrResult {
	candidate := PnLToReturns(TrimPnLToYears(in.Candidate.Records, corrTrimYears))

	eligible := make([]NeighbourReturns, 0, len(in.Neighbours))
	for _, nb := range in.Neighbours {
		if in.Candidate.ID != "" && nb.ID == in.Candidate.ID {
			continue // excludeId: never correlate the candidate with itself
		}
		eligible = append(eligible, NeighbourReturns{
			ID:      nb.ID,
			Returns: PnLToReturns(TrimPnLToYears(nb.Records, corrTrimYears)),
		})
	}
	return MaxSelfCorr(candidate, eligible, corrTopK)
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
