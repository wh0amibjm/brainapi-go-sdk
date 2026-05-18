package brainapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestSelf(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/self" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "users_self.json"))
	})
	u, err := cl.Self(context.Background())
	if err != nil {
		t.Fatalf("Self: %v", err)
	}
	if !u.Verified || !u.Approved {
		t.Errorf("verified/approved flags wrong: %+v", u)
	}
}

func TestCompetitions(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "competitions.json"))
	})
	p, err := cl.Competitions(context.Background())
	if err != nil {
		t.Fatalf("Competitions: %v", err)
	}
	if p.Count != 1 || len(p.Results) != 1 {
		t.Errorf("wrong page: %+v", p)
	}
	if p.Results[0].Leaderboard == nil || p.Results[0].Leaderboard.Level != "GOLD" {
		t.Errorf("leaderboard missing: %+v", p.Results[0])
	}
}

func TestActivities_DailyEnvelope(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/self/activities/simulations" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "activities_simulations.json"))
	})
	s, err := cl.Activities(context.Background(), brainapi.ActivitySimulations)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	if s.Type != brainapi.ActivityTypeDaily {
		t.Errorf("expected DAILY, got %q", s.Type)
	}
	if s.Yesterday == nil || s.Total == nil {
		t.Errorf("daily envelope missing yesterday/total: %+v", s)
	}
	recs, err := brainapi.DecodeActivities(s)
	if err != nil {
		t.Fatalf("DecodeActivities: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}
	// Sanity-check decoded first record.
	var date string
	var value int
	_ = json.Unmarshal(recs[0]["date"], &date)
	_ = json.Unmarshal(recs[0]["value"], &value)
	if date != "2026-04-11" || value != 268 {
		t.Errorf("first decoded record wrong: date=%q value=%d", date, value)
	}
}

func TestActivities_ListEnvelope(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "activities_other_payment_empty.json"))
	})
	s, err := cl.Activities(context.Background(), brainapi.ActivityOtherPayment)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	if s.Type != brainapi.ActivityTypeList {
		t.Errorf("expected LIST, got %q", s.Type)
	}
	if s.Yesterday != nil {
		t.Errorf("LIST envelope should not have yesterday: %+v", s.Yesterday)
	}
	if s.Total == nil {
		t.Error("LIST envelope must have total")
	}
}
