// brainapi is the cross-platform CLI for the BRAIN platform API. Every
// subcommand maps 1:1 to a documented HTTP endpoint; output is stable JSON
// on stdout, structured logs on stderr, and exit codes are documented in
// the README so shell pipelines can rely on them.
package main

import (
	"context"
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
	baseURL      string
	profile      string
	proxy        string
	cookieJar    string
	email        string
	password     string
	timeout      time.Duration
	maxLongPolls int
	logLevel     string
	output       string
}

// defaultMaxLongPolls is the poll cap when the flag is unset / non-positive.
// Mirrors Options.MaxLongPolls's own zero-value default so the CLI and the
// library agree on 60.
const defaultMaxLongPolls = 60

// resolveMaxLongPolls normalizes the --max-long-polls flag: a non-positive value
// falls back to the default. A slow multi-simulation (parent running up to 10
// children sequentially) can outrun the 60-poll default, so `wait-multi` callers
// raise it; every other command keeps 60.
func resolveMaxLongPolls(v int) int {
	if v <= 0 {
		return defaultMaxLongPolls
	}
	return v
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
	pf.IntVar(&gf.maxLongPolls, "max-long-polls", defaultMaxLongPolls, "Max long-poll iterations for wait loops (raise for slow multi-simulations)")
	pf.StringVar(&gf.logLevel, "log-level", "warn", "Log level: error|warn|info|debug")
	pf.StringVar(&gf.output, "output", "json", "Output format (only 'json' is currently supported)")

	root.AddCommand(
		newAuthCmd(),
		newAlphasCmd(),
		newSimulationsCmd(),
		newUsersCmd(),
		newMessagesCmd(),
		newSchemaCmd(),
		newRegisterCmd(),
		newEmailCmd(),
		newPasswordCmd(),
		newFeedbackCmd(),
		newDescribeCmd(),
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
		MaxLongPolls:  resolveMaxLongPolls(gf.maxLongPolls),
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
// JSON envelope on stdout, plus the right exit code on stderr. The kind and
// details come from brainapi.Classify (shared with the brainapi-mcp server, so
// both consumption modes classify identically); the exit code is looked up from
// the same staticErrorKinds table that `describe` publishes, so the CLI's exit
// codes cannot drift from the documented spec.
func writeErr(err error) {
	kind, details := brainapi.Classify(err)
	// A nil map boxed in `any` would serialize as "details":null; keep the
	// envelope's omitempty behavior by passing an untyped nil for detail-less kinds.
	var d any
	if details != nil {
		d = details
	}
	_ = jsonout.Failure(kind, err.Error(), d)
	os.Exit(exitCodeForKind(kind))
}

// exitCodeForKind returns the process exit code for an error kind, sourced from
// the describe table. Unknown kinds fall back to the generic API exit code.
func exitCodeForKind(kind string) int {
	for _, k := range staticErrorKinds {
		if k.Kind == kind {
			return k.ExitCode
		}
	}
	return exitAPI
}

func writeOK(data any) {
	if err := jsonout.Success(data); err != nil {
		fmt.Fprintln(os.Stderr, "brainapi: write output:", err)
		os.Exit(exitNetwork)
	}
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
