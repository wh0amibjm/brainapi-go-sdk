// brainapi is the cross-platform CLI for the BRAIN platform API. Every
// subcommand maps 1:1 to a documented HTTP endpoint; output is stable JSON
// on stdout, structured logs on stderr, and exit codes are documented in
// the README so shell pipelines can rely on them.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/internal/jsonout"
	"github.com/wh0amibjm/brainapi-go-sdk/internal/version"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

// Exit codes form a stable contract with shell consumers.
const (
	exitOK             = 0
	exitUsage          = 2
	exitRateLimit      = 3
	exitBanned         = 4
	exitDRF            = 5
	exitAPI            = 6
	exitBudget         = 7
	exitNetwork        = 8
	exitPersonaInquiry = 10
)

// Global flags shared by every subcommand.
type globalFlags struct {
	baseURL   string
	profile   string
	proxy     string
	cookieJar string
	email     string
	password  string
	timeout   time.Duration
	logLevel  string
	output    string
}

var gf globalFlags

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		// Errors from RunE flow through writeErr; getting here means cobra
		// itself bailed (e.g. unknown flag).
		fmt.Fprintln(os.Stderr, "brainapi:", err)
		os.Exit(exitUsage)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "brainapi",
		Short:         "WorldQuant BRAIN platform API client",
		Long:          "brainapi wraps the BRAIN HTTP API behind a stable, JSON-in / JSON-out CLI. Each subcommand mirrors one documented endpoint.\n\nThe full endpoint reference, exit-code map, and configuration matrix are in README.md.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}
	pf := root.PersistentFlags()
	pf.StringVar(&gf.baseURL, "base-url", "", "BRAIN API base URL (default https://api.worldquantbrain.com, or $BRAINAPI_BASE_URL)")
	pf.StringVar(&gf.profile, "profile", "", "TLS impersonation profile: chrome131|chrome133|chrome144|safari16|safari-ios|firefox132 or auto:<email>")
	pf.StringVar(&gf.proxy, "proxy", "", "Proxy URL (or $BRAINAPI_PROXY)")
	pf.StringVar(&gf.cookieJar, "cookie-jar", "", "Path to file-backed cookie jar (default: platform-specific data dir keyed by email)")
	pf.StringVar(&gf.email, "user", "", "Account email (or $BRAINAPI_USER)")
	pf.StringVar(&gf.password, "pass", "", "Account password (or $BRAINAPI_PASS)")
	pf.DurationVar(&gf.timeout, "timeout", 15*time.Second, "Per-request HTTP timeout")
	pf.StringVar(&gf.logLevel, "log-level", "warn", "Log level: error|warn|info|debug")
	pf.StringVar(&gf.output, "output", "json", "Output format (only 'json' is currently supported)")

	root.AddCommand(
		newAuthCmd(),
		newAlphasCmd(),
		newSimulationsCmd(),
		newUsersCmd(),
		newSchemaCmd(),
		newRegisterCmd(),
		newEmailCmd(),
		newPasswordCmd(),
		newVersionCmd(),
	)
	return root
}

// newClient resolves the global flags + env into a Client. Always returns
// a usable client (defaults are sane) unless the cookie-jar path is bad.
func newClient(_ *cobra.Command) (*brainapi.Client, error) {
	baseURL := firstNonEmpty(gf.baseURL, os.Getenv("BRAINAPI_BASE_URL"), "https://api.worldquantbrain.com")
	proxy := firstNonEmpty(gf.proxy, os.Getenv("BRAINAPI_PROXY"))
	email := firstNonEmpty(gf.email, os.Getenv("BRAINAPI_USER"))
	pass := firstNonEmpty(gf.password, os.Getenv("BRAINAPI_PASS"))

	jarPath := gf.cookieJar
	if jarPath == "" && email != "" {
		jarPath = defaultJarPath(email)
	}

	logger := newLogger(gf.logLevel)

	opts := brainapi.Options{
		BaseURL:       baseURL,
		Profile:       brainapi.ParseProfile(gf.profile),
		Proxy:         proxy,
		CookieJarPath: jarPath,
		Timeout:       gf.timeout,
		MaxRetries:    3,
		MaxLongPolls:  60,
		BanThreshold:  3,
		Logger:        logger,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
		Email:         email,
		Password:      pass,
	}
	return brainapi.NewClient(opts)
}

