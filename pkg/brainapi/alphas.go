package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// GetAlpha calls GET /alphas/{id}. Single round-trip, returns the full alpha
// record. Note: is.selfCorrelation is only populated when status==ACTIVE; for
// UNSUBMITTED alphas the verdict comes from SubmitAlpha's long-poll, not from
// this endpoint.
func (c *Client) GetAlpha(ctx context.Context, id string) (*Alpha, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: alpha id required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id,
	})
	if err != nil {
		return nil, err
	}
	var a Alpha
	if err := json.Unmarshal(resp.body, &a); err != nil {
		return nil, fmt.Errorf("brainapi: parse alpha: %w", err)
	}
	return &a, nil
}

// CheckAlpha calls GET /alphas/{id}/check and long-polls until a terminal
// (non-empty) body is returned, or MaxLongPolls is exceeded.
//
// /check covers the deterministic pre-submit gates (LOW_SHARPE, LOW_FITNESS,
// turnover bounds, concentrated weight, sub-universe sharpe, matches-comp).
// It does NOT compute SELF_CORRELATION — that verdict only comes from
// SubmitAlpha.
func (c *Client) CheckAlpha(ctx context.Context, id string) (*IsBlock, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: alpha id required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id + "/check",
		hints: retryHints{
			longPoll200Empty: true,
			maxLongPolls:     30, // /check resolves in 1-3 GETs typically
		},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	var wrap struct {
		Is *IsBlock `json:"is"`
	}
	if err := json.Unmarshal(resp.body, &wrap); err != nil {
		return nil, fmt.Errorf("brainapi: parse check body: %w", err)
	}
	if wrap.Is == nil {
		wrap.Is = &IsBlock{}
	}
	return wrap.Is, nil
}

// SubmitAlpha calls POST /alphas/{id}/submit then long-polls GET until a
// terminal verdict is observed. This is the ONLY source of truth for
// SELF_CORRELATION outcomes; GET /alphas/{id} alone is insufficient.
//
// Verdict.Status is one of: "verified" | "corr_rejected" | "submit_failed" |
// "pending_corr" (latter means long-poll cap exceeded).
//
// Budget: each call consumes one /submit slot if DailyBudget.Submits is set.
func (c *Client) SubmitAlpha(ctx context.Context, id string) (*Verdict, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: alpha id required", ErrInvalidArgument)
	}
	if err := c.checkBudget("submit"); err != nil {
		return nil, err
	}

	postPath := "/alphas/" + id + "/submit"
	postResp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   postPath,
		hints:  retryHints{accept503: true},
	})
	if err != nil {
		return nil, err
	}
	if v := parseSubmitVerdict(postResp); v != nil {
		return v, nil
	}

	for i := 0; i < c.maxLongPolls; i++ {
		if d, ok := postResp.retryAfter(); ok {
			if err := sleepCtx(ctx, clamp(d, longPollFloor, longPollCeiling)); err != nil {
				return nil, err
			}
		} else {
			if err := sleepCtx(ctx, longPollFloor); err != nil {
				return nil, err
			}
		}
		resp, err := c.do(ctx, doRequest{
			method: "GET",
			path:   postPath,
			hints:  retryHints{accept503: true},
		})
		if err != nil {
			return nil, err
		}
		if v := parseSubmitVerdict(resp); v != nil {
			return v, nil
		}
		postResp = resp
	}
	return &Verdict{Status: "pending_corr", Reason: "long-poll cap exceeded"}, nil
}

