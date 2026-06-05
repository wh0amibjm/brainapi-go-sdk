// Command brainapi-mcp exposes the BRAIN API as an MCP server over stdio,
// reusing pkg/brainapi directly (no subprocess). It covers the full SDK
// surface: read-only (GET) tools are always registered; every mutating
// operation (submit, simulations-create, register, email/password, auth)
// requires --enable-writes. submit additionally passes a self-correlation
// gate and a confirm flag.
//
// Credentials come from BRAINAPI_USER / BRAINAPI_PASS (set them via the MCP
// client's `env` block). All logs go to stderr — stdout carries JSON-RPC only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wh0amibjm/brainapi-go-sdk/internal/version"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/feedback"
)

func main() {
	enableWrites := flag.Bool("enable-writes", false,
		"register mutating tools (submit, simulations-create, register, email/password, auth); off by default")
	profileName := flag.String("profile", "",
		"TLS impersonation profile (default chrome131, or auto:<email>)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	email := os.Getenv("BRAINAPI_USER")
	jarPath := ""
	if email != "" {
		jarPath = defaultJarPath(email)
	}

	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       firstNonEmpty(os.Getenv("BRAINAPI_BASE_URL"), "https://api.worldquantbrain.com"),
		Profile:       brainapi.ParseProfile(*profileName),
		Proxy:         os.Getenv("BRAINAPI_PROXY"),
		CookieJarPath: jarPath,
		Timeout:       15 * time.Second,
		MaxRetries:    3,
		MaxLongPolls:  60,
		BanThreshold:  3,
		Logger:        logger,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
		Email:         email,
		Password:      os.Getenv("BRAINAPI_PASS"),
	})
	if err != nil {
		logger.Error("client init failed", "err", err)
		os.Exit(1)
	}

	s := newServer(cl, *enableWrites)
	if *enableWrites {
		logger.Warn("write tools enabled — submit / simulations-create / register / email / password / auth are registered")
	}

	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// newServer builds the MCP server: read-only tools are always registered, and
// the mutating tools are registered only when enableWrites is set. Extracted
// from main so tests can assert the read/write tool gating without spawning a
// subprocess (connect a client over an in-memory transport and list tools).
func newServer(cl *brainapi.Client, enableWrites bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "brainapi", Version: version.Version}, nil)
	registerReadTools(s, cl)
	registerFeedbackTool(s)
	if enableWrites {
		registerWriteTools(s, cl)
	}
	return s
}

// ─── argument schemas ─────────────────────────────────────────────────────────

type noArgs struct{}

type idArg struct {
	ID string `json:"id" jsonschema:"BRAIN alpha id, e.g. qMPjAxnO"`
}

type simIDArg struct {
	ID string `json:"id" jsonschema:"simulation id (the Location returned by simulations_create)"`
}

type activitiesArg struct {
	Kind string `json:"kind" jsonschema:"one of: base-payment, other-payment, simulations, submissions"`
}

type listAlphasArg struct {
	Status string `json:"status,omitempty" jsonschema:"ACTIVE | UNSUBMITTED | DECOMMISSIONED | empty for all"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size (BRAIN default 100)"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
	Order  string `json:"order,omitempty" jsonschema:"sort key, e.g. -dateCreated"`
}

type messagesArg struct {
	Type   string `json:"type,omitempty" jsonschema:"ANNOUNCEMENT | NOTIFICATION | empty for all"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
	Order  string `json:"order,omitempty" jsonschema:"sort key, e.g. -dateCreated"`
}

type dataFieldsArg struct {
	Region         string `json:"region" jsonschema:"required, e.g. USA"`
	Universe       string `json:"universe" jsonschema:"required, e.g. TOP3000"`
	Delay          int    `json:"delay" jsonschema:"required, 0 or 1"`
	InstrumentType string `json:"instrument_type,omitempty" jsonschema:"e.g. EQUITY"`
	Limit          int    `json:"limit,omitempty"`
	Offset         int    `json:"offset,omitempty"`
}

type performanceArg struct {
	CompetitionID string `json:"competition_id" jsonschema:"competition id"`
	AlphaID       string `json:"alpha_id" jsonschema:"alpha id"`
}

// ─── read-only tools (always registered) ──────────────────────────────────────

