package brainapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestCreateSimulation_ReadsLocation(t *testing.T) {
	t.Parallel()
	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/simulations" {
			t.Errorf("wrong: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Location", "https://api.worldquantbrain.com/simulations/R8sL22iY4nxcgCxslACWlI")
		w.Header().Set("Retry-After", "5.0")
		w.WriteHeader(201)
	})
	_ = srv
	id, err := cl.CreateSimulation(context.Background(), brainapi.SimulationRequest{
		Type:    "REGULAR",
		Regular: "close",
		Settings: brainapi.SimSettings{
			InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000",
			Delay: 1, Decay: 12, Neutralization: "SUBINDUSTRY",
			Truncation: 0.02, Pasteurization: "ON", UnitHandling: "VERIFY",
			NanHandling: "OFF", Language: "FASTEXPR",
		},
	})
	if err != nil {
		t.Fatalf("CreateSimulation: %v", err)
	}
	if id != "R8sL22iY4nxcgCxslACWlI" {
		t.Errorf("got id=%q", id)
	}
}

func TestWaitForSimulation_PollsUntilComplete(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.WriteHeader(200)
		if n < 3 {
			w.Header().Set("Retry-After", "0.01")
			_, _ = w.Write(loadFixture(t, "simulation_in_progress.json"))
			return
		}
		_, _ = w.Write(loadFixture(t, "simulation_complete.json"))
	})
	s, err := cl.WaitForSimulation(context.Background(), "abc")
	if err != nil {
		t.Fatalf("WaitForSimulation: %v", err)
	}
	if s.Status != "COMPLETE" || s.Alpha != "qMPjAxnO" {
		t.Errorf("wrong terminal: %+v", s)
	}
}

func TestCreateSimulation_MissingLocation(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
	})
	_, err := cl.CreateSimulation(context.Background(), brainapi.SimulationRequest{
		Type: "REGULAR", Regular: "close",
		Settings: brainapi.SimSettings{InstrumentType: "EQUITY", Region: "USA", Universe: "TOP3000", Delay: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "Location") {
		t.Fatalf("expected Location error, got %v", err)
	}
}
