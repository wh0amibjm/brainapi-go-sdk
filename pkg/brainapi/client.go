package brainapi

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// CaptchaSolver fetches a /captcha challenge and returns the base64-encoded
// solution payload to drop into auxiliary.captcha on POST /users.
//
// The fetch callback is supplied by the SDK and bound to the active Client's
// transport — implementations don't import brainapi, which keeps the captcha
// sub-package free of import cycles. The standard implementation lives in
// pkg/captcha/altcha (CaptchaAdapter); callers can plug their own for tests
// or non-Altcha future captcha mechanisms.
type CaptchaSolver interface {
	Solve(ctx context.Context, fetch func(context.Context) ([]byte, error)) (string, error)
}

// DailyBudget is the in-process daily quota gate. Counters reset at the
// next BRAIN day boundary (ET midnight; 3 AM ET is only the data refresh).
// Set Sims or Submits to 0 to disable the gate for that operation type.
type DailyBudget struct {
	Sims    int
	Submits int
}

// Options configures a Client. Zero-value Options yields a usable client
// against BRAIN production with default Chrome 131 fingerprint and no proxy
// or persistent cookies.
type Options struct {
	// BaseURL overrides "https://api.worldquantbrain.com".
	BaseURL string

	// Profile selects the TLS/JA3 fingerprint. Default: ProfileChrome131.
	Profile BrowserProfile

	// Proxy is an optional HTTP/HTTPS/SOCKS5 proxy URL. Default: direct.
	Proxy string

	// CookieJarPath, when non-empty, persists cookies to disk after every
	// successful request, and reloads them at NewClient time.
	CookieJarPath string

	// Timeout per HTTP call. Default: 15s.
	Timeout time.Duration

	// MaxRetries is the number of retry attempts (in addition to the
	// initial call) for non-long-poll error classes. Default: 3.
	MaxRetries int

	// MaxLongPolls bounds /alphas/{id}/submit, /simulations/{id},
	// /alphas/{id}/check, /alphas/{id}/recordsets/pnl loops. Default: 60.
	MaxLongPolls int

	// MaxConcurrentSims is the semaphore size for in-flight POST /simulations.
	// Default: 2 for main, 1 for sub — callers must set explicitly based on
	// account tier.
	MaxConcurrentSims int

	// BanThreshold is the consecutive-403 streak that flips a secondary account
	// to BannedError. 0 disables ban detection. Default: 3.
	BanThreshold int

	// DailyBudget enforces in-process quota gates per BRAIN challenge-day.
	DailyBudget DailyBudget

	// Logger is the slog target. Default: slog.Default().
	Logger *slog.Logger

	// Observer is the instrumentation hook. Default: no-op. Wire to
	// Prometheus / OTel by implementing the Observer interface.
	Observer Observer

	// CaptchaSolver is used by Register. Default: pkg/captcha/altcha solver
	// (wired by SetDefaultAltchaSolver in init).
	CaptchaSolver CaptchaSolver

	// Email / Password are cached for auto-relogin on 401. Setting these
	// makes the Client capable of recovering from session expiry without
	// caller intervention.
	Email    string
	Password string
}

// Client is the BRAIN HTTP client. All endpoint methods route through it.
// Goroutine-safe; one Client per (account, proxy) pair is the intended
// usage pattern.
type Client struct {
	baseURL           *url.URL
	profile           BrowserProfile
	proxy             string
	cookieJarPath     string
	timeout           time.Duration
	maxRetries        int
	maxLongPolls      int
	maxConcurrentSims int
	banThreshold      int
	dailyBudget       DailyBudget
	logger            *slog.Logger
	observer          Observer
	captchaSolver     CaptchaSolver

	credMu   sync.RWMutex
	email    string
	password string

	tls *tlsHTTP

	consecutive403 atomic.Int32
	bannedFlag     atomic.Bool
	banReason      atomic.Pointer[string] // reason captured when bannedFlag is set; reused by the short-circuit

	cooldownMu    sync.RWMutex
	cooldownUntil time.Time

	simSem chan struct{}

	budgetMu   sync.Mutex
	budgetDay  string
	budgetSims int
	budgetSubs int
}

