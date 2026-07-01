package brainapi

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// parseSimLocation extracts the simulation id from a POST /simulations Location
// header (the PARENT id for a multi-simulation).
func parseSimLocation(loc string) (string, error) {
	if loc == "" {
		return "", fmt.Errorf("brainapi: POST /simulations 201 without Location header")
	}
	idx := strings.LastIndex(loc, "/simulations/")
	if idx < 0 {
		return "", fmt.Errorf("brainapi: malformed Location %q", loc)
	}
	id := strings.TrimSuffix(loc[idx+len("/simulations/"):], "/")
	if id == "" {
		return "", fmt.Errorf("brainapi: empty simulation id in Location %q", loc)
	}
	return id, nil
}

// CreateSimulation calls POST /simulations with a single simulation object and
// returns the new simulation id plus the daily-quota RateLimit parsed from the
// X-Ratelimit-* response headers. The body shape mirrors what BRAIN's platform
// UI sends — see SimulationRequest.
//
// Budget: consumes one /sim slot if DailyBudget.Sims is set.
// Concurrency: bounded by MaxConcurrentSims.
func (c *Client) CreateSimulation(ctx context.Context, req SimulationRequest) (*CreateSimulationResult, error) {
	if err := requireNonEmpty(req.Type, "simulation type"); err != nil {
		return nil, err
	}
	if err := c.checkBudget("sim"); err != nil {
		return nil, err
	}

	release, err := c.reserveSimSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/simulations",
		body:   req,
	})
	if err != nil {
		return nil, err
	}
	if resp.status != 201 && resp.status != 200 {
		return nil, &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/simulations", nil), Body: resp.body}
	}
	id, err := parseSimLocation(resp.header.Get("Location"))
	if err != nil {
		return nil, err
	}
	return &CreateSimulationResult{ID: id, RateLimit: resp.rateLimit()}, nil
}

// CreateMultiSimulation calls POST /simulations with an ARRAY of 2..10
// simulation objects — the MULTI_SIMULATION consultant feature. BRAIN runs the
// children sequentially under a single PARENT simulation and returns the parent
// id in Location; poll it with WaitForMultiSimulation.
//
// All requests must be homogeneous — same Type, InstrumentType, Region, Delay
// and Language (BRAIN rejects a mixed batch). See validateMultiSimHomogeneous.
//
// Budget: reserves len(reqs) /sim slots — each child counts against the daily
// simulation quota. The returned RateLimit mirrors the authoritative server
// X-Ratelimit-Remaining after the batch was admitted.
func (c *Client) CreateMultiSimulation(ctx context.Context, reqs []SimulationRequest) (*CreateSimulationResult, error) {
	if err := validateMultiSimHomogeneous(reqs); err != nil {
		return nil, err
	}
	// Each child is a counted simulation. Reserve all len(reqs) units in ONE
	// all-or-nothing step so a mid-batch gate trip can't leak k-1 units for a
	// POST that never left the client.
	if err := c.checkBudgetN("sim", len(reqs)); err != nil {
		return nil, err
	}

	release, err := c.reserveSimSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/simulations",
		body:   reqs, // a JSON slice marshals to the array body BRAIN expects
	})
	if err != nil {
		return nil, err
	}
	if resp.status != 201 && resp.status != 200 {
		return nil, &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/simulations", nil), Body: resp.body}
	}
	id, err := parseSimLocation(resp.header.Get("Location"))
	if err != nil {
		return nil, err
	}
	return &CreateSimulationResult{ID: id, RateLimit: resp.rateLimit()}, nil
}

// validateMultiSimHomogeneous enforces BRAIN's multi-simulation constraints:
// 2..10 requests that share Type, InstrumentType, Region, Delay and Language.
func validateMultiSimHomogeneous(reqs []SimulationRequest) error {
	if len(reqs) < 2 || len(reqs) > 10 {
		return fmt.Errorf("%w: multi-simulation needs 2..10 requests, got %d", ErrInvalidArgument, len(reqs))
	}
	first := reqs[0]
	for i, r := range reqs {
		if err := requireNonEmpty(r.Type, "simulation type"); err != nil {
			return err
		}
		if r.Type != first.Type ||
			r.Settings.InstrumentType != first.Settings.InstrumentType ||
			r.Settings.Region != first.Settings.Region ||
			r.Settings.Delay != first.Settings.Delay ||
			r.Settings.Language != first.Settings.Language {
			return fmt.Errorf("%w: multi-simulation request %d differs in type/instrumentType/region/delay/language from request 0", ErrInvalidArgument, i)
		}
	}
	return nil
}

