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
