package brainapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

// tlsHTTP wraps a bogdanfinn/tls-client.HttpClient with the cookie-jar
// persistence behavior the BRAIN flows expect. Treat it as private — endpoint
// methods go through Client.do, not directly through tlsHTTP.
type tlsHTTP struct {
	client  tls_client.HttpClient
	jarPath string
	baseURL *url.URL
	logger  *slog.Logger

	saveMu sync.Mutex
}

func newTLSHTTP(profile BrowserProfile, proxy string, timeout time.Duration, jarPath string, baseURL *url.URL, logger *slog.Logger) (*tlsHTTP, error) {
	jar := tls_client.NewCookieJar()

	opts := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(int(timeout.Seconds())),
		tls_client.WithClientProfile(tlsClientProfile(profile)),
		tls_client.WithCookieJar(jar),
		// BRAIN signals "submission still processing" with a 303 back to the
		// same /submit URL whose Location carries an http:// scheme (+ :443) the
		// h2 transport rejects ("http2: unsupported scheme"). Don't auto-follow;
		// surface the 3xx so the submit long-poll treats it as a keep-polling
		// tick (see Client.do + parseSubmitVerdict).
		tls_client.WithNotFollowRedirects(),
	}
	if proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
	}

	hc, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
	if err != nil {
		return nil, fmt.Errorf("brainapi: init tls-client: %w", err)
	}

	t := &tlsHTTP{
		client:  hc,
		jarPath: jarPath,
		baseURL: baseURL,
		logger:  logger,
	}

	if jarPath != "" {
		if err := t.loadJar(); err != nil {
			// Loading is best-effort. A fresh installation has no jar file.
			logger.Debug("cookie jar not loaded", "path", jarPath, "err", err.Error())
		}
	}

	return t, nil
}

// rawRequest is the internal request envelope. Headers are applied in stable
// order so the impersonated browser's signature stays consistent.
type rawRequest struct {
	method  string
	url     string
	body    []byte
	headers []hdrPair
}

type hdrPair struct {
	name, value string
}

// rawResponse is the internal response envelope. Fully buffered — BRAIN
// bodies are small enough (< 100 KB typical) that streaming buys nothing
// and full-buffering simplifies retry/log/diagnose code paths.
type rawResponse struct {
	status int
	header fhttp.Header
	body   []byte
}

func (r *rawResponse) retryAfter() (time.Duration, bool) {
	return parseRetryAfter(r.header.Get("Retry-After"))
}

// do executes a single HTTP round-trip via tls-client. Caller is responsible
// for retry / classification / cookie persistence — keep doRT minimal.
func (t *tlsHTTP) do(ctx context.Context, r rawRequest) (*rawResponse, error) {
	var rdr io.Reader
	if len(r.body) > 0 {
		rdr = bytes.NewReader(r.body)
	}
	req, err := fhttp.NewRequestWithContext(ctx, r.method, r.url, rdr)
	if err != nil {
		return nil, fmt.Errorf("brainapi: new request: %w", err)
	}

	if len(r.headers) > 0 {
		req.Header = fhttp.Header{}
		order := make([]string, 0, len(r.headers))
		for _, h := range r.headers {
			low := strings.ToLower(h.name)
			req.Header[low] = []string{h.value}
			order = append(order, low)
		}
		req.Header[fhttp.HeaderOrderKey] = order
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brainapi: %s %s: %w", r.method, r.url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("brainapi: read body: %w", err)
	}

	if t.jarPath != "" {
		if saveErr := t.saveJar(); saveErr != nil {
			t.logger.Debug("cookie jar save failed", "err", saveErr.Error())
		}
	}

	return &rawResponse{
		status: resp.StatusCode,
		header: resp.Header,
		body:   body,
	}, nil
}

// loadJar reads previously-persisted cookies into the jar. The on-disk
// format is JSON-encoded []CookieEntry; we restrict scope to baseURL.
func (t *tlsHTTP) loadJar() error {
	b, err := os.ReadFile(t.jarPath)
	if err != nil {
		return err
	}
	var snap CookieSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	if len(snap.Cookies) == 0 {
		return nil
	}
	jar := t.client.GetCookieJar()
	if jar == nil {
		return errors.New("brainapi: tls-client has no cookie jar")
	}
	cookies := make([]*fhttp.Cookie, 0, len(snap.Cookies))
	for _, c := range snap.Cookies {
		cookies = append(cookies, &fhttp.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  c.Expires,
			Secure:   c.Secure,
			HttpOnly: c.HTTP,
		})
	}
	jar.SetCookies(t.baseURL, cookies)
	return nil
}

