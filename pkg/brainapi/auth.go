package brainapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Login performs POST /authentication with HTTP Basic auth. On success the
// session cookie lands in the configured cookie jar (and is persisted to
// disk if CookieJarPath is set). The email/password are cached on the
// Client so subsequent 401 retries can re-login transparently.
//
// Three possible terminal outcomes:
//   - normal login: returns a Session with User+Token+Permissions
//   - persona inquiry: returns *PersonaInquiryError carrying the Inquiry id
//   - bad credentials: returns *APIError with status 401
func (c *Client) Login(ctx context.Context, email, password string) (*Session, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("%w: email and password required", ErrInvalidArgument)
	}
	resp, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/authentication",
		auth:   &basicAuth{user: email, pass: password},
		hints:  retryHints{noAutoRelogin: true},
	})
	if err != nil {
		return nil, err
	}
	if resp.status < 200 || resp.status >= 300 {
		return nil, &APIError{Status: resp.status, Method: "POST", URL: c.joinURL("/authentication", nil), Body: resp.body}
	}

	var sess Session
	if len(resp.body) > 0 {
		if err := json.Unmarshal(resp.body, &sess); err != nil {
			return nil, fmt.Errorf("brainapi: parse session body: %w", err)
		}
	}
	if sess.Inquiry != "" && sess.User == nil {
		return nil, &PersonaInquiryError{Inquiry: sess.Inquiry}
	}
	c.SetCredentials(email, password)
	return &sess, nil
}

// Probe calls GET /authentication. Returns the live session info. Treats a
// 401 or an empty 200 body (observed in production after Logout destroys
// the server-side session but cached cookies look syntactically valid) as
// ErrNotAuthenticated — both mean "no usable session".
func (c *Client) Probe(ctx context.Context) (*SessionInfo, error) {
	resp, err := c.do(ctx, doRequest{
		method: "GET",
		path:   "/authentication",
	})
	if err != nil {
		if ae, ok := AsAPIError(err); ok && ae.Status == 401 {
			return nil, ErrNotAuthenticated
		}
		return nil, err
	}
	if resp.status == 401 || len(resp.body) == 0 {
		return nil, ErrNotAuthenticated
	}
	var info SessionInfo
	if err := json.Unmarshal(resp.body, &info); err != nil {
		return nil, fmt.Errorf("brainapi: parse probe body: %w", err)
	}
	return &info, nil
}

// Logout calls DELETE /authentication. On success it also wipes the local
// session state: the persisted cookie jar file (if any), the in-memory jar
// cookies for the configured baseURL, and the cached email/password. The
// Client is safe to reuse for a fresh Login afterwards.
//
// If DELETE fails (network error, 5xx, etc.) local state is left untouched
// so the caller can retry without losing a still-valid server-side session.
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.do(ctx, doRequest{
		method: "DELETE",
		path:   "/authentication",
	})
	if err != nil {
		return err
	}
	c.tls.clearJar()
	c.ClearCredentials()
	return nil
}

// CompletePersona drives the persona-inquiry flow. Operationally dead-code
// in current BRAIN production (no secondary account login has triggered the
// inquiry envelope since rotation), but kept as a safety net per spec.
//
// The function POSTs the inquiry id to /authentication/persona and then
// re-issues POST /authentication to finalize the session.
func (c *Client) CompletePersona(ctx context.Context, inquiry, email, password string) (*Session, error) {
	if inquiry == "" {
		return nil, fmt.Errorf("%w: inquiry id required", ErrInvalidArgument)
	}
	type personaBody struct {
		Inquiry string `json:"inquiry"`
	}
	if _, err := c.do(ctx, doRequest{
		method: "POST",
		path:   "/authentication/persona",
		body:   personaBody{Inquiry: inquiry},
	}); err != nil {
		// Persona-completion endpoint shapes aren't fully spec'd; surface as APIError.
		var ae *APIError
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, err
	}
	return c.Login(ctx, email, password)
}