func registerReadTools(s *mcp.Server, cl *brainapi.Client) {
	addTool(s, "probe", "GET /authentication: probe the live session (auto-login on 401); returns user id + permissions.",
		func(ctx context.Context, _ noArgs) (*brainapi.SessionInfo, error) { return cl.Probe(ctx) })

	addTool(s, "whoami", "GET /users/self: full profile of the authenticated user.",
		func(ctx context.Context, _ noArgs) (*brainapi.User, error) { return cl.Self(ctx) })

	addTool(s, "competitions", "GET /users/self/competitions: competitions the user is in.",
		func(ctx context.Context, _ noArgs) (*brainapi.Page[brainapi.Competition], error) {
			return cl.Competitions(ctx)
		})

	addTool(s, "operators", "GET /operators: the operator catalog (bare array).",
		func(ctx context.Context, _ noArgs) ([]brainapi.Operator, error) { return cl.Operators(ctx) })

	addTool(s, "get_alpha", "GET /alphas/{id}: fetch the full alpha record.",
		func(ctx context.Context, in idArg) (*brainapi.Alpha, error) { return cl.GetAlpha(ctx, in.ID) })

	addTool(s, "check_alpha", "GET /alphas/{id}/check: long-poll the pre-submit validation checks (raw IsBlock).",
		func(ctx context.Context, in idArg) (*brainapi.IsBlock, error) { return cl.CheckAlpha(ctx, in.ID) })

	addTool(s, "check_alpha_decoded", "GET /alphas/{id}/check, decoded to (passed, messages) including WARNING text.",
		func(ctx context.Context, in idArg) (checkDecoded, error) {
			passed, msgs, err := cl.AlphaCheckBody(ctx, in.ID)
			return checkDecoded{Passed: passed, Messages: msgs}, err
		})

	addTool(s, "self_correlation", "GET /alphas/{id}/correlations/self: pre-submit correlation gate. Submittable when max < 0.7.",
		func(ctx context.Context, in idArg) (*brainapi.SelfCorrelationBlock, error) {
			return cl.AlphaSelfCorrelation(ctx, in.ID)
		})

	addTool(s, "alpha_pnl", "GET /alphas/{id}/recordsets/pnl: the alpha's PnL series.",
		func(ctx context.Context, in idArg) (*brainapi.PnLSeries, error) { return cl.AlphaPnL(ctx, in.ID) })

	addTool(s, "performance", "GET /competitions/{cid}/alphas/{id}/before-and-after-performance: projected submit impact.",
		func(ctx context.Context, in performanceArg) (*brainapi.BeforeAndAfterPerformance, error) {
			return cl.BeforeAndAfterPerformance(ctx, in.CompetitionID, in.AlphaID)
		})

	addTool(s, "activities", "GET /users/self/activities/{kind}: activity counts. Note: 'current' is month-to-date; the BRAIN day rolls at 3 AM US/Eastern.",
		func(ctx context.Context, in activitiesArg) (*brainapi.ActivityStream, error) {
			return cl.Activities(ctx, brainapi.ActivityKind(in.Kind))
		})

	addTool(s, "list_alphas", "GET /users/self/alphas: one page of the user's alphas.",
		func(ctx context.Context, in listAlphasArg) (*brainapi.Page[brainapi.Alpha], error) {
			return cl.ListAlphas(ctx, brainapi.ListAlphasOptions{Status: in.Status, Limit: in.Limit, Offset: in.Offset, Order: in.Order})
		})

	addTool(s, "list_alphas_all", "GET /users/self/alphas, drained across ALL pages into one array (may be large/slow).",
		func(ctx context.Context, in listAlphasArg) ([]brainapi.Alpha, error) {
			return drainChan(cl.ListAlphasAll(ctx, brainapi.ListAlphasOptions{Status: in.Status, Limit: in.Limit, Offset: in.Offset, Order: in.Order}))
		})

	addTool(s, "messages", "GET /users/self/messages: one page of the notification feed.",
		func(ctx context.Context, in messagesArg) (*brainapi.Page[brainapi.Message], error) {
			return cl.Messages(ctx, brainapi.ListMessagesOptions{Type: in.Type, Limit: in.Limit, Offset: in.Offset, Order: in.Order})
		})

	addTool(s, "messages_all", "GET /users/self/messages, drained across ALL pages into one array.",
		func(ctx context.Context, in messagesArg) ([]brainapi.Message, error) {
			return drainChan(cl.MessagesAll(ctx, brainapi.ListMessagesOptions{Type: in.Type, Limit: in.Limit, Offset: in.Offset, Order: in.Order}))
		})

	addTool(s, "data_fields", "GET /data-fields: one page of the tier-gated data-field catalog. region + universe + delay are mandatory.",
		func(ctx context.Context, in dataFieldsArg) (*brainapi.DataFieldsPage, error) {
			return cl.DataFields(ctx, dfQuery(in))
		})

	addTool(s, "data_fields_all", "GET /data-fields, drained across ALL pages into one array. region + universe + delay are mandatory.",
		func(ctx context.Context, in dataFieldsArg) ([]brainapi.DataField, error) {
			return drainChan(cl.DataFieldsAll(ctx, dfQuery(in)))
		})

	addTool(s, "get_simulation", "GET /simulations/{id}: simulation status/result.",
		func(ctx context.Context, in simIDArg) (*brainapi.Simulation, error) {
			return cl.GetSimulation(ctx, in.ID)
		})

	addTool(s, "wait_simulation", "GET /simulations/{id}, long-polled until the simulation reaches a terminal state.",
		func(ctx context.Context, in simIDArg) (*brainapi.Simulation, error) {
			return cl.WaitForSimulation(ctx, in.ID)
		})

	addTool(s, "captcha_challenge", "GET /captcha: the raw Altcha challenge JSON (used internally by register).",
		func(ctx context.Context, _ noArgs) (string, error) {
			b, err := cl.FetchCaptchaChallenge(ctx)
			return string(b), err
		})
}

