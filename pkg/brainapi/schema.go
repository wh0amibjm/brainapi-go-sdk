package brainapi

import (
	"context"
	"fmt"
	"strconv"
)

// Operators calls GET /operators. The response is a bare JSON array, not
// the Django REST pagination envelope.
func (c *Client) Operators(ctx context.Context) ([]Operator, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/operators",
	})
	if err != nil {
		return nil, err
	}
	out, err := decodeBody[[]Operator](resp.body, "operators")
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// DataFields calls GET /data-fields with the four REQUIRED query params
// (instrumentType, region, universe, delay) and optional pagination. The
// response envelope is {count, results} — no next/previous URLs.
func (c *Client) DataFields(ctx context.Context, q DataFieldsQuery) (*DataFieldsPage, error) {
	if q.InstrumentType == "" || q.Region == "" || q.Universe == "" {
		return nil, fmt.Errorf("%w: instrumentType, region, universe required", ErrInvalidArgument)
	}
	qs := newQuery().
		set("instrumentType", q.InstrumentType).
		set("region", q.Region).
		set("universe", q.Universe).
		set("delay", strconv.Itoa(q.Delay)).
		setIfPositive("limit", q.Limit).
		setIfPositive("offset", q.Offset).
		values()
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/data-fields",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	page, err := decodeBody[DataFieldsPage](resp.body, "data-fields")
	if err != nil {
		return nil, err
	}
	return page, nil
}

// DataFieldsAll iterates every data-field for the (region, universe, delay)
// tier by following BRAIN's count-vs-results pagination. Implementation
// walks the offset cursor since /data-fields has no next/previous links.
func (c *Client) DataFieldsAll(ctx context.Context, q DataFieldsQuery) (<-chan DataField, <-chan error) {
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	return paginateAll(ctx, q.Offset, func(offset int) ([]DataField, bool, error) {
		q2 := q
		q2.Limit = limit
		q2.Offset = offset
		page, err := c.DataFields(ctx, q2)
		if err != nil {
			return nil, false, err
		}
		done := offset+len(page.Results) >= page.Count
		return page.Results, done, nil
	})
}

// Datasets calls GET /data-sets — the Data Explorer "Datasets" catalog for a
// (instrumentType, region, universe, delay) tier. Each result carries the
// consultant-only Dataset Value Score (under-utilization signal) plus crowding
// (alphaCount/userCount) and any live pyramid-theme multiplier. Mirrors
// DataFields: same required query params, same {count, results} envelope.
//
// The /data-sets path and its value-score field name are NOT pinned by a local
// reference doc (brain-api.md documents /data-fields only). Both are the most
// reasonable inference from the platform's /data/data-sets Data Explorer page
// and the ACE get_datasets helper; the Dataset type decodes value-score /
// pyramid aliases defensively (all pointer/omitempty) so an unexpected shape
// degrades to nil rather than erroring. Confirm the exact path + field via an
// active probe against the live endpoint before relying on the score.
func (c *Client) Datasets(ctx context.Context, q DataFieldsQuery) (*DatasetsPage, error) {
	if q.InstrumentType == "" || q.Region == "" || q.Universe == "" {
		return nil, fmt.Errorf("%w: instrumentType, region, universe required", ErrInvalidArgument)
	}
	qs := newQuery().
		set("instrumentType", q.InstrumentType).
		set("region", q.Region).
		set("universe", q.Universe).
		set("delay", strconv.Itoa(q.Delay)).
		setIfPositive("limit", q.Limit).
		setIfPositive("offset", q.Offset).
		values()
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/data-sets",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	page, err := decodeBody[DatasetsPage](resp.body, "data-sets")
	if err != nil {
		return nil, err
	}
	return page, nil
}

// DatasetsAll drains every dataset for the tier via the offset cursor, mirroring
// DataFieldsAll.
func (c *Client) DatasetsAll(ctx context.Context, q DataFieldsQuery) (<-chan Dataset, <-chan error) {
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	return paginateAll(ctx, q.Offset, func(offset int) ([]Dataset, bool, error) {
		q2 := q
		q2.Limit = limit
		q2.Offset = offset
		page, err := c.Datasets(ctx, q2)
		if err != nil {
			return nil, false, err
		}
		done := offset+len(page.Results) >= page.Count
		return page.Results, done, nil
	})
}

// Themes calls GET /themes — the currently-announced consultant Themes (region
// / dataset / delay bonuses running 1-3 weeks). Each theme carries a
// QualityFactor multiplier; when an alpha satisfies several overlapping themes
// the effective multiplier is sum(multipliers) - count + 1
// (themes/multiplier-rules.md). Returns a bare or {results}-wrapped array,
// decoded flexibly.
//
// FAIL-OPEN: no local reference doc pins the /themes path or response schema
// (the multiplier-rules doc describes the arithmetic, not an API). The path is
// the most reasonable inference; on a 404/403/shape-mismatch the caller should
// degrade gracefully (the toolkit report treats an error as "themes
// unavailable"). Confirm path + schema via an active probe. The Theme type
// keeps a Raw fallback so extra fields survive a decode.
func (c *Client) Themes(ctx context.Context) ([]Theme, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/themes",
	})
	if err != nil {
		return nil, err
	}
	// Accept either a bare array or a {count, results} / {results} envelope.
	if out, err := decodeBody[[]Theme](resp.body, "themes"); err == nil {
		return *out, nil
	}
	env, err := decodeBody[struct {
		Results []Theme `json:"results"`
	}](resp.body, "themes")
	if err != nil {
		return nil, err
	}
	return env.Results, nil
}
