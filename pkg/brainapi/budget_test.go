package brainapi

import (
	"errors"
	"testing"
	"time"
)

// TestChallengeDayStr pins the BRAIN challenge-day boundary: a fixed UTC-4
// offset with the roll at MIDNIGHT (04:00 UTC year-round, no DST/EST fallback).
// A prior implementation shifted by -3h (3 AM ET) and mis-filed every
// 00:00–03:00 EDT call into the previous day — these cases guard against that
// regression.
func TestChallengeDayStr(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"03:59 UTC is still the previous EDT day", time.Date(2025, 7, 2, 3, 59, 0, 0, time.UTC), "2025-07-01"},
		{"04:00 UTC rolls to the new EDT day", time.Date(2025, 7, 2, 4, 0, 0, 0, time.UTC), "2025-07-02"},
		{"01:00 EDT (05:00 UTC) files as the NEW day, not shifted back 3h", time.Date(2025, 7, 2, 5, 0, 0, 0, time.UTC), "2025-07-02"},
		{"winter: boundary stays at 04:00 UTC (no EST fallback) - before", time.Date(2025, 1, 2, 3, 59, 0, 0, time.UTC), "2025-01-01"},
		{"winter: boundary stays at 04:00 UTC (no EST fallback) - after", time.Date(2025, 1, 2, 4, 0, 0, 0, time.UTC), "2025-01-02"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := challengeDayStr(c.in); got != c.want {
				t.Errorf("challengeDayStr(%s) = %s, want %s", c.in.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestCheckBudget_DayRollover exercises the reset branch (c.budgetDay != day):
// the gate fires within a day, then resets when the clock crosses the next
// 04:00-UTC challenge-day boundary.
func TestCheckBudget_DayRollover(t *testing.T) {
	cl, err := NewClient(Options{DailyBudget: DailyBudget{Submits: 1}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Day N, well inside the day.
	dayN := time.Date(2025, 7, 2, 12, 0, 0, 0, time.UTC)
	cl.now = func() time.Time { return dayN }

	if err := cl.checkBudget("submit"); err != nil {
		t.Fatalf("first submit of the day must pass: %v", err)
	}
	if err := cl.checkBudget("submit"); !errors.Is(err, ErrDailyBudgetExhausted) {
		t.Fatalf("second submit same day must be gated, got: %v", err)
	}

	// Cross the boundary into day N+1 (past 04:00 UTC) — the counter must reset.
	dayNplus1 := time.Date(2025, 7, 3, 5, 0, 0, 0, time.UTC)
	cl.now = func() time.Time { return dayNplus1 }

	if err := cl.checkBudget("submit"); err != nil {
		t.Fatalf("first submit after day rollover must pass (counter reset): %v", err)
	}
	if err := cl.checkBudget("submit"); !errors.Is(err, ErrDailyBudgetExhausted) {
		t.Fatalf("second submit on the new day must be gated again, got: %v", err)
	}
}