// ctxWithSignal returns a context that's cancelled on SIGINT/SIGTERM.
func ctxWithSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// writeErr maps a Go error into the stable {ok:false, error:{kind,message,details}}
// JSON envelope on stdout, plus the right exit code on stderr.
//
//nolint:gocyclo // straight dispatch table; splitting helpers would obscure the mapping
func writeErr(err error) {
	var kind string
	var details any
	code := exitAPI

	var apiErr *brainapi.APIError
	var rlErr *brainapi.RateLimitError
	var banErr *brainapi.BannedError
	var drfErr *brainapi.DRFError
	var nvErr *brainapi.NotVerifiedError
	var personaErr *brainapi.PersonaInquiryError

	switch {
	case errors.As(err, &apiErr):
		kind = "api"
		details = map[string]any{
			"status": apiErr.Status, "method": apiErr.Method,
			"url": apiErr.URL, "body": tryJSON(apiErr.Body),
		}
		code = exitAPI
	case errors.As(err, &rlErr):
		kind = "rate_limit"
		details = map[string]any{
			"status": rlErr.Status, "retry_after_ms": rlErr.RetryAfter.Milliseconds(),
			"cooldown": rlErr.Cooldown,
		}
		code = exitRateLimit
	case errors.As(err, &banErr):
		kind = "banned"
		details = map[string]any{"streak": banErr.Streak, "reason": banErr.Reason}
		code = exitBanned
	case errors.As(err, &drfErr):
		kind = "drf_validation"
		details = drfErr.Fields
		code = exitDRF
	case errors.As(err, &nvErr):
		kind = "not_verified"
		code = exitBanned
	case errors.As(err, &personaErr):
		kind = "persona_inquiry"
		details = map[string]any{"inquiry": personaErr.Inquiry}
		code = exitPersonaInquiry
	case errors.Is(err, brainapi.ErrDailyBudgetExhausted):
		kind = "budget"
		code = exitBudget
	case errors.Is(err, brainapi.ErrNotAuthenticated):
		kind = "not_authenticated"
	case errors.Is(err, brainapi.ErrCooldown):
		kind = "cooldown"
		code = exitRateLimit
	case errors.Is(err, brainapi.ErrLongPollExceeded):
		kind = "long_poll_exceeded"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		kind = "context"
		code = exitNetwork
	default:
		kind = "error"
	}
	_ = jsonout.Failure(kind, err.Error(), details)
	os.Exit(code)
}

func writeOK(data any) {
	if err := jsonout.Success(data); err != nil {
		fmt.Fprintln(os.Stderr, "brainapi: write output:", err)
		os.Exit(exitNetwork)
	}
}

// tryJSON returns body parsed as any, falling back to the string form for
// non-JSON bodies. Keeps the error envelope readable when BRAIN returns a
// JSON body.
func tryJSON(b []byte) any {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return ""
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return rawMessageFromBytes(b)
	}
	return trimmed
}

// rawMessageFromBytes wraps a []byte so the JSON encoder emits it verbatim.
type rawJSON []byte

// MarshalJSON satisfies json.Marshaler. Error return is always nil but the
// signature is mandated by the interface.
func (r rawJSON) MarshalJSON() ([]byte, error) { //nolint:unparam // interface contract
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return []byte(r), nil
}

func rawMessageFromBytes(b []byte) rawJSON {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// defaultJarPath returns the platform-appropriate cache location for a
// per-account cookie jar.
func defaultJarPath(email string) string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = filepath.Join(os.TempDir(), "brainapi")
	} else {
		dir = filepath.Join(dir, "brainapi")
	}
	safe := url.PathEscape(strings.ToLower(strings.TrimSpace(email)))
	return filepath.Join(dir, "cookies-"+safe+".json")
}

// newLogger builds a stderr-targeted slog handler at the configured level.
func newLogger(lvl string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(lvl) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelWarn
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the brainapi version",
		RunE: func(_ *cobra.Command, _ []string) error {
			writeOK(map[string]string{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
			})
			return nil
		},
	}
}
