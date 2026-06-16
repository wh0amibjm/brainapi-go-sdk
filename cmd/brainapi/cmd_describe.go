package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wh0amibjm/brainapi-go-sdk/internal/version"
)

// describeSpec is the machine-readable SDK protocol spec emitted by
// `brainapi describe`. Downstream wrappers in other languages can codegen
// typed clients from this. Static sections (envelope/exitCodes/errorKinds/
// nonObviousContracts) mirror docs/sdk-protocol.md and are kept in sync by
// code review. The commands section is auto-walked from the cobra tree, so
// it cannot drift from the binary's actual surface.
type describeSpec struct {
	Version             string          `json:"version"`
	Envelope            envelopeSpec    `json:"envelope"`
	ExitCodes           []exitCodeSpec  `json:"exitCodes"`
	ErrorKinds          []errorKindSpec `json:"errorKinds"`
	Commands            []commandSpec   `json:"commands"`
	NonObviousContracts []contractSpec  `json:"nonObviousContracts"`
}

type envelopeSpec struct {
	Success string `json:"success"`
	Failure string `json:"failure"`
}

type exitCodeSpec struct {
	Code  int      `json:"code"`
	Name  string   `json:"name"`
	Kinds []string `json:"kinds,omitempty"`
}

type errorKindSpec struct {
	Kind         string `json:"kind"`
	ExitCode     int    `json:"exitCode"`
	DetailsShape string `json:"detailsShape,omitempty"`
}

type commandSpec struct {
	Path     []string   `json:"path"`
	Short    string     `json:"short"`
	Args     string     `json:"args,omitempty"`
	Flags    []flagSpec `json:"flags,omitempty"`
	LongPoll bool       `json:"longPoll,omitempty"`
}

type flagSpec struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage,omitempty"`
}

type contractSpec struct {
	ID      string `json:"id"`
	Topic   string `json:"topic"`
	Summary string `json:"summary"`
	Ref     string `json:"ref"`
}

func newDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe",
		Short: "Emit a machine-readable spec of the SDK protocol (for codegen consumers)",
		Long: `Emit a JSON spec of brainapi's stable contract: envelope shapes,
exit code -> error.kind mapping, every subcommand's path/args/flags, and the
list of non-obvious schema contracts. Use this to codegen typed client
wrappers in other languages.

The command tree is auto-walked from cobra at runtime, so it cannot drift
from the binary's actual surface. Static sections mirror docs/sdk-protocol.md
and are kept in sync by code review.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			writeOK(buildDescribeSpec(cmd.Root()))
			return nil
		},
	}
}

func buildDescribeSpec(root *cobra.Command) describeSpec {
	return describeSpec{
		Version:             version.String(),
		Envelope:            staticEnvelope,
		ExitCodes:           staticExitCodes,
		ErrorKinds:          staticErrorKinds,
		Commands:            walkCommands(root, nil),
		NonObviousContracts: staticContracts,
	}
}

// longPollCommands is keyed by space-joined command path. Encoded here
// rather than in the command builders themselves so it stays close to the
// spec (the long-poll contract is the wrapper-relevant fact; the builder
// just cares about wiring).
var longPollCommands = map[string]bool{
	"alphas check":       true,
	"alphas submit":      true,
	"alphas pnl":         true,
	"alphas corr":        true,
	"alphas performance": true,
	"simulations wait":   true,
}

func walkCommands(c *cobra.Command, prefix []string) []commandSpec {
	var out []commandSpec
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		path := make([]string, 0, len(prefix)+1)
		path = append(path, prefix...)
		path = append(path, sub.Name())
		if len(sub.Commands()) > 0 {
			out = append(out, walkCommands(sub, path)...)
			continue
		}
		spec := commandSpec{
			Path:  path,
			Short: sub.Short,
			Args:  argsFromUse(sub.Use),
			Flags: collectFlags(sub),
		}
		if longPollCommands[strings.Join(path, " ")] {
			spec.LongPoll = true
		}
		out = append(out, spec)
	}
	return out
}

// argsFromUse strips the command name from cobra's Use string, returning
// just the args/flags description (e.g. "submit <alphaId>" -> "<alphaId>").
func argsFromUse(use string) string {
	parts := strings.SplitN(use, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func collectFlags(c *cobra.Command) []flagSpec {
	var out []flagSpec
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		out = append(out, flagSpec{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
		})
	})
	return out
}

var staticEnvelope = envelopeSpec{
	Success: `{"ok":true,"data":<endpoint-specific>}`,
	Failure: `{"ok":false,"error":{"kind":"<enum>","message":"<human>","details":<any>}}`,
}

var staticExitCodes = []exitCodeSpec{
	{Code: 0, Name: "OK"},
	{Code: 2, Name: "USAGE", Kinds: []string{"no_output", "invalid_argument"}},
	{Code: 3, Name: "RATE_LIMIT", Kinds: []string{"rate_limit", "cooldown"}},
	{Code: 4, Name: "BANNED", Kinds: []string{"banned", "not_verified"}},
	{Code: 5, Name: "DRF_VALIDATION", Kinds: []string{"drf_validation"}},
	{Code: 6, Name: "API", Kinds: []string{"api", "error", "not_authenticated", "permission_denied", "long_poll_exceeded"}},
	{Code: 7, Name: "BUDGET", Kinds: []string{"budget"}},
	{Code: 8, Name: "NETWORK", Kinds: []string{"context", "network"}},
	{Code: 10, Name: "PERSONA", Kinds: []string{"persona_inquiry"}},
}

var staticErrorKinds = []errorKindSpec{
	{Kind: "api", ExitCode: 6, DetailsShape: "{status:int, method:string, url:string, body:any}"},
	{Kind: "rate_limit", ExitCode: 3, DetailsShape: "{status:int, retry_after_ms:int, cooldown:bool, body:any}"},
	{Kind: "banned", ExitCode: 4, DetailsShape: "{streak:int, reason:string}"},
	{Kind: "permission_denied", ExitCode: 6, DetailsShape: "{status:int, detail:string, body:any}"},
	{Kind: "not_verified", ExitCode: 4, DetailsShape: "{status:int, body:any}"},
	{Kind: "drf_validation", ExitCode: 5, DetailsShape: "{status:int, url:string, fields:{<field>:[<msg>,...]}}"},
	{Kind: "persona_inquiry", ExitCode: 10, DetailsShape: "{inquiry:string}"},
	{Kind: "budget", ExitCode: 7},
	{Kind: "not_authenticated", ExitCode: 6},
	{Kind: "cooldown", ExitCode: 3},
	{Kind: "long_poll_exceeded", ExitCode: 6},
	{Kind: "context", ExitCode: 8},
	{Kind: "network", ExitCode: 8},
	{Kind: "invalid_argument", ExitCode: 2},
	{Kind: "error", ExitCode: 6},
	{Kind: "no_output", ExitCode: 2},
}

var staticContracts = []contractSpec{
	{
		ID:      "activities-current-is-mtd",
		Topic:   "users activities",
		Summary: "ActivityStream.current is month-to-date, not today. Today's row lives in records[] keyed by date === <today's BRAIN day>.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "brain-day-3am-et",
		Topic:   "daily windows",
		Summary: "BRAIN day rolls at 3 AM US/Eastern, not local midnight. Affects activities yesterday/current, daily budget, Challenge score windows.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "activities-decode-required",
		Topic:   "users activities",
		Summary: "records.records is an array of positional tuples; column names live in records.schema.properties[*].name. Non-Go consumers MUST pass --decode to get {colName:value} dicts.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "raw-message-fields",
		Topic:   "forward compatibility",
		Summary: "Alpha.Team, Alpha.Color, Alpha.Category, Competition.Team, Leaderboard.University are json.RawMessage in the SDK. BRAIN may reshape them; type as unknown/any in wrappers.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "retry-after-float",
		Topic:   "transport",
		Summary: "Retry-After is a float seconds string ('5.0' not '5'). Parse with parseFloat, clamp [1,120] for 429 and [0.5,30] for 503.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "wait-terminates-on-alpha",
		Topic:   "simulations wait",
		Summary: "Long-poll terminates whenever s.alpha is populated, regardless of s.status. BRAIN occasionally returns {status:'WARNING', alpha:'<id>'} for sims with soft flags; an alpha id is the success signal.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "submit-corr-only-via-submit",
		Topic:   "alphas submit",
		Summary: "SELF_CORRELATION verdict appears only via POST + GET long-poll on /alphas/{id}/submit. GET /alphas/{id} stays result:'PENDING' indefinitely for unsubmitted alphas.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "presubmit-corr-gate",
		Topic:   "alphas corr",
		Summary: "GET /alphas/{id}/correlations/self returns {schema,records,min,max} after a 503-queued long-poll (cached after first run). Gate SubmitAlpha on max < 0.7 — same threshold as the post-submit SELF_CORRELATION check, but free of submit-budget cost. /correlations/prod is 403 on IQC consultant tier through July 2026.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "before-after-performance",
		Topic:   "alphas performance",
		Summary: "GET /competitions/{cid}/alphas/{id}/before-and-after-performance projects submit impact after a 503-queued long-poll: {score:{before,after}, stats:{before,after}, yearlyStats:{before,after}, pnl}. Competition-scoped — caller supplies the competition id. yearlyStats.{before,after} and pnl are positional {schema,records} blocks; pnl columns are date, beforePnL, afterPnL. Free of submit-budget cost.",
		Ref:     "docs/protocol.md",
	},
	{
		ID:      "list-endpoint-envelopes-diverge",
		Topic:   "list endpoints",
		Summary: "Three different shapes: GET /operators returns a bare JSON array; GET /data-fields returns {count,results}; GET /users/self/alphas returns full DRF {count,next,previous,results}.",
		Ref:     "docs/sdk-protocol.md",
	},
	{
		ID:      "register-payload",
		Topic:   "register",
		Summary: "POST /users body uses education.graduationYear (NOT gradYear) and has no address.zip — live-confirmed 2026-05-19. Education.degree must be BACHELORS / MASTERS / ASSOCIATE. The SDK auto-fetches /captcha and injects the solved Altcha PoW into auxiliary.captcha when empty.",
		Ref:     "docs/protocol.md",
	},
}
