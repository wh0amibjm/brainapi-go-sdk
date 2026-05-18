package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	var out []Operator
	if err := json.Unmarshal(resp.body, &out); err != nil {
		return nil, fmt.Errorf("brainapi: parse operators: %w", err)
	}
	return out, nil
}

// DataFields calls GET /data-fields with the four REQUIRED query params
// (instrumentType, region, universe, delay) and optional pagination. The
// response envelope is {count, results} — no next/previous URLs.
func (c *Client) DataFields(ctx context.Context, q DataFieldsQuery) (*DataFieldsPage, error) {
	if q.InstrumentType == "" || q.Region == "" || q.Universe == "" {
		return nil, fmt.Errorf("%w: instrumentType, region, universe required", ErrInvalidArgument)
	}
	qs := url.Values{}
	qs.Set("instrumentType", q.InstrumentType)
	qs.Set("region", q.Region)
	qs.Set("universe", q.Universe)
	qs.Set("delay", strconv.Itoa(q.Delay))
	if q.Limit > 0 {
		qs.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		qs.Set("offset", strconv.Itoa(q.Offset))
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/data-fields",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	var page DataFieldsPage
	if err := json.Unmarshal(resp.body, &page); err != nil {
		return nil, fmt.Errorf("brainapi: parse data-fields: %w", err)
	}
	return &page, nil
}

// DataFieldsAll iterates every data-field for the (region, universe, delay)
// tier by following BRAIN's count-vs-results pagination. Implementation
// walks the offset cursor since /data-fields has no next/previous links.
func (c *Client) DataFieldsAll(ctx context.Context, q DataFieldsQuery) (<-chan DataField, <-chan error) {
	out := make(chan DataField)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		offset := q.Offset
		limit := q.Limit
		if limit == 0 {
			limit = 100
		}
		for {
			q2 := q
			q2.Limit = limit
			q2.Offset = offset
			page, err := c.DataFields(ctx, q2)
			if err != nil {
				errs <- err
				return
			}
			for _, f := range page.Results {
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- f:
				}
			}
			if len(page.Results) == 0 || offset+len(page.Results) >= page.Count {
				return
			}
			offset += len(page.Results)
		}
	}()
	return out, errs
}
