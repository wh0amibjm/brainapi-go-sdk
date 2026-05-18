package brainapi

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
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
// next BRAIN day boundary (3 AM ET). Set Sims or Submits to 0 to disable
// the gate for that operation type.
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
	captchaSolver     CaptchaSolver

	credMu   sync.RWMutex
	email    string
	password string

	tls *tlsHTTP

	consecutive403 atomic.Int32
	bannedFlag     atomic.Bool

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

// reserveSimSlot acquires one of MaxConcurrentSims slots. Returns a release
// func; the caller must always defer it. ctxDone wakes the caller if the
// context is cancelled while waiting.
func (c *Client) reserveSimSlot() func() {
	c.simSem <- struct{}{}
	return func() { <-c.simSem }
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

// challengeDayStr returns the BRAIN challenge-day string. BRAIN's day rolls
// over at 3 AM US/Eastern, not midnight UTC and not midnight local. Mirrors
// the TS helper of the same name.
func challengeDayStr(now time.Time) string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC
	}
	et := now.In(loc).Add(-3 * time.Hour)
	return et.Format("2006-01-02")
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
