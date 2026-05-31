package brainapi_test

import (
	"context"
	"net/http"
	"testing"
)

func TestBeforeAndAfterPerformance(t *testing.T) {
	t.Parallel()
	var gotPath string
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "before_and_after_performance.json"))
	})

	p, err := cl.BeforeAndAfterPerformance(context.Background(), "IQC2026S2", "qMgwmK2j")
	if err != nil {
		t.Fatalf("BeforeAndAfterPerformance: %v", err)
	}

	if want := "/competitions/IQC2026S2/alphas/qMgwmK2j/before-and-after-performance"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if p.Score.Before != 9566 || p.Score.After != 9662 {
		t.Errorf("score = %+v, want {before:9566 after:9662}", p.Score)
	}
	if p.Stats.Before.Fitness != 3.79 || p.Stats.After.Sharpe != 4.38 {
		t.Errorf("stats before.fitness=%v after.sharpe=%v", p.Stats.Before.Fitness, p.Stats.After.Sharpe)
	}
	// yearly-stats decode as positional tuples on each side
	if p.YearlyStats.Before.Schema == nil || len(p.YearlyStats.Before.Schema.Properties) != 12 {
		t.Errorf("yearly before schema props = %v", p.YearlyStats.Before.Schema)
	}
	if len(p.YearlyStats.Before.Records) != 2 || len(p.YearlyStats.After.Records) != 2 {
		t.Errorf("yearly records before=%d after=%d", len(p.YearlyStats.Before.Records), len(p.YearlyStats.After.Records))
	}
	// pnl block columns: date, beforePnL, afterPnL
	if p.PnL.Schema == nil || len(p.PnL.Schema.Properties) != 3 {
		t.Fatalf("pnl schema props = %v", p.PnL.Schema)
	}
	if p.PnL.Schema.Properties[1].Name != "beforePnL" {
		t.Errorf("pnl col[1] = %q, want beforePnL", p.PnL.Schema.Properties[1].Name)
	}
	if len(p.PnL.Records) != 2 {
		t.Errorf("pnl records = %d, want 2", len(p.PnL.Records))
	}
	if p.PartitionName != "EQUITY:1" {
		t.Errorf("partitionName = %q", p.PartitionName)
	}
}

func TestBeforeAndAfterPerformance_RequiresIDs(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit when an id is empty")
	})
	if _, err := cl.BeforeAndAfterPerformance(context.Background(), "", "qMgwmK2j"); err == nil {
		t.Error("expected error for empty competition id")
	}
	if _, err := cl.BeforeAndAfterPerformance(context.Background(), "IQC2026S2", ""); err == nil {
		t.Error("expected error for empty alpha id")
	}
}