// NewClient constructs a Client from Options. Returns an error only if the
// underlying tls-client could not be initialized (e.g. an invalid proxy URL).
func NewClient(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.worldquantbrain.com"
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	if opts.Profile == "" {
		opts.Profile = DefaultProfile
	}
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
	if opts.MaxLongPolls == 0 {
		opts.MaxLongPolls = 60
	}
	if opts.MaxConcurrentSims == 0 {
		opts.MaxConcurrentSims = 2
	}
	if opts.BanThreshold == 0 {
		opts.BanThreshold = 3
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Observer == nil {
		opts.Observer = noopObserver{}
	}
	if opts.CaptchaSolver == nil {
		opts.CaptchaSolver = defaultCaptchaSolver()
	}

	c := &Client{
		baseURL:           u,
		profile:           opts.Profile,
		proxy:             opts.Proxy,
		cookieJarPath:     opts.CookieJarPath,
		timeout:           opts.Timeout,
		maxRetries:        opts.MaxRetries,
		maxLongPolls:      opts.MaxLongPolls,
		maxConcurrentSims: opts.MaxConcurrentSims,
		banThreshold:      opts.BanThreshold,
		dailyBudget:       opts.DailyBudget,
		logger:            opts.Logger,
		observer:          opts.Observer,
		captchaSolver:     opts.CaptchaSolver,
		email:             opts.Email,
		password:          opts.Password,
		simSem:            make(chan struct{}, opts.MaxConcurrentSims),
	}

	tls, err := newTLSHTTP(c.profile, c.proxy, c.timeout, c.cookieJarPath, c.baseURL, c.logger)
	if err != nil {
		return nil, err
	}
	c.tls = tls
	return c, nil
}

// BaseURL returns the resolved base URL.
func (c *Client) BaseURL() *url.URL { return c.baseURL }

// Profile returns the configured browser fingerprint name.
func (c *Client) Profile() BrowserProfile { return c.profile }

// Logger returns the Client's slog target. Useful for endpoint methods that
// want to log under the same handler.
func (c *Client) Logger() *slog.Logger { return c.logger }

// CookieJarPath returns the on-disk path for persisted cookies (or "").
func (c *Client) CookieJarPath() string { return c.cookieJarPath }

// SetCredentials updates the email/password cache used for auto-relogin.
// Safe to call concurrently with other methods.
func (c *Client) SetCredentials(email, password string) {
	c.credMu.Lock()
	defer c.credMu.Unlock()
	c.email = email
	c.password = password
}

// ClearCredentials wipes the cached email/password, undoing SetCredentials.
// After this call 401 responses propagate ErrNotAuthenticated instead of
// triggering a transparent re-login. Logout invokes this automatically;
// callers in long-lived services should invoke it when the credentials are
// no longer needed (e.g. after switching accounts). Safe to call
// concurrently with other Client methods.
func (c *Client) ClearCredentials() {
	c.credMu.Lock()
	defer c.credMu.Unlock()
	c.email = ""
	c.password = ""
}

func (c *Client) credentials() (string, string) {
	c.credMu.RLock()
	defer c.credMu.RUnlock()
	return c.email, c.password
}

// IsBanned returns true after the consecutive-403 ban threshold has fired.
// Re-creating the Client clears the flag; this is intentional — the caller
// decides when to recover.
func (c *Client) IsBanned() bool { return c.bannedFlag.Load() }

// Cooldown returns how long the Client is locked out by a server-side
// "concurrent" / "previous to finish" 429 hint. Zero if not cooling down.
func (c *Client) Cooldown() time.Duration {
	c.cooldownMu.RLock()
	defer c.cooldownMu.RUnlock()
	if c.cooldownUntil.IsZero() {
		return 0
	}
	d := time.Until(c.cooldownUntil)
	if d < 0 {
		return 0
	}
	return d
}

func (c *Client) setCooldown(d time.Duration) {
	c.cooldownMu.Lock()
	defer c.cooldownMu.Unlock()
	t := time.Now().Add(d)
	if t.After(c.cooldownUntil) {
		c.cooldownUntil = t
	}
}

// reserveSimSlot acquires one of MaxConcurrentSims submission slots, bounding
// the number of concurrent POST /simulations in flight (NOT the number of
// simulations running on BRAIN — the slot is released once the create returns,
// before WaitForSimulation polls). Returns a release func the caller must
// defer. Honors ctx: a caller cancelled or timed-out while queued for a slot
// returns ctx.Err() instead of blocking indefinitely.
func (c *Client) reserveSimSlot(ctx context.Context) (func(), error) {
	select {
	case c.simSem <- struct{}{}:
		return func() { <-c.simSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// checkBudget enforces the daily quota gate. kind is "sim" or "submit".
// Returns ErrDailyBudgetExhausted if the gate is set and exceeded.
func (c *Client) checkBudget(kind string) error {
	day := challengeDayStr(time.Now())
	c.budgetMu.Lock()
	defer c.budgetMu.Unlock()
	if c.budgetDay != day {
		c.budgetDay = day
		c.budgetSims = 0
		c.budgetSubs = 0
	}
	switch kind {
	case "sim":
		if c.dailyBudget.Sims > 0 && c.budgetSims >= c.dailyBudget.Sims {
			return ErrDailyBudgetExhausted
		}
		c.budgetSims++
	case "submit":
		if c.dailyBudget.Submits > 0 && c.budgetSubs >= c.dailyBudget.Submits {
			return ErrDailyBudgetExhausted
		}
		c.budgetSubs++
	}
	return nil
}

// challengeDayStr returns the BRAIN challenge-day string: each submission is
// attributed to its EDT (fixed UTC-4) calendar day by the MIDNIGHT (00:00)
// boundary — NOT DST-aware Eastern, so the boundary stays at 04:00 UTC all
// year (no EST fallback in winter). BRAIN's own dateSubmitted carries an
// explicit -04:00 offset confirming this. The 3 AM ET event is only the
// challenge / paybasement DATA refresh, NOT the day-attribution boundary — an
// earlier -3h shift here mis-filed every 00:00–03:00 EDT call into the
// previous day. Mirrors the TS helper brainDay().
func challengeDayStr(now time.Time) string {
	return now.UTC().Add(-4 * time.Hour).Format("2006-01-02")
}

// joinURL builds an absolute URL from a path/qs pair under c.baseURL.
func (c *Client) joinURL(path string, qs url.Values) string {
	u := *c.baseURL
	u.Path = singleSlash(u.Path, path)
	if qs != nil {
		u.RawQuery = qs.Encode()
	}
	return u.String()
}

// singleSlash joins two path segments with exactly one slash between them.
func singleSlash(a, b string) string {
	switch {
	case a == "":
		return b
	case len(a) > 0 && a[len(a)-1] == '/' && len(b) > 0 && b[0] == '/':
		return a + b[1:]
	case len(a) > 0 && a[len(a)-1] != '/' && (len(b) == 0 || b[0] != '/'):
		return a + "/" + b
	default:
		return a + b
	}
}

// queryParams is a small fluent builder over url.Values that skips empty or
// non-positive optional fields, replacing the repetitive `if x != "" { qs.Set }`
// guards in the list endpoints. Methods return the receiver so calls chain;
// the underlying map is shared across the chain.
type queryParams struct{ v url.Values }

func newQuery() queryParams { return queryParams{v: url.Values{}} }

// set always writes the pair (for required fields).
func (q queryParams) set(key, val string) queryParams {
	q.v.Set(key, val)
	return q
}

// setIfNotEmpty writes the pair only when val is non-empty.
func (q queryParams) setIfNotEmpty(key, val string) queryParams {
	if val != "" {
		q.v.Set(key, val)
	}
	return q
}

// setIfPositive writes key=val only when val > 0.
func (q queryParams) setIfPositive(key string, val int) queryParams {
	if val > 0 {
		q.v.Set(key, strconv.Itoa(val))
	}
	return q
}

func (q queryParams) values() url.Values { return q.v }

// basicAuthHeader produces the value for Authorization: Basic ...
func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// defaultCaptchaSolverFunc is settable via SetDefaultCaptchaSolver. We avoid
// a hard import-time dep on pkg/captcha/altcha so the brainapi package can
// be used without pulling captcha code.
var defaultCaptchaSolverFunc atomic.Pointer[CaptchaSolver]

// SetDefaultCaptchaSolver registers the captcha solver used by NewClient
// when Options.CaptchaSolver is nil. The altcha package calls this from
// its init(). Concurrent-safe.
func SetDefaultCaptchaSolver(s CaptchaSolver) {
	defaultCaptchaSolverFunc.Store(&s)
}

func defaultCaptchaSolver() CaptchaSolver {
	if p := defaultCaptchaSolverFunc.Load(); p != nil {
		return *p
	}
	return nil
}

// ErrInvalidArgument is returned when the SDK rejects a caller-supplied value
// before dispatching the request (e.g. an empty alpha id).
var ErrInvalidArgument = errors.New("brainapi: invalid argument")
