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

// DataCategories calls GET /data-categories. Live probe (2026-07-02): the
// response is a BARE JSON array (not the {count, results} envelope) of
// category descriptors, each with a category-level `valueScore` (float), a
// `region` array, `datasetCount`/`fieldCount`/`alphaCount`/`userCount` counts,
// and a `children` array of subcategories with the same shape.
//
// Unlike /data-sets this endpoint takes NO query params — the category tree is
// global, spanning every region (each node lists the regions it covers in
// Region). It complements Datasets: /data-categories gives the coarse
// value-score map to pick a category, /data-sets drills into the datasets under
// it (with the pyramid multiplier).
func (c *Client) DataCategories(ctx context.Context) ([]DataCategory, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/data-categories",
	})
	if err != nil {
		return nil, err
	}
	// Bare array per the live probe; accept a {results} envelope defensively in
	// case BRAIN later wraps it.
	if out, err := decodeBody[[]DataCategory](resp.body, "data-categories"); err == nil {
		return *out, nil
	}
	env, err := decodeBody[struct {
		Results []DataCategory `json:"results"`
	}](resp.body, "data-categories")
	if err != nil {
		return nil, err
	}
	return env.Results, nil
}

// Themes calls GET /themes — the currently-announced consultant Themes (region
// / dataset / delay bonuses running 1-3 weeks). Each theme carries a
// QualityFactor multiplier; when an alpha satisfies several overlapping themes
// the effective multiplier is sum(multipliers) - count + 1
// (themes/multiplier-rules.md). Returns a bare or {results}-wrapped array,
// decoded flexibly.
//
// PROBED 404 (2026-07-02): there is NO independent /themes endpoint on BRAIN —
// the live probe returned 404. The theme CALENDAR lives as a Learn
// documentation page (learn/documentation/themes/consgrpdefault, a weekly
// rolling table), which has no JSON API. The authoritative API source for the
// multiplier CURRENTLY in effect is instead the /data-sets list field
// `pyramidMultiplier` (see Datasets / Dataset.PyramidMultiplier). This method
// is retained ONLY for its already-fail-open semantics: on the expected 404 the
// caller degrades to "themes unavailable" and should read pyramidMultiplier
// off /data-sets instead. Do NOT treat a 404 here as an error condition — it is
// the confirmed steady state. The Theme type keeps a Raw fallback so extra
// fields survive a decode if BRAIN ever ships the endpoint.
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