// parseSubmitVerdict mirrors the TS _parse_submit_verdict function. Returns
// nil while the verdict is still pending; returns a populated Verdict on
// terminal outcome.
//
// A 503 is BRAIN's "queued, keep polling" signal and surfaces here as
// empty-body — return nil to keep the outer loop going.
func parseSubmitVerdict(resp *rawResponse) *Verdict {
	if resp == nil {
		return nil
	}
	if len(resp.body) == 0 {
		if resp.status == 503 || (resp.status >= 200 && resp.status < 300) {
			return nil // accepted/queued, body will populate; keep polling
		}
		return &Verdict{
			Status: "submit_failed",
			Reason: fmt.Sprintf("http_%d_no_body", resp.status),
			HTTP:   resp.status,
		}
	}
	var body struct {
		Is *IsBlock `json:"is"`
	}
	if err := json.Unmarshal(resp.body, &body); err != nil {
		return &Verdict{
			Status: "submit_failed",
			Reason: "unparseable body: " + err.Error(),
			HTTP:   resp.status,
		}
	}
	if body.Is == nil {
		if resp.status >= 200 && resp.status < 300 {
			return &Verdict{Status: "verified", HTTP: resp.status}
		}
		return &Verdict{Status: "submit_failed", Reason: fmt.Sprintf("http_%d_no_is", resp.status), HTTP: resp.status}
	}
	var nonCorrFail []string
	var corr *Check
	for i := range body.Is.Checks {
		ch := &body.Is.Checks[i]
		switch ch.Name {
		case "SELF_CORRELATION":
			corr = ch
		default:
			if ch.Result == "FAIL" {
				nonCorrFail = append(nonCorrFail, ch.Name)
			}
		}
	}
	if len(nonCorrFail) > 0 {
		return &Verdict{
			Status: "submit_failed",
			Reason: "check_fail:" + joinStrings(nonCorrFail, ","),
			Checks: body.Is.Checks,
			HTTP:   resp.status,
		}
	}
	if corr != nil && corr.Result == "FAIL" {
		val := "?"
		if corr.Value != nil {
			val = strconv.FormatFloat(*corr.Value, 'f', -1, 64)
		}
		return &Verdict{
			Status: "corr_rejected",
			Reason: "SELF_CORRELATION=" + val,
			Checks: body.Is.Checks,
			HTTP:   resp.status,
		}
	}
	if corr != nil && corr.Result == "PENDING" {
		return nil // keep polling
	}
	if resp.status >= 200 && resp.status < 300 {
		var alpha Alpha
		if err := json.Unmarshal(resp.body, &alpha); err == nil {
			return &Verdict{Status: "verified", Body: &alpha, Checks: body.Is.Checks, HTTP: resp.status}
		}
		return &Verdict{Status: "verified", Checks: body.Is.Checks, HTTP: resp.status}
	}
	return &Verdict{
		Status: "submit_failed",
		Reason: fmt.Sprintf("http_%d_no_fail_check", resp.status),
		Checks: body.Is.Checks,
		HTTP:   resp.status,
	}
}

func joinStrings(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for _, x := range s[1:] {
		out += sep + x
	}
	return out
}

// AlphaPnL calls GET /alphas/{id}/recordsets/pnl and long-polls until the
// PnL series is populated. Returns nil + ErrLongPollExceeded if the cache
// stays cold past MaxLongPolls.
func (c *Client) AlphaPnL(ctx context.Context, id string) (*PnLSeries, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: alpha id required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id + "/recordsets/pnl",
		hints: retryHints{
			longPoll200Empty: true,
			maxLongPolls:     6,
		},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	var s PnLSeries
	if err := json.Unmarshal(resp.body, &s); err != nil {
		return nil, fmt.Errorf("brainapi: parse pnl: %w", err)
	}
	return &s, nil
}

// ListAlphas returns the first page of GET /users/self/alphas with the
// supplied options. Use ListAlphasAll for an iterator over all pages.
func (c *Client) ListAlphas(ctx context.Context, opts ListAlphasOptions) (*Page[Alpha], error) {
	qs := url.Values{}
	if opts.Status != "" {
		qs.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		qs.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		qs.Set("offset", strconv.Itoa(opts.Offset))
	}
	if opts.Order != "" {
		qs.Set("order", opts.Order)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/users/self/alphas",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	var page Page[Alpha]
	if err := json.Unmarshal(resp.body, &page); err != nil {
		return nil, fmt.Errorf("brainapi: parse alphas page: %w", err)
	}
	return &page, nil
}

// ListAlphasAll yields every alpha matching opts by following Django REST
// pagination. Callers must drain the returned channel; cancellation is
// honored via ctx.
func (c *Client) ListAlphasAll(ctx context.Context, opts ListAlphasOptions) (<-chan Alpha, <-chan error) {
	out := make(chan Alpha)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		offset := opts.Offset
		limit := opts.Limit
		if limit == 0 {
			limit = 100
		}
		for {
			page, err := c.ListAlphas(ctx, ListAlphasOptions{
				Status: opts.Status,
				Limit:  limit,
				Offset: offset,
				Order:  opts.Order,
			})
			if err != nil {
				errs <- err
				return
			}
			for _, a := range page.Results {
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- a:
				}
			}
			if page.Next == nil || *page.Next == "" || len(page.Results) == 0 {
				return
			}
			offset += len(page.Results)
		}
	}()
	return out, errs
}

// AlphaCheckBody is a convenience that calls CheckAlpha and converts the
// resulting checks into a verdict-like {pass: bool, fails: []string} pair.
// Useful for CLI output.
func (c *Client) AlphaCheckBody(ctx context.Context, id string) (bool, []string, error) {
	is, err := c.CheckAlpha(ctx, id)
	if err != nil {
		return false, nil, err
	}
	var fails []string
	for _, ch := range is.Checks {
		if ch.Result == "FAIL" {
			fails = append(fails, ch.Name)
		}
	}
	return len(fails) == 0, fails, nil
}
