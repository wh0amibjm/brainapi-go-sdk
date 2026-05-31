package brainapi

import "context"

// BeforeAndAfterPerformance calls
// GET /competitions/{competitionID}/alphas/{alphaID}/before-and-after-performance
// and long-polls until BRAIN finishes the projection. It returns the competition
// score plus the performance metrics the alpha would carry BEFORE vs AFTER being
// submitted into the competition — the data behind the "Performance Comparison"
// panel on an unsubmitted alpha's page.
//
// Cold-cache long-polled like the recordset endpoints: BRAIN answers 503 (or 200
// with an empty body) + Retry-After while computing, then 200 with the body.
// Returns nil + ErrLongPollExceeded if it stays cold past MaxLongPolls.
//
// Unlike /alphas/{id}/recordsets/*, this endpoint lives under the competition the
// alpha would be submitted to, so the caller supplies the competition id (e.g.
// "IQC2026S2"). It is free of submit-budget cost.
func (c *Client) BeforeAndAfterPerformance(ctx context.Context, competitionID, alphaID string) (*BeforeAndAfterPerformance, error) {
	if err := requireNonEmpty(competitionID, "competition id"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(alphaID, "alpha id"); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/competitions/" + competitionID + "/alphas/" + alphaID + "/before-and-after-performance",
		hints:  retryHints{longPoll503: true, longPoll200Empty: true},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.body) == 0 {
		return nil, ErrLongPollExceeded
	}
	p, err := decodeBody[BeforeAndAfterPerformance](resp.body, "before-and-after-performance")
	if err != nil {
		return nil, err
	}
	return p, nil
}
