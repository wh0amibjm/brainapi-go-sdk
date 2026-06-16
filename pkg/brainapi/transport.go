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

// saveJar dumps the current cookies for baseURL to t.jarPath. Writes to a
// per-write UNIQUE temp file then atomic-renames it into place, so neither a
// partial write nor a CONCURRENT brainapi process sharing this jarPath can
// corrupt the jar (the in-process path is already serialized by saveMu; the
// unique temp name removes the remaining cross-process clobber on a fixed
// "<jar>.tmp"). Last writer wins, harmless since all hold the same session.
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
	// CreateTemp picks a unique name (and 0o600 perms) in the jar's dir, so two
	// processes saving at once never write the same temp before the rename.
	f, err := os.CreateTemp(dir, filepath.Base(t.jarPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if err := os.Rename(tmp, t.jarPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	method string
	path   string
	query  url.Values
	// rawQuery holds PRE-ENCODED query fragments appended after the url.Values
	// `query`. BRAIN's list filters embed the operator in the field token, e.g.
	// `is.sharpe>=1.25`, which must travel as ONE token with no key/value "="
	// separator. url.Values can't express that — it always emits `key=value`, so
	// it would append a stray "=" and double-encode the operator. Callers
	// percent-encode each fragment themselves (see ListAlphas → encodeFilters);
	// do() appends them as-is without re-encoding, and url.Parse preserves
	// RawQuery as written. The caller owns each fragment's correctness.
	rawQuery []string
	body     any    // serialized as JSON if non-nil
	rawBody  []byte // overrides body if set
	headers  []hdrPair
	auth     *basicAuth // sets Authorization: Basic ...
	bearer   string     // sets Authorization: Bearer ...
	noJSON   bool       // true to omit "Accept: application/json" / "Content-Type"
	hints    retryHints
}

type basicAuth struct {
	user, pass string
}

// attemptDecision is the outcome of classifying one HTTP attempt: either a
// terminal (resp, err) to hand back to the caller, or a request to retry after
// sleeping for sleep (which may be 0, e.g. the post-relogin retry).
type attemptDecision struct {
	resp  *rawResponse
	err   error
	retry bool
	sleep time.Duration
}

func terminal(resp *rawResponse, err error) attemptDecision {
	return attemptDecision{resp: resp, err: err}
}

func retryIn(sleep time.Duration) attemptDecision {
	return attemptDecision{retry: true, sleep: sleep}
}

// retryState carries the loop-local state the per-attempt evaluator reads and
// updates across attempts.
//
// errAttempt counts only real error-class retries (network / 403 / 429 / 5xx)
// and is what the maxRetries budget gates on. It is deliberately SEPARATE from
// longPollSeen: a long-poll tick (longPoll200Empty / longPoll503) must not burn
// the error-retry budget, otherwise a transient blip partway through a
// multi-minute poll would no longer be retried.
type retryState struct {
	relogged     bool
	longPollSeen int
	errAttempt   int
}

// preflight applies the local short-circuit gates that refuse a request before
// it touches the network: a tripped ban flag and an active cooldown.
func (c *Client) preflight() error {
	if c.bannedFlag.Load() {
		reason := ""
		if p := c.banReason.Load(); p != nil {
			reason = *p
		}
		return &BannedError{Streak: int(c.consecutive403.Load()), Reason: reason}
	}
	if d := c.Cooldown(); d > 0 {
		return fmt.Errorf("%w: %s remaining", ErrCooldown, d.Round(time.Second))
	}
	return nil
}

// do performs the request and applies the full retry+ban+cooldown policy.
// Returns the buffered response (status, headers, body) on terminal outcome.
// Per-attempt classification lives in evaluate; this loop only dispatches and
// honors the resulting sleep/return decision.
func (c *Client) do(ctx context.Context, r doRequest) (*rawResponse, error) {
	if err := c.preflight(); err != nil {
		return nil, err
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
	// Append pre-encoded raw query fragments after the url.Values query (see
	// doRequest.rawQuery): BRAIN's comparison filters travel as a single encoded
	// field+op+value token with no key/value "=" separator, which url.Values cannot
	// express. Callers already percent-encoded them; we only join, never re-encode.
	if len(r.rawQuery) > 0 {
		sep := "&"
		if !strings.Contains(urlStr, "?") {
			sep = "?"
		}
		urlStr += sep + strings.Join(r.rawQuery, "&")
	}
	headers := buildHeaders(r, len(bodyBytes) > 0)
	var st retryState

	// Termination is guaranteed by evaluate: error classes are capped by
	// st.errAttempt (<= maxRetries), long-polls by st.longPollSeen, the
	// relogin is one-shot (st.relogged), and 2xx/4xx are terminal.
	for {
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

		dec := c.evaluate(ctx, r, urlStr, resp, err, &st)
		if !dec.retry {
			return dec.resp, dec.err
		}
		if sleepCtx(ctx, dec.sleep) != nil {
			return nil, ctx.Err()
		}
	}
}

// evaluate classifies a single attempt's transport result (resp, err) into an
// attemptDecision. It owns the response policy — success, long-poll,
// auto-relogin, ban detection, rate-limit, server/client errors — and updates
// the cross-attempt counters in st; the caller (do) owns the loop and the
// sleep. Returns terminal(...) to stop, retryIn(d) to retry. Error-class
// retries are budgeted by st.errAttempt (independent of long-poll ticks).
//
//nolint:gocritic // the status policy is intentionally one flat switch
func (c *Client) evaluate(ctx context.Context, r doRequest, urlStr string, resp *rawResponse, err error, st *retryState) attemptDecision {
	if err != nil {
		if st.errAttempt >= c.maxRetries {
			return terminal(nil, err)
		}
		c.logger.Warn("transport error, retrying",
			"method", r.method, "url", urlStr, "attempt", st.errAttempt+1, "err", err.Error())
		sleep := clamp(time.Duration(st.errAttempt+1)*networkErrFloor, networkErrFloor, networkErrCeiling)
		c.observer.ObserveRetry(r.method, r.path, 0, st.errAttempt, RetryKindNetwork, sleep)
		st.errAttempt++
		return retryIn(sleep)
	}

	// 2xx — happy path. Reset the 403 streak.
	if resp.status >= 200 && resp.status < 300 {
		// 200 + Retry-After + empty body = "still computing" for some endpoints.
		if r.hints.longPoll200Empty && len(resp.body) == 0 {
			if d, ok := resp.retryAfter(); ok {
				st.longPollSeen++
				if st.longPollSeen > effectiveMaxLongPolls(c.maxLongPolls, r.hints.maxLongPolls) {
					return terminal(nil, ErrLongPollExceeded)
				}
				sleep := clamp(d, longPollFloor, longPollCeiling)
				c.observer.ObserveLongPoll(r.method, r.path, st.longPollSeen, sleep)
				return retryIn(sleep)
			}
		}
		c.consecutive403.Store(0)
		return terminal(resp, nil)
	}

	switch {
	case r.hints.accept503 && resp.status >= 300 && resp.status < 400:
		// BRAIN's "accepted, still processing" poll signal is a 303 See Other
		// back to the submit URL (+ Retry-After), NOT a real move. With redirect
		// following disabled it lands here; hand it back so the SubmitAlpha
		// long-poll keeps polling, exactly like the 503 case below.
		c.consecutive403.Store(0)
		return terminal(resp, nil)
	case r.hints.accept503 && resp.status == 503:
		c.consecutive403.Store(0)
		return terminal(resp, nil)

	case resp.status == 401:
		if r.hints.noAutoRelogin {
			return terminal(resp, &APIError{Status: 401, Method: r.method, URL: urlStr, Body: resp.body})
		}
		if st.relogged {
			// Still 401 AFTER a successful re-login → a genuine auth failure
			// (forbidden account / immediately-invalidated session), not
			// success. Returning nil here masked it as an empty-body 2xx.
			return terminal(resp, &APIError{Status: 401, Method: r.method, URL: urlStr, Body: resp.body})
		}
		email, pass := c.credentials()
		if email == "" || pass == "" {
			// No cached credentials to log in with — this is "not logged in /
			// not configured", not a server-side API failure. Surface the stable
			// ErrNotAuthenticated (kind: not_authenticated) so every caller — the
			// CLI envelope and each MCP tool — gets the same signal regardless of
			// which endpoint was hit first. (Probe already maps its own 401 here.)
			return terminal(resp, ErrNotAuthenticated)
		}
		if _, err := c.Login(ctx, email, pass); err != nil {
			return terminal(resp, err)
		}
		st.relogged = true
		c.observer.ObserveRetry(r.method, r.path, 401, st.errAttempt, RetryKindUnauthorized, 0)
		return retryIn(0)

	case resp.status == 403:
		// 403 with a `checks` field is a normal alpha-rejection, not a ban.
		if bytes.Contains(resp.body, []byte(`"checks"`)) {
			c.consecutive403.Store(0)
			return terminal(resp, nil)
		}
		// NOT_VERIFIED is its own typed error and not a ban-trigger.
		if bytes.Contains(resp.body, []byte("NOT_VERIFIED")) {
			return terminal(resp, &NotVerifiedError{Status: resp.status, Body: resp.body})
		}
		// A 403 carrying the DRF permission/auth envelope ({"detail": "..."}) is
		// a terminal authorization boundary for this endpoint, not an account
		// ban. Retrying can never clear it, and counting it toward the ban streak
		// would let one call to a permission-gated endpoint self-trip the ban
		// flag on a healthy account — so reset the streak and surface a typed,
		// non-retryable PermissionDeniedError. Only opaque 403s (no detail body —
		// edge blocks, real bans) fall through to ban detection below.
		if detail, ok := drfDetail(resp.body); ok {
			c.consecutive403.Store(0)
			return terminal(resp, &PermissionDeniedError{Status: resp.status, Detail: detail, Body: resp.body})
		}
		streak := c.consecutive403.Add(1)
		if c.banThreshold > 0 && int(streak) >= c.banThreshold {
			reason := shortBody(resp.body)
			c.bannedFlag.Store(true)
			c.banReason.Store(&reason)
			return terminal(resp, &BannedError{Streak: int(streak), Reason: reason})
		}
		if st.errAttempt >= c.maxRetries {
			return terminal(resp, nil)
		}
		c.observer.ObserveRetry(r.method, r.path, 403, st.errAttempt, RetryKindForbidden, serverErrFloor)
		st.errAttempt++
		return retryIn(serverErrFloor)

	case resp.status == 429:
		cd := isCooldownBody(resp.body)
		d, _ := resp.retryAfter()
		d = clamp(d, rateLimitFloor, rateLimitCeiling)
		if cd {
			c.setCooldown(d)
			c.observer.ObserveRetry(r.method, r.path, 429, st.errAttempt, RetryKindCooldown, d)
		}
		if st.errAttempt >= c.maxRetries {
			return terminal(resp, &RateLimitError{Status: 429, RetryAfter: d, Cooldown: cd, URL: urlStr, Body: resp.body})
		}
		c.logger.Warn("rate-limited, sleeping",
			"url", urlStr, "retry_after", d.String(), "cooldown", cd, "attempt", st.errAttempt+1)
		c.observer.ObserveRetry(r.method, r.path, 429, st.errAttempt, RetryKindRateLimit, d)
		st.errAttempt++
		return retryIn(d)

	case resp.status == 503 && r.hints.longPoll503:
		st.longPollSeen++
		if st.longPollSeen > effectiveMaxLongPolls(c.maxLongPolls, r.hints.maxLongPolls) {
			return terminal(nil, ErrLongPollExceeded)
		}
		d, _ := resp.retryAfter()
		d = clamp(d, longPollFloor, longPollCeiling)
		c.observer.ObserveLongPoll(r.method, r.path, st.longPollSeen, d)
		return retryIn(d)

	case resp.status >= 500:
		if st.errAttempt >= c.maxRetries {
			return terminal(resp, &APIError{Status: resp.status, Method: r.method, URL: urlStr, Body: resp.body})
		}
		d := clamp(time.Duration(st.errAttempt+1)*serverErrFloor, serverErrFloor, serverErrCeiling)
		c.logger.Warn("server error, retrying",
			"status", resp.status, "url", urlStr, "attempt", st.errAttempt+1, "sleep", d.String())
		c.observer.ObserveRetry(r.method, r.path, resp.status, st.errAttempt, RetryKindServer, d)
		st.errAttempt++
		return retryIn(d)

	case resp.status == 405:
		return terminal(resp, &APIError{Status: 405, Method: r.method, URL: urlStr, Body: resp.body})

	case resp.status == 404:
		return terminal(resp, &APIError{Status: 404, Method: r.method, URL: urlStr, Body: resp.body})

	default:
		// 4xx — terminal, treat body as the DRF field-error envelope.
		return terminal(resp, classifyClientError(resp, urlStr))
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
