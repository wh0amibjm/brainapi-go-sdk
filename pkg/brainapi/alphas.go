package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// encodeFilters percent-encodes each BRAIN comparison filter (e.g.
// "is.sharpe>=1.25") for transport. BRAIN parses the operator (>, >=, <, <=) off
// the parameter NAME, so the whole "field+op+value" travels as one token with no
// key/value "=" separator — url.QueryEscape encodes the operator chars while
// leaving the field path, dots and digits intact (e.g. "is.sharpe%3E%3D1.25").
// Empty fragments are dropped. Verified against the live endpoint 2026-06-07;
// the DRF "__gte" form returns HTTP 400 there, hence this raw-append path.
func encodeFilters(filters []string) []string {
	if len(filters) == 0 {
		return nil
	}
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		if f == "" {
			continue
		}
		out = append(out, url.QueryEscape(f))
	}
	return out
}

// GetAlpha calls GET /alphas/{id}. Single round-trip, returns the full alpha
// record. Note: is.selfCorrelation is only populated when status==ACTIVE; for
// UNSUBMITTED alphas the verdict comes from SubmitAlpha's long-poll, not from
// this endpoint.
func (c *Client) GetAlpha(ctx context.Context, id string) (*Alpha, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id,
	})
	if err != nil {
		return nil, err
	}
	a, err := decodeBody[Alpha](resp.body, "alpha")
	if err != nil {
		return nil, err
	}
	return a, nil
}

