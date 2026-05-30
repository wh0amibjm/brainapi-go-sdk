package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CreateSimulation calls POST /simulations and returns the new simulation id
// (extracted from the Location header). The body shape mirrors what BRAIN's
// platform UI sends — see SimulationRequest.
//
// Budget: consumes one /sim slot if DailyBudget.Sims is set.
// Concurrency: bounded by MaxConcurrentSims.
func (c *Client) CreateSimulation(ctx context.Context, req SimulationRequest) (string, error) {
	if req.Type == "" {
		return "", fmt.Errorf("%w: simulation type required", ErrInvalidArgument)
	}
	if err := c.checkBudget("sim"); err != nil {
		return "", err
	}

	release, err := c.reserveSimSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/simulations",
		body:   req,
	})
	if err != nil {
		return "", err
	}
	if resp.status != 201 && resp.status != 200 {
		return "", &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/simulations", nil), Body: resp.body}
	}
	loc := resp.header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("brainapi: POST /simulations 201 without Location header")
	}
	idx := strings.LastIndex(loc, "/simulations/")
	if idx < 0 {
		return "", fmt.Errorf("brainapi: malformed Location %q", loc)
	}
	id := loc[idx+len("/simulations/"):]
	id = strings.TrimSuffix(id, "/")
	if id == "" {
		return "", fmt.Errorf("brainapi: empty simulation id in Location %q", loc)
	}
	return id, nil
}

// GetSimulation calls GET /simulations/{id} and returns the current status.
// One-shot call; for a wait-to-completion loop use WaitForSimulation.
func (c *Client) GetSimulation(ctx context.Context, id string) (*Simulation, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: simulation id required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/simulations/" + id,
	})
	if err != nil {
		return nil, err
	}
	var s Simulation
	if err := json.Unmarshal(resp.body, &s); err != nil {
		return nil, fmt.Errorf("brainapi: parse simulation: %w", err)
	}
	return &s, nil
}

// WaitForSimulation polls GET /simulations/{id} until the response carries
// terminal status (COMPLETE / FAIL / ERROR) or MaxLongPolls is exceeded.
// FAIL and ERROR are NOT wrapped as Go errors — the caller is expected to
// inspect Simulation.Status. We only surface transport / context errors.
func (c *Client) WaitForSimulation(ctx context.Context, id string) (*Simulation, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: simulation id required", ErrInvalidArgument)
	}
	for i := 0; i < c.maxLongPolls; i++ {
		s, err := c.GetSimulation(ctx, id)
		if err != nil {
			return nil, err
		}
		// BRAIN populates `alpha` whenever the sim produced one — regardless
		// of whether status is COMPLETE or WARNING (reversion / low-corr
		// advisories etc.). Mirror the reference project's check: any populated alpha
		// counts as terminal-success. FAIL/ERROR are still explicit failures.
		if s.Alpha != "" || s.Status == "FAIL" || s.Status == "ERROR" {
			return s, nil
		}
		d, _ := parseRetryAfter("") // GetSimulation strips headers; default longPollFloor
		if d <= 0 {
			d = longPollFloor * 10 // 5s default
		}
		d = clamp(d, longPollFloor, longPollCeiling)
		if err := sleepCtx(ctx, d); err != nil {
			return nil, err
		}
	}
	return nil, ErrLongPollExceeded
}
