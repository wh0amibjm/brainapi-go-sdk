package brainapi

import (
	"context"
	"encoding/json"
	"fmt"
)

// AgreeAll is the canonical auxiliary.agree list that BRAIN's registration
// flow expects. Mirrors the AGREE_ALL constant in the TS registrar.
var AgreeAll = []string{
	"user-agreement",
	"terms-conditions",
	"privacy-policy",
	"account-details",
	"challenge",
}

// Register calls POST /users to create a new secondary account. The caller supplies
// profile data; the SDK fetches a fresh Altcha PoW captcha challenge via the
// configured CaptchaSolver and injects the encoded solution into
// auxiliary.captcha.
//
// On 201 the body is opaque (TBD upstream); we return the parsed body if
// non-empty, else nil.
func (c *Client) Register(ctx context.Context, in RegisterInput) (*User, error) {
	if in.Email == "" {
		return nil, fmt.Errorf("%w: email required", ErrInvalidArgument)
	}
	if in.Auxiliary.Password == "" {
		return nil, fmt.Errorf("%w: auxiliary.password required", ErrInvalidArgument)
	}
	if in.Auxiliary.Confirmation == "" {
		in.Auxiliary.Confirmation = in.Auxiliary.Password
	}
	if len(in.Auxiliary.Agree) == 0 {
		in.Auxiliary.Agree = AgreeAll
	}
	if in.Auxiliary.Captcha == "" {
		if c.captchaSolver == nil {
			return nil, fmt.Errorf("%w: register requires a CaptchaSolver but none configured", ErrInvalidArgument)
		}
		payload, err := c.captchaSolver.Solve(ctx, c.FetchCaptchaChallenge)
		if err != nil {
			return nil, fmt.Errorf("brainapi: captcha solve: %w", err)
		}
		in.Auxiliary.Captcha = payload
	}
	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/users",
		body:   in,
	})
	if err != nil {
		return nil, err
	}
	if resp.status != 201 && resp.status != 200 {
		return nil, &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/users", nil), Body: resp.body}
	}
	if len(resp.body) == 0 {
		return nil, nil
	}
	var u User
	if err := json.Unmarshal(resp.body, &u); err != nil {
		return nil, nil //nolint:nilerr // BRAIN's 201 body is opaque; not parsing isn't a hard error
	}
	return &u, nil
}

// FetchCaptchaChallenge calls GET /captcha and returns the raw challenge body.
// Used by the altcha solver implementation. Exposed so callers can compose
// their own captcha flow if they need to.
func (c *Client) FetchCaptchaChallenge(ctx context.Context) ([]byte, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/captcha",
	})
	if err != nil {
		return nil, err
	}
	if resp.status != 200 {
		return nil, &APIError{Status: resp.status, Method: "GET", URL: c.joinURL("/captcha", nil), Body: resp.body}
	}
	return resp.body, nil
}