// isTerminalSimStatus reports whether a simulation status string is terminal.
// BRAIN uses WAITING/SIMULATING while running and
// COMPLETE/WARNING/CANCELLED/TIMEOUT/ERROR/FAIL at the end. WARNING still
// produces an alpha (reversion / low-corr advisories etc.).
func isTerminalSimStatus(status string) bool {
	switch status {
	case "COMPLETE", "WARNING", "CANCELLED", "TIMEOUT", "ERROR", "FAIL":
		return true
	}
	return false
}

// getSimulationRaw fetches GET /simulations/{id} and also surfaces the server's
// Retry-After hint (0 when absent) so poll loops can pace on it.
func (c *Client) getSimulationRaw(ctx context.Context, id string) (*Simulation, time.Duration, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/simulations/" + id,
	})
	if err != nil {
		return nil, 0, err
	}
	s, err := decodeBody[Simulation](resp.body, "simulation")
	if err != nil {
		return nil, 0, err
	}
	ra, _ := resp.retryAfter()
	return s, ra, nil
}

// GetSimulation calls GET /simulations/{id} and returns the current status.
// One-shot call; for a wait-to-completion loop use WaitForSimulation.
func (c *Client) GetSimulation(ctx context.Context, id string) (*Simulation, error) {
	if err := requireNonEmpty(id, "simulation id"); err != nil {
		return nil, err
	}
	s, _, err := c.getSimulationRaw(ctx, id)
	return s, err
}

// WaitForSimulation polls GET /simulations/{id} until the response is terminal
// — a populated alpha (single sim or multi-sim child), a populated children
// list (a completed multi-sim PARENT), or a terminal status — or MaxLongPolls
// is exceeded. FAIL/ERROR are NOT wrapped as Go errors; the caller inspects
// Simulation.Status. Polls pace on the server Retry-After hint when present,
// else on the fixed long-poll default.
func (c *Client) WaitForSimulation(ctx context.Context, id string) (*Simulation, error) {
	if err := requireNonEmpty(id, "simulation id"); err != nil {
		return nil, err
	}
	for i := 0; i < c.maxLongPolls; i++ {
		s, retryAfter, err := c.getSimulationRaw(ctx, id)
		if err != nil {
			return nil, err
		}
		if s.Alpha != "" || len(s.Children) > 0 || isTerminalSimStatus(s.Status) {
			return s, nil
		}
		d := retryAfter
		if d <= 0 {
			d = longPollFloor * 10
		}
		if err := sleepCtx(ctx, clamp(d, longPollFloor, longPollCeiling)); err != nil {
			return nil, err
		}
	}
	return nil, ErrLongPollExceeded
}

// WaitForMultiSimulation waits for a multi-simulation PARENT to reach terminal
// state, then waits for and returns every child simulation. The parent carries
// no alpha of its own — each child in Children[] produces one.
//
// A single child error does NOT abort the fan-out: the remaining children are
// still resolved best-effort, and the FIRST error is returned alongside the
// children that DID resolve — so one momentarily-unreachable child never drops
// the healthy ones. A non-nil error with a non-nil parent means "incomplete,
// re-poll the missing children", not "everything failed". Context cancellation
// stops the loop early (later children cannot succeed).
func (c *Client) WaitForMultiSimulation(ctx context.Context, parentID string) (*Simulation, []*Simulation, error) {
	if err := requireNonEmpty(parentID, "parent simulation id"); err != nil {
		return nil, nil, err
	}
	parent, err := c.WaitForSimulation(ctx, parentID)
	if err != nil {
		return nil, nil, err
	}
	children := make([]*Simulation, 0, len(parent.Children))
	var firstErr error
	for _, cid := range parent.Children {
		child, cerr := c.WaitForSimulation(ctx, cid)
		if cerr != nil {
			if firstErr == nil {
				firstErr = cerr
			}
			if ctx.Err() != nil {
				break // cancelled/timed-out: remaining children can't succeed
			}
			continue
		}
		children = append(children, child)
	}
	return parent, children, firstErr
}