// ─── feedback tool (always registered) ────────────────────────────────────────

type reportIssueArg struct {
	Title    string `json:"title" jsonschema:"one-line summary of the SDK problem"`
	Body     string `json:"body" jsonschema:"details: what you did, what you expected, and what actually happened (markdown ok)"`
	Category string `json:"category,omitempty" jsonschema:"triage bucket: bug | docs | enhancement | question (default bug)"`
	// Optional (unlike submit_alpha's confirm): the safe default is harmless —
	// omitting it yields a draft URL, never a post.
	Confirm bool `json:"confirm,omitempty" jsonschema:"set true to actually open the GitHub issue (needs a token configured); omit/false returns a click-to-file draft URL"`
}

// registerFeedbackTool wires the agent feedback channel. It is registered
// unconditionally (independent of --enable-writes) so an agent on the
// default read-only surface can still report SDK defects. Filing opens an
// issue on a public tracker, so — like submit_alpha — it is gated: it only
// POSTs when a token is configured AND confirm=true; otherwise it returns a
// prefilled draft URL for a human to open.
func registerFeedbackTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "report_issue",
		Description: "Report a defect in the brainapi SDK itself — a wrong response shape, a " +
			"mis-classified error, a stale/incorrect doc, or a tool that errors unexpectedly. " +
			"This is for the SDK, NOT for BRAIN platform issues or the user's alpha/strategy work. " +
			"Without confirm=true (or without a GitHub token configured) it returns a click-to-file " +
			"draft URL; with both it opens the issue via the GitHub API.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in reportIssueArg) (*mcp.CallToolResult, *feedback.Result, error) {
		res, err := feedback.File(
			ctx,
			feedback.Report{Title: in.Title, Body: in.Body, Category: in.Category, Surface: "mcp"},
			feedback.RuntimeEnv(version.Version, version.Commit),
			feedback.ConfigFromEnv(),
			in.Confirm,
		)
		if err != nil {
			return nil, nil, classifyErr(err)
		}
		return nil, &res, nil
	})
}

// ─── mutating tools (only with --enable-writes) ───────────────────────────────

type submitArg struct {
	ID      string `json:"id" jsonschema:"alpha id to submit"`
	Confirm bool   `json:"confirm" jsonschema:"must be true to actually submit; submit consumes a scarce daily slot and is near-irreversible"`
}

type submitResult struct {
	Submitted bool              `json:"submitted"`
	Blocked   bool              `json:"blocked,omitempty"`
	CorrMax   *float64          `json:"corr_max,omitempty"`
	Note      string            `json:"note"`
	Verdict   *brainapi.Verdict `json:"verdict,omitempty"`
}

