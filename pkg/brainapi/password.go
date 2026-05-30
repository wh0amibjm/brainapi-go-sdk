package brainapi

import (
	"context"
)

// ForgotPassword calls POST /user/password/forgot to initiate the reset
// flow. recaptcha is the legacy v2 token; same caveat as ReverifyEmail.
func (c *Client) ForgotPassword(ctx context.Context, email, recaptcha string) error {
	if err := requireNonEmpty(email, "email"); err != nil {
		return err
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
	if err := c.checkStatus(resp, "/user/password/forgot"); err != nil {
		return err
	}
	return nil
}

// ResetPassword calls POST /user/password/reset with the JWT from the reset
// email link plus the desired new password.
func (c *Client) ResetPassword(ctx context.Context, jwt, newPassword string) error {
	if err := requireNonEmpty(jwt, "jwt"); err != nil {
		return err
	}
	if err := requireNonEmpty(newPassword, "new password"); err != nil {
		return err
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
	if err := c.checkStatus(resp, "/user/password/reset"); err != nil {
		return err
	}
	return nil
}
