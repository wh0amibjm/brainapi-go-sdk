package brainapi

import (
	"context"
)

// ReverifyEmail calls POST /user/email/reverify in the anonymous (no-session)
// mode used by the production revival path.
//
// Critical: the endpoint behaves differently when called with an active
// session cookie — see account-registration.md. Use a fresh Client (no
// prior Login) to stay in anonymous mode.
//
// recaptcha is the legacy reCAPTCHA v2 token; this endpoint hasn't migrated
// to Altcha yet. Pass an empty string if BRAIN has dropped the field by
// now (we'll find out the first time the live call returns 400 with
// {recaptcha: ["required"]}).
func (c *Client) ReverifyEmail(ctx context.Context, email, recaptcha string) error {
	if err := requireNonEmpty(email, "email"); err != nil {
		return err
	}
	body := map[string]string{"email": email}
	if recaptcha != "" {
		body["recaptcha"] = recaptcha
	}
	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/user/email/reverify",
		body:   body,
	})
	if err != nil {
		return err
	}
	if err := c.checkStatus(resp, "/user/email/reverify"); err != nil {
		return err
	}
	return nil
}

// VerifyEmail calls POST /user/email/verify with the JWT extracted from the
// verification-mail link. The JWT is the entire payload — body is empty,
// auth lives in the Authorization: Bearer header.
func (c *Client) VerifyEmail(ctx context.Context, jwt string) error {
	if err := requireNonEmpty(jwt, "jwt"); err != nil {
		return err
	}
	resp, err := c.do(ctx, doRequest{
		method:  "POST",
		path:    "/user/email/verify",
		bearer:  jwt,
		rawBody: []byte("{}"),
	})
	if err != nil {
		return err
	}
	if err := c.checkStatus(resp, "/user/email/verify"); err != nil {
		return err
	}
	return nil
}