type emailArg struct {
	Email     string `json:"email" jsonschema:"account email"`
	Recaptcha string `json:"recaptcha,omitempty" jsonschema:"reCAPTCHA token if the endpoint requires one"`
}

type jwtArg struct {
	JWT string `json:"jwt" jsonschema:"the token from the verification/reset email link"`
}

type resetArg struct {
	JWT         string `json:"jwt" jsonschema:"the token from the reset email link"`
	NewPassword string `json:"new_password" jsonschema:"the new password to set"`
}

type personaArg struct {
	Inquiry string `json:"inquiry" jsonschema:"the persona inquiry id returned by login"`
}

func registerWriteTools(s *mcp.Server, cl *brainapi.Client) {
	// submit_alpha: extra-gated (corr < 0.7 + confirm).
	mcp.AddTool(s, &mcp.Tool{
		Name: "submit_alpha",
		Description: "POST /alphas/{id}/submit — DESTRUCTIVE: consumes a scarce daily submit slot. " +
			"Always runs the self-correlation gate (max < 0.7) first, and requires confirm=true " +
			"(otherwise it returns a dry-run without submitting).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in submitArg) (*mcp.CallToolResult, *submitResult, error) {
		block, err := cl.AlphaSelfCorrelation(ctx, in.ID)
		if err != nil {
			return nil, nil, classifyErr(fmt.Errorf("corr gate failed: %w", err))
		}
		var corrMax *float64
		if block != nil {
			corrMax = block.Max
		}
		if corrMax != nil && *corrMax >= 0.7 {
			return nil, &submitResult{
				Submitted: false, Blocked: true, CorrMax: corrMax,
				Note: fmt.Sprintf("self-correlation max %.4f >= 0.7 — would fail SELF_CORRELATION; not submitting (slot saved)", *corrMax),
			}, nil
		}
		if !in.Confirm {
			return nil, &submitResult{
				Submitted: false, CorrMax: corrMax,
				Note: "dry-run: corr gate passed. Re-call with confirm=true to submit (consumes a daily slot).",
			}, nil
		}
		v, err := cl.SubmitAlpha(ctx, in.ID)
		if err != nil {
			return nil, nil, classifyErr(fmt.Errorf("submit failed: %w", err))
		}
		return nil, &submitResult{Submitted: true, CorrMax: corrMax, Note: "submitted", Verdict: v}, nil
	})

	addTool(s, "simulations_create", "POST /simulations — DESTRUCTIVE: consumes simulation quota. Provide a SimulationRequest: 'type' (REGULAR|SUPER|COMBO), the alpha expression in 'regular' (or 'super'), and 'settings' (instrumentType, region, universe, delay, decay, neutralization, truncation, …). Returns the simulation id; poll it with wait_simulation.",
		func(ctx context.Context, in brainapi.SimulationRequest) (locResult, error) {
			loc, err := cl.CreateSimulation(ctx, in)
			return locResult{Location: loc}, err
		})

	addTool(s, "register", "POST /users — DESTRUCTIVE: creates an account. Provide a RegisterInput: email, firstName, lastName, fullName, gender, address{country,…}, education{university, major, degree (BACHELORS|MASTERS|ASSOCIATE), graduationYear}, auxiliary{agree, password, confirmation}. The Altcha captcha is auto-solved by the SDK — leave auxiliary.captcha empty.",
		func(ctx context.Context, in brainapi.RegisterInput) (*brainapi.User, error) {
			return cl.Register(ctx, in)
		})

	addTool(s, "login", "POST /authentication — establish a session using the configured BRAINAPI_USER/PASS.",
		func(ctx context.Context, _ noArgs) (*brainapi.Session, error) {
			return cl.Login(ctx, os.Getenv("BRAINAPI_USER"), os.Getenv("BRAINAPI_PASS"))
		})

	addTool(s, "logout", "DELETE /authentication — end the current session.",
		func(ctx context.Context, _ noArgs) (okResult, error) { return okWrap(cl.Logout(ctx)) })

	addTool(s, "email_verify", "POST /user/email/verify — verify an email using the token from the email link.",
		func(ctx context.Context, in jwtArg) (okResult, error) { return okWrap(cl.VerifyEmail(ctx, in.JWT)) })

	addTool(s, "email_reverify", "POST /user/email/reverify — re-send the verification email.",
		func(ctx context.Context, in emailArg) (okResult, error) {
			return okWrap(cl.ReverifyEmail(ctx, in.Email, in.Recaptcha))
		})

	addTool(s, "password_forgot", "POST /user/password/forgot — trigger a password-reset email.",
		func(ctx context.Context, in emailArg) (okResult, error) {
			return okWrap(cl.ForgotPassword(ctx, in.Email, in.Recaptcha))
		})

	addTool(s, "password_reset", "POST /user/password/reset — set a new password using the token from the reset email.",
		func(ctx context.Context, in resetArg) (okResult, error) {
			return okWrap(cl.ResetPassword(ctx, in.JWT, in.NewPassword))
		})

	addTool(s, "persona_complete", "POST /authentication/persona — complete a 2FA persona inquiry (rare; dead-code in current BRAIN). Uses configured creds.",
		func(ctx context.Context, in personaArg) (*brainapi.Session, error) {
			return cl.CompletePersona(ctx, in.Inquiry, os.Getenv("BRAINAPI_USER"), os.Getenv("BRAINAPI_PASS"))
		})
}

