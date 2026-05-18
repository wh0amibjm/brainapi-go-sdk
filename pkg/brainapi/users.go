package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
)

// Self calls GET /users/self.
func (c *Client) Self(ctx context.Context) (*User, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/users/self",
	})
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal(resp.body, &u); err != nil {
		return nil, fmt.Errorf("brainapi: parse self: %w", err)
	}
	return &u, nil
}

// Competitions calls GET /users/self/competitions and returns the (paginated)
// list of competitions the user has signed up for, with leaderboard info.
func (c *Client) Competitions(ctx context.Context) (*Page[Competition], error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/users/self/competitions",
	})
	if err != nil {
		return nil, err
	}
	var p Page[Competition]
	if err := json.Unmarshal(resp.body, &p); err != nil {
		return nil, fmt.Errorf("brainapi: parse competitions: %w", err)
	}
	return &p, nil
}

// Activities calls GET /users/self/activities/{kind}. The two envelope shapes
// (DAILY and LIST) are handled by ActivityStream's tagged decoder.
func (c *Client) Activities(ctx context.Context, kind ActivityKind) (*ActivityStream, error) {
	if kind == "" {
		return nil, fmt.Errorf("%w: activity kind required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/users/self/activities/" + string(kind),
	})
	if err != nil {
		return nil, err
	}
	var s ActivityStream
	if err := json.Unmarshal(resp.body, &s); err != nil {
		return nil, fmt.Errorf("brainapi: parse activities: %w", err)
	}
	return &s, nil
}

// ActivityRecord is a generic decoded row from ActivityStream.Records.records,
// keyed by RecordSchema.Properties[*].Name. Values are kept as json.RawMessage
// because the column types are heterogeneous (date/amount/integer/text).
type ActivityRecord map[string]json.RawMessage

// DecodeActivities converts the tuple-array RecordSetBlock.Records into
// ActivityRecord dicts using the columnar schema. Returns an empty slice if
// either the schema or the records are absent.
func DecodeActivities(s *ActivityStream) ([]ActivityRecord, error) {
	if s == nil || s.Records == nil || s.Records.Schema == nil {
		return nil, nil
	}
	props := s.Records.Schema.Properties
	if len(props) == 0 {
		return nil, nil
	}
	out := make([]ActivityRecord, 0, len(s.Records.Records))
	for _, raw := range s.Records.Records {
		var row []json.RawMessage
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("brainapi: decode activity row: %w", err)
		}
		if len(row) > len(props) {
			row = row[:len(props)]
		}
		rec := make(ActivityRecord, len(row))
		for i, v := range row {
			rec[props[i].Name] = v
		}
		out = append(out, rec)
	}
	return out, nil
}