// saveJar dumps the current cookies for baseURL to t.jarPath. atomic-rename
// so a partial-write doesn't corrupt the jar.
func (t *tlsHTTP) saveJar() error {
	t.saveMu.Lock()
	defer t.saveMu.Unlock()

	jar := t.client.GetCookieJar()
	if jar == nil {
		return nil
	}
	cookies := jar.Cookies(t.baseURL)
	out := CookieSnapshot{
		URL:     t.baseURL.String(),
		Saved:   time.Now().UTC(),
		Cookies: make([]CookieEntry, 0, len(cookies)),
	}
	for _, c := range cookies {
		out.Cookies = append(out.Cookies, CookieEntry{
			Name:    c.Name,
			Value:   c.Value,
			Path:    c.Path,
			Domain:  c.Domain,
			Expires: c.Expires,
			Secure:  c.Secure,
			HTTP:    c.HttpOnly,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(t.jarPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := t.jarPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.jarPath)
}

// clearJar wipes any cookies the in-memory jar holds for the configured base
// URL and removes the persisted jar file (if jarPath is set). Idempotent;
// a missing file is not an error. Errors from os.Remove are surfaced via
// the logger rather than returned so they don't mask the caller's primary
// signal (the DELETE /authentication result in Logout).
func (t *tlsHTTP) clearJar() {
	if jar := t.client.GetCookieJar(); jar != nil {
		current := jar.Cookies(t.baseURL)
		if len(current) > 0 {
			past := time.Now().Add(-time.Hour)
			expired := make([]*fhttp.Cookie, 0, len(current))
			for _, ck := range current {
				expired = append(expired, &fhttp.Cookie{
					Name:    ck.Name,
					Path:    ck.Path,
					Domain:  ck.Domain,
					Expires: past,
					MaxAge:  -1,
				})
			}
			jar.SetCookies(t.baseURL, expired)
		}
	}

	if t.jarPath == "" {
		return
	}
	t.saveMu.Lock()
	defer t.saveMu.Unlock()
	if err := os.Remove(t.jarPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.logger.Warn("clear cookie jar file failed", "path", t.jarPath, "err", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Client.do — the retry-aware request loop. Every endpoint method calls this.
// ---------------------------------------------------------------------------

type doRequest struct {
	method  string
	path    string
	query   url.Values
	body    any    // serialized as JSON if non-nil
	rawBody []byte // overrides body if set
	headers []hdrPair
	auth    *basicAuth // sets Authorization: Basic ...
	bearer  string     // sets Authorization: Bearer ...
	noJSON  bool       // true to omit "Accept: application/json" / "Content-Type"
	hints   retryHints
}

type basicAuth struct {
	user, pass string
}

// do performs the request and applies the full retry+ban+cooldown policy.
// Returns the buffered response (status, headers, body) on terminal outcome.
//
//nolint:gocritic // unifies HTTP retry policy in one place by design
func (c *Client) do(ctx context.Context, r doRequest) (*rawResponse, error) {
	if c.bannedFlag.Load() {
		reason := ""
		if p := c.banReason.Load(); p != nil {
			reason = *p
		}
		return nil, &BannedError{Streak: int(c.consecutive403.Load()), Reason: reason}
	}
	if d := c.Cooldown(); d > 0 {
		return nil, fmt.Errorf("%w: %s remaining", ErrCooldown, d.Round(time.Second))
	}

	bodyBytes := r.rawBody
	if bodyBytes == nil && r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, fmt.Errorf("brainapi: marshal body: %w", err)
		}
		bodyBytes = b
	}

	urlStr := c.joinURL(r.path, r.query)

	headers := buildHeaders(r, len(bodyBytes) > 0)
	relogged := false
	longPollSeen := 0

	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := rawRequest{
			method:  r.method,
			url:     urlStr,
			body:    bodyBytes,
			headers: headers,
		}

		startedAt := time.Now()
		resp, err := c.tls.do(ctx, req)
		dur := time.Since(startedAt)
		status := 0
		if resp != nil {
			status = resp.status
		}
		c.observer.ObserveRequest(r.method, r.path, status, dur, err)

		if err != nil {
			if attempt >= c.maxRetries {
				return nil, err
			}
			c.logger.Warn("transport error, retrying",
				"method", r.method, "url", urlStr, "attempt", attempt+1, "err", err.Error())
			sleep := clamp(time.Duration(attempt+1)*networkErrFloor, networkErrFloor, networkErrCeiling)
			c.observer.ObserveRetry(r.method, r.path, 0, attempt, RetryKindNetwork, sleep)
			if sleepCtx(ctx, sleep) != nil {
				return nil, ctx.Err()
			}
			continue
		}

		// 2xx — happy path. Reset the 403 streak.
		if resp.status >= 200 && resp.status < 300 {
			// 200 + Retry-After + empty body = "still computing" for some endpoints.
			if r.hints.longPoll200Empty && len(resp.body) == 0 {
				if d, ok := resp.retryAfter(); ok {
					longPollSeen++
					if longPollSeen > effectiveMaxLongPolls(c.maxLongPolls, r.hints.maxLongPolls) {
						return nil, ErrLongPollExceeded
					}
					sleep := clamp(d, longPollFloor, longPollCeiling)
					c.observer.ObserveLongPoll(r.method, r.path, longPollSeen, sleep)
					if sleepCtx(ctx, sleep) != nil {
						return nil, ctx.Err()
					}
					continue
				}
			}
			c.consecutive403.Store(0)
			return resp, nil
		}

		switch {
		case r.hints.accept503 && resp.status >= 300 && resp.status < 400:
			// BRAIN's "accepted, still processing" poll signal is a 303 See Other
			// back to the submit URL (+ Retry-After), NOT a real move. With redirect
			// following disabled it lands here; hand it back so the SubmitAlpha
			// long-poll keeps polling, exactly like the 503 case below.
			c.consecutive403.Store(0)
			return resp, nil
		case r.hints.accept503 && resp.status == 503:
			c.consecutive403.Store(0)
			return resp, nil

		case resp.status == 401:
			if r.hints.noAutoRelogin {
				return resp, &APIError{Status: 401, Method: r.method, URL: urlStr, Body: resp.body}
			}
			if relogged {
				// Still 401 AFTER a successful re-login → a genuine auth failure
				// (forbidden account / immediately-invalidated session), not
				// success. Returning nil here masked it as an empty-body 2xx.
				return resp, &APIError{Status: 401, Method: r.method, URL: urlStr, Body: resp.body}
			}
			email, pass := c.credentials()
			if email == "" || pass == "" {
				return resp, &APIError{Status: 401, Method: r.method, URL: urlStr, Body: resp.body}
			}
			if _, err := c.Login(ctx, email, pass); err != nil {
				return resp, err
			}
			relogged = true
			continue

		case resp.status == 403:
			// 403 with a `checks` field is a normal alpha-rejection, not a ban.
			if bytes.Contains(resp.body, []byte(`"checks"`)) {
				c.consecutive403.Store(0)
				return resp, nil
			}
			// NOT_VERIFIED is its own typed error and not a ban-trigger.
			if bytes.Contains(resp.body, []byte("NOT_VERIFIED")) {
				return resp, &NotVerifiedError{Status: resp.status, Body: resp.body}
			}
			streak := c.consecutive403.Add(1)
			if c.banThreshold > 0 && int(streak) >= c.banThreshold {
				reason := shortBody(resp.body)
				c.bannedFlag.Store(true)
				c.banReason.Store(&reason)
				return resp, &BannedError{Streak: int(streak), Reason: reason}
			}
			if attempt >= c.maxRetries {
				return resp, nil
			}
			c.observer.ObserveRetry(r.method, r.path, 403, attempt, RetryKindForbidden, serverErrFloor)
			if sleepCtx(ctx, serverErrFloor) != nil {
				return nil, ctx.Err()
			}
			continue

		case resp.status == 429:
			cd := isCooldownBody(resp.body)
			d, _ := resp.retryAfter()
			d = clamp(d, rateLimitFloor, rateLimitCeiling)
			if cd {
				c.setCooldown(d)
				c.observer.ObserveRetry(r.method, r.path, 429, attempt, RetryKindCooldown, d)
			}
			if attempt >= c.maxRetries {
				return resp, &RateLimitError{Status: 429, RetryAfter: d, Cooldown: cd, URL: urlStr, Body: resp.body}
			}
			c.logger.Warn("rate-limited, sleeping",
				"url", urlStr, "retry_after", d.String(), "cooldown", cd, "attempt", attempt+1)
			c.observer.ObserveRetry(r.method, r.path, 429, attempt, RetryKindRateLimit, d)
			if sleepCtx(ctx, d) != nil {
				return nil, ctx.Err()
			}
			continue

		case resp.status == 503 && r.hints.longPoll503:
			longPollSeen++
			if longPollSeen > effectiveMaxLongPolls(c.maxLongPolls, r.hints.maxLongPolls) {
				return nil, ErrLongPollExceeded
			}
			d, _ := resp.retryAfter()
			d = clamp(d, longPollFloor, longPollCeiling)
			c.observer.ObserveLongPoll(r.method, r.path, longPollSeen, d)
			if sleepCtx(ctx, d) != nil {
				return nil, ctx.Err()
			}
			continue

		case resp.status >= 500:
			if attempt >= c.maxRetries {
				return resp, &APIError{Status: resp.status, Method: r.method, URL: urlStr, Body: resp.body}
			}
			d := clamp(time.Duration(attempt+1)*serverErrFloor, serverErrFloor, serverErrCeiling)
			c.logger.Warn("server error, retrying",
				"status", resp.status, "url", urlStr, "attempt", attempt+1, "sleep", d.String())
			c.observer.ObserveRetry(r.method, r.path, resp.status, attempt, RetryKindServer, d)
			if sleepCtx(ctx, d) != nil {
				return nil, ctx.Err()
			}
			continue

		case resp.status == 405:
			return resp, &APIError{Status: 405, Method: r.method, URL: urlStr, Body: resp.body}

		case resp.status == 404:
			return resp, &APIError{Status: 404, Method: r.method, URL: urlStr, Body: resp.body}

		default:
			// 4xx — terminal, treat body as the DRF field-error envelope.
			return resp, classifyClientError(resp, urlStr)
		}
	}
}

// buildHeaders returns the ordered header list for a request. Chrome-style
// order: accept first, then content-type (POST only), authorization, etc.
func buildHeaders(r doRequest, hasBody bool) []hdrPair {
	headers := make([]hdrPair, 0, 8+len(r.headers))
	if !r.noJSON {
		headers = append(headers, hdrPair{"accept", "application/json, text/plain, */*"})
	}
	if hasBody && !r.noJSON {
		headers = append(headers, hdrPair{"content-type", "application/json"})
	}
	if r.auth != nil {
		headers = append(headers, hdrPair{"authorization", basicAuthHeader(r.auth.user, r.auth.pass)})
	}
	if r.bearer != "" {
		headers = append(headers, hdrPair{"authorization", "Bearer " + r.bearer})
	}
	headers = append(
		headers,
		hdrPair{"accept-language", "en-US,en;q=0.9"},
		hdrPair{"origin", "https://platform.worldquantbrain.com"},
		hdrPair{"referer", "https://platform.worldquantbrain.com/"},
	)
	headers = append(headers, r.headers...)
	return headers
}

// classifyClientError decides whether a non-2xx <500 body fits the DRF
// envelope and returns the right typed error.
func classifyClientError(resp *rawResponse, urlStr string) error {
	if len(resp.body) == 0 {
		return &APIError{Status: resp.status, URL: urlStr, Body: resp.body}
	}
	trimmed := bytes.TrimSpace(resp.body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return &APIError{Status: resp.status, URL: urlStr, Body: resp.body}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return &APIError{Status: resp.status, URL: urlStr, Body: resp.body}
	}
	if d, ok := fields["detail"]; ok {
		var s string
		if err := json.Unmarshal(d, &s); err == nil {
			return &APIError{Status: resp.status, URL: urlStr, Body: resp.body}
		}
	}
	out := &DRFError{Status: resp.status, URL: urlStr, Fields: map[string][]string{}}
	hasFieldList := false
	for k, v := range fields {
		var msgs []string
		if err := json.Unmarshal(v, &msgs); err == nil {
			out.Fields[k] = msgs
			hasFieldList = true
			continue
		}
		out.Fields[k] = []string{string(v)}
	}
	if !hasFieldList {
		return &APIError{Status: resp.status, URL: urlStr, Body: resp.body}
	}
	return out
}

func effectiveMaxLongPolls(clientCap, hintCap int) int {
	if hintCap > 0 {
		return hintCap
	}
	return clientCap
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func shortBody(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
