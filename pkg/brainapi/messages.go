package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Messages returns the first page of GET /users/self/messages with the supplied
// options — the notification feed behind the BRAIN platform notification center.
// Use MessagesAll for an iterator over all pages.
//
// Type filters the feed: "ANNOUNCEMENT" (platform announcements, incl. new-
// dataset notices) or "NOTIFICATION" (per-user events, e.g. achievements).
// Empty Type returns every type in one feed.
func (c *Client) Messages(ctx context.Context, opts ListMessagesOptions) (*Page[Message], error) {
	qs := url.Values{}
	if opts.Type != "" {
		qs.Set("type", opts.Type)
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
		path:   "/users/self/messages",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	var page Page[Message]
	if err := json.Unmarshal(resp.body, &page); err != nil {
		return nil, fmt.Errorf("brainapi: parse messages page: %w", err)
	}
	return &page, nil
}

// MessagesAll yields every message matching opts by following Django REST
// pagination. Callers must drain the returned channel; cancellation is honored
// via ctx.
func (c *Client) MessagesAll(ctx context.Context, opts ListMessagesOptions) (<-chan Message, <-chan error) {
	out := make(chan Message)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		offset := opts.Offset
		limit := opts.Limit
		if limit == 0 {
			limit = 50
		}
		for {
			page, err := c.Messages(ctx, ListMessagesOptions{
				Type:   opts.Type,
				Limit:  limit,
				Offset: offset,
				Order:  opts.Order,
			})
			if err != nil {
				errs <- err
				return
			}
			for _, m := range page.Results {
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- m:
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
