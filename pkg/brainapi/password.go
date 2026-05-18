package brainapi

import (
	"context"
	"fmt"
)

// ForgotPassword calls POST /user/password/forgot to initiate the reset
// flow. recaptcha is the legacy v2 token; same caveat as ReverifyEmail.
func (c *Client) ForgotPassword(ctx context.Context, email, recaptcha string) error {
	if email == "" {
		return fmt.Errorf("%w: email required", ErrInvalidArgument)
	}
	body := map[string]string{"email": email}
	if recaptcha != "" {
		body["recaptcha"] = recaptcha
	}
	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/user/password/forgot",
		body:   body,
	})
	if err != nil {
		return err
	}
	if resp.status < 200 || resp.status >= 300 {
		return &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/user/password/forgot", nil), Body: resp.body}
	}
	return nil
}

// ResetPassword calls POST /user/password/reset with the JWT from the reset
// email link plus the desired new password.
func (c *Client) ResetPassword(ctx context.Context, jwt, newPassword string) error {
	if jwt == "" {
		return fmt.Errorf("%w: jwt required", ErrInvalidArgument)
	}
	if newPassword == "" {
		return fmt.Errorf("%w: new password required", ErrInvalidArgument)
	}
	body := map[string]string{
		"password":     newPassword,
		"confirmation": newPassword,
	}
	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/user/password/reset",
		bearer: jwt,
		body:   body,
	})
	if err != nil {
		return err
	}
	if resp.status < 200 || resp.status >= 300 {
		return &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/user/password/reset", nil), Body: resp.body}
	}
	return nil
}
