package brainapi

import (
	"context"
)

// Messages returns the first page of GET /users/self/messages with the supplied
// options — the notification feed behind the BRAIN platform notification center.
// Use MessagesAll for an iterator over all pages.
//
// Type filters the feed: "ANNOUNCEMENT" (platform announcements, incl. new-
// dataset notices) or "NOTIFICATION" (per-user events, e.g. achievements).
// Empty Type returns every type in one feed.
func (c *Client) Messages(ctx context.Context, opts ListMessagesOptions) (*Page[Message], error) {
	qs := newQuery().
		setIfNotEmpty("type", opts.Type).
		setIfPositive("limit", opts.Limit).
		setIfPositive("offset", opts.Offset).
		setIfNotEmpty("order", opts.Order).
		values()
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/users/self/messages",
		query:  qs,
	})
	if err != nil {
		return nil, err
	}
	page, err := decodeBody[Page[Message]](resp.body, "messages page")
	if err != nil {
		return nil, err
	}
	return page, nil
}

// MessagesAll yields every message matching opts by following Django REST
// pagination. Callers must drain the returned channel; cancellation is honored
// via ctx.
func (c *Client) MessagesAll(ctx context.Context, opts ListMessagesOptions) (<-chan Message, <-chan error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 50
	}
	return paginateAll(ctx, opts.Offset, func(offset int) ([]Message, bool, error) {
		page, err := c.Messages(ctx, ListMessagesOptions{
			Type:   opts.Type,
			Limit:  limit,
			Offset: offset,
			Order:  opts.Order,
		})
		if err != nil {
			return nil, false, err
		}
		done := page.Next == nil || *page.Next == ""
		return page.Results, done, nil
	})
}