// CheckAlpha calls GET /alphas/{id}/check and long-polls until a terminal
// (non-empty) body is returned, or MaxLongPolls is exceeded.
//
// /check covers the deterministic pre-submit gates (LOW_SHARPE, LOW_FITNESS,
// turnover bounds, concentrated weight, sub-universe sharpe, matches-comp).
// It does NOT compute SELF_CORRELATION — that verdict only comes from
// SubmitAlpha.
func (c *Client) CheckAlpha(ctx context.Context, id string) (*IsBlock, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
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
	wrap, err := decodeBody[struct {
		Is *IsBlock `json:"is"`
	}](resp.body, "check body")
	if err != nil {
		return nil, err
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
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
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
	if resp.status >= 300 && resp.status < 400 {
		return nil // 303 "still processing" poll signal (redirect-follow disabled); keep polling
	}
	if resp.status == 503 {
		return nil // BRAIN's "queued, keep polling" signal — regardless of body shape
	}
	if len(resp.body) == 0 {
		if resp.status >= 200 && resp.status < 300 {
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
			Reason: "check_fail:" + strings.Join(nonCorrFail, ","),
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
	// SELF_CORRELATION must be attached AND PASS for a verified submit. The
	// deterministic gates (LOW_SHARPE, LOW_FITNESS, …) attach to the body before
	// corr is computed, so a 2xx whose corr check is absent, PENDING, ERROR, or
	// any non-PASS result is NOT terminal-verified — keep polling (→ pending_corr
	// at the cap) rather than mis-reporting an un-screened alpha as live.
	if corr == nil || corr.Result != "PASS" {
		return nil
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

// AlphaPnL calls GET /alphas/{id}/recordsets/pnl and long-polls until the
// PnL series is populated. Returns nil + ErrLongPollExceeded if the cache
// stays cold past MaxLongPolls.
func (c *Client) AlphaPnL(ctx context.Context, id string) (*PnLSeries, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
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
	s, err := decodeBody[PnLSeries](resp.body, "pnl")
	if err != nil {
		return nil, err
	}
	return s, nil
}

// AlphaRecordSet calls GET /alphas/{id}/recordsets/{name} and long-polls until
// the recordset is populated (same cold-cache long-poll semantics as AlphaPnL:
// 200 + Retry-After + empty body while still computing). Returns the generic
// {schema, records} envelope so callers can decode any positional recordset —
// yearly-stats, IS-Ladder, the VISUALIZATION feeds — via Schema.Properties.
//
// AlphaPnL keeps its typed *PnLSeries wrapper (selfcorr.go depends on it); this
// is the generic escape hatch for the OTHER recordset names. Discover the
// available names with AlphaRecordSets.
func (c *Client) AlphaRecordSet(ctx context.Context, alphaID, name string) (*RecordSetBlock, error) {
	if err := requireNonEmpty(alphaID, "alpha id"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(name, "recordset name"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + alphaID + "/recordsets/" + name,
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
	b, err := decodeBody[RecordSetBlock](resp.body, "recordset")
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AlphaRecordSets calls GET /alphas/{id}/recordsets and returns the list of
// available recordset descriptors for the alpha (the names you can pass to
// AlphaRecordSet). The body is returned as raw JSON — its shape is a dynamic
// list of {name, ...} entries and is passed through untyped so a new recordset
// kind never breaks the decode.
func (c *Client) AlphaRecordSets(ctx context.Context, alphaID string) (json.RawMessage, error) {
	if err := requireNonEmpty(alphaID, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + alphaID + "/recordsets",
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.body), nil
}

// AlphaSelfCorrelation calls GET /alphas/{id}/correlations/self and long-polls
// until BRAIN returns the top-N most-correlated already-submitted alphas plus
// min/max aggregates. Cached server-side after the first run per alpha;
// subsequent calls return 200 immediately.
//
// "Still computing" is signaled two ways depending on account tier — 503 with
// Retry-After (observed on Conditional-Consultant) or 200 with empty body and
// Retry-After (observed on TUTORIAL-tier accounts). Both hints are set so the
// transport retries either signal.
//
// Use BEFORE SubmitAlpha: if *block.Max >= 0.7 the alpha will be rejected by
// the post-submit SELF_CORRELATION check and burn a DailyBudget.Submits slot
// for nothing. This endpoint itself is free of submit-budget cost.
//
// Sibling /correlations/prod is exposed by AlphaProdCorrelation (the CONSULTANT
// tier upgrade lifted its former 403).
func (c *Client) AlphaSelfCorrelation(ctx context.Context, id string) (*SelfCorrelationBlock, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id + "/correlations/self",
		hints:  retryHints{longPoll503: true, longPoll200Empty: true},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	b, err := decodeBody[SelfCorrelationBlock](resp.body, "self-correlation")
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AlphaProdCorrelation calls GET /alphas/{id}/correlations/prod and long-polls
// until BRAIN returns the alpha's correlation to the whole PRODUCTION alpha pool
// — a histogram of production-alpha counts per correlation bin plus the signed
// min/max aggregates. Same long-poll semantics as AlphaSelfCorrelation
// (200 + Retry-After + empty body while still computing; 503 on some tiers).
//
// Use BEFORE SubmitAlpha alongside AlphaSelfCorrelation: gate on *block.Max >=
// 0.7. A candidate can pass the self-corr gate (max vs your own submitted book)
// yet fail here (max vs all BRAIN production alphas), so both gates matter.
func (c *Client) AlphaProdCorrelation(ctx context.Context, id string) (*ProdCorrelationBlock, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id + "/correlations/prod",
		hints:  retryHints{longPoll503: true, longPoll200Empty: true},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	b, err := decodeBody[ProdCorrelationBlock](resp.body, "prod-correlation")
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AlphaPowerPoolCorrelation calls GET /alphas/{id}/correlations/power-pool and
// long-polls until BRAIN returns the alpha's correlation to the Power Pool
// members. Live probe (2026-07-02): same long-poll handshake as the sibling
// self/prod endpoints (200 + Retry-After + empty body while computing, then
// 200 + body), and the SAME body shape as /correlations/self —
// schema.name = "selfCorrelation", per-alpha records, top-level min/max — NOT
// the prod-corr histogram. Hence it reuses SelfCorrelation's decode path exactly.
//
// EMPTY-POOL fail-open: a new Power-Pool account has no comparable PP alpha, so
// the probe returned records=[] with max=null. A nil block.Max therefore means
// "pool empty / no constraint", and callers MUST fail-OPEN on it (submit-side
// gates treat nil Max and a fetch error alike as "pass") rather than block.
// Gate on *block.Max < 0.5 only when Max is non-nil.
func (c *Client) AlphaPowerPoolCorrelation(ctx context.Context, id string) (*PowerPoolCorrelationBlock, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/alphas/" + id + "/correlations/power-pool",
		hints:  retryHints{longPoll503: true, longPoll200Empty: true},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	b, err := decodeBody[PowerPoolCorrelationBlock](resp.body, "power-pool-correlation")
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AlphaRegularProperties carries the REGULAR-scoped mutable properties nested
// under the `regular` key of an AlphaProperties PATCH body. BRAIN homes the
// alpha description HERE — {"regular":{"description":"…"}} — NOT at the top
// level. `omitempty` keeps a nil Description out of the wire body.
type AlphaRegularProperties struct {
	Description *string `json:"description,omitempty"`
}

// AlphaProperties is the mutable-properties patch body for SetAlphaProperties.
// Every field is a pointer / slice with `omitempty`, so a field left nil/empty
// is OMITTED from the marshalled JSON entirely — the byte-for-byte "not set =>
// not sent" contract. Callers set only what they want to change (PATCH is a
// partial update).
//
// Primary use (alpha-toolkit pure Power Pool submits): write a >=100-char
// description (Idea + Rationale template) into the alpha's PROPERTIES section by
// setting Regular.Description. Per BRAIN docs (getting-started-power-pool-alphas.md,
// "Description of the Power Pool Alpha"), a Power Pool alpha WITHOUT such a
// description is not eligible; the "PowerPoolSelected" tag likewise lives in
// Properties → Tags (the top-level Tags slice here).
//
// Wire contract VERIFIED live 2026-07-02 against PATCH /alphas/{id} (alpha
// akd9jor5, account DH52706):
//   - description → NESTED under `regular`: {"regular":{"description":"…"}}.
//     A TOP-LEVEL "description" is REJECTED 400 {"description":["Unexpected
//     property."]}. Hence Description lives on AlphaRegularProperties, not here.
//   - tags → top-level JSON string array, e.g. {"tags":["PowerPoolSelected"]}
//     (accepted 200 and echoed back verbatim; multiple tags round-trip).
//   - name / color / category → top-level scalars (name confirmed live; color /
//     category share the same top-level home per GET /alphas/{id} + ACE's
//     set_alpha_properties).
type AlphaProperties struct {
	Regular  *AlphaRegularProperties `json:"regular,omitempty"`
	Name     *string                 `json:"name,omitempty"`
	Color    *string                 `json:"color,omitempty"`
	Category *string                 `json:"category,omitempty"`
	Tags     []string                `json:"tags,omitempty"`
}

// SetAlphaProperties calls PATCH /alphas/{id} to update an alpha's mutable
// PROPERTIES (description, name, color, category, tags) and returns the updated
// alpha record parsed from the 200 body. Only the non-nil/non-empty fields of
// props are sent (see AlphaProperties — omitempty gives the "not set => not
// sent" byte-for-byte contract).
//
// This is the write path the pure Power Pool submit flow needs: a Power Pool
// alpha is not eligible until a >=100-char Idea+Rationale description is present
// in its Properties section (BRAIN docs: getting-started-power-pool-alphas.md
// L54-73), and the "PowerPoolSelected" tag lives there too.
//
// Budget: this is a property EDIT, NOT a submission — it does NOT consume a
// DailyBudget.Submits slot (no checkBudget call), same as GetAlpha/CheckAlpha.
//
// Wire contract VERIFIED live 2026-07-02 against PATCH /alphas/{id}: the
// description is nested under `regular` (a top-level "description" is rejected
// 400 "Unexpected property.") and tags are a top-level string array. See
// AlphaProperties for the full probe result.
func (c *Client) SetAlphaProperties(ctx context.Context, id string, props AlphaProperties) (*Alpha, error) {
	if err := requireNonEmpty(id, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "PATCH",
		path:   "/alphas/" + id,
		body:   props,
	})
	if err != nil {
		return nil, err
	}
	a, err := decodeBody[Alpha](resp.body, "alpha")
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAlphas returns the first page of GET /users/self/alphas with the
// supplied options. Use ListAlphasAll for an iterator over all pages.
func (c *Client) ListAlphas(ctx context.Context, opts ListAlphasOptions) (*Page[Alpha], error) {
	qs := newQuery().
		setIfNotEmpty("status", opts.Status).
		setIfPositive("limit", opts.Limit).
		setIfPositive("offset", opts.Offset).
		setIfNotEmpty("order", opts.Order).
		values()
	resp, err := c.do(ctx, doRequest{
		method:   "GET",
		path:     "/users/self/alphas",
		query:    qs,
		rawQuery: encodeFilters(opts.Filters),
	})
	if err != nil {
		return nil, err
	}
	page, err := decodeBody[Page[Alpha]](resp.body, "alphas page")
	if err != nil {
		return nil, err
	}
	return page, nil
}

// ListAlphasAll yields every alpha matching opts by following Django REST
// pagination. Callers must drain the returned channel; cancellation is
// honored via ctx.
func (c *Client) ListAlphasAll(ctx context.Context, opts ListAlphasOptions) (<-chan Alpha, <-chan error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}
	return paginateAll(ctx, opts.Offset, func(offset int) ([]Alpha, bool, error) {
		page, err := c.ListAlphas(ctx, ListAlphasOptions{
			Status:  opts.Status,
			Limit:   limit,
			Offset:  offset,
			Order:   opts.Order,
			Filters: opts.Filters,
		})
		if err != nil {
			return nil, false, err
		}
		done := page.Next == nil || *page.Next == ""
		return page.Results, done, nil
	})
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