// ─── result shapes ────────────────────────────────────────────────────────────

type checkDecoded struct {
	Passed   bool     `json:"passed"`
	Messages []string `json:"messages"`
}

type locResult struct {
	Location string `json:"location"`
}

type okResult struct {
	OK bool `json:"ok"`
}

func okWrap(err error) (okResult, error) {
	if err != nil {
		return okResult{}, err
	}
	return okResult{OK: true}, nil
}

func dfQuery(in dataFieldsArg) brainapi.DataFieldsQuery {
	return brainapi.DataFieldsQuery{
		InstrumentType: in.InstrumentType, Region: in.Region, Universe: in.Universe,
		Delay: in.Delay, Limit: in.Limit, Offset: in.Offset,
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// toolOut wraps every read tool's payload in an object: MCP structured-output
// schemas must be JSON objects, so a bare array (e.g. []Operator) is rejected.
type toolOut[T any] struct {
	Data T `json:"data"`
}

// addTool registers a tool from a plain (ctx, In) -> (T, error) function. On
// error the result is classified into the SDK's stable error taxonomy via
// classifyErr, so the agent receives a structured {kind,message,details} payload
// (as IsError text content) and can branch on the kind — rate_limit → back off,
// budget → stop, drf_validation → fix a field — instead of parsing English. This
// is the same taxonomy the CLI exposes in its {ok:false,error:{kind,...}} envelope.
func addTool[In, T any](s *mcp.Server, name, desc string, fn func(context.Context, In) (T, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, toolOut[T], error) {
			out, err := fn(ctx, in)
			if err != nil {
				return nil, toolOut[T]{}, classifyErr(err)
			}
			return nil, toolOut[T]{Data: out}, nil
		})
}

// structuredErr carries the SDK's stable error kind and details alongside the
// human message. The go-sdk renders a handler error through
// CallToolResult.SetError, which uses Error(); encoding the payload as a JSON
// string therefore delivers the structured classification to the agent via the
// error's text content, mirroring the CLI's {kind,message,details} envelope.
type structuredErr struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *structuredErr) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		return e.Message
	}
	return string(b)
}

// classifyErr wraps an SDK error into a structuredErr using brainapi.Classify,
// the error taxonomy shared with the CLI. Returns nil for a nil error.
func classifyErr(err error) error {
	if err == nil {
		return nil
	}
	kind, details := brainapi.Classify(err)
	return &structuredErr{Kind: kind, Message: err.Error(), Details: details}
}

// drainChan collects a (items, errs) channel pair from the SDK's *All iterators
// into a slice. Both channels are closed by the producer; errs is buffered and
// carries at most one error.
func drainChan[T any](items <-chan T, errs <-chan error) ([]T, error) {
	out := []T{}
	for it := range items {
		out = append(out, it)
	}
	if err := <-errs; err != nil {
		return out, err
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// defaultJarPath mirrors the CLI: a per-account cookie jar under UserCacheDir.
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
