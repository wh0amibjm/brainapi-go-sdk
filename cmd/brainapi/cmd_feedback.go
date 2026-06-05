package main

import (
	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/internal/version"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/feedback"
)

// newFeedbackCmd is the agent feedback channel for the CLI surface: when an
// agent (or human) driving brainapi hits a defect in the SDK itself, this opens
// a GitHub issue on the SDK repo. It mirrors the brainapi-mcp `report_issue`
// tool. Outward-facing, so it never posts without --confirm; without a token
// (or without --confirm) it prints a click-to-file draft URL instead.
func newFeedbackCmd() *cobra.Command {
	var (
		title    string
		body     string
		category string
		confirm  bool
	)
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Report a defect in the brainapi SDK itself (opens a GitHub issue)",
		Long: `Report a problem in the brainapi SDK — a wrong response shape, a
mis-classified error/exit code, a stale or incorrect doc, or a command that
errors unexpectedly. This is for the SDK, NOT for BRAIN platform questions or
your own alpha/strategy work.

Without --confirm (or without a GitHub token configured), this prints a
prefilled "new issue" URL for a human to open — no network call. With both a
token (BRAINAPI_FEEDBACK_TOKEN, else GITHUB_TOKEN/GH_TOKEN) and --confirm, it
files the issue via the GitHub API and returns its URL. Target repo defaults to
the SDK upstream; override with BRAINAPI_FEEDBACK_REPO=owner/repo.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := ctxWithSignal()
			defer cancel()
			res, err := feedback.File(
				ctx,
				feedback.Report{Title: title, Body: body, Category: category, Surface: "cli"},
				feedback.RuntimeEnv(version.Version, version.Commit),
				feedback.ConfigFromEnv(),
				confirm,
			)
			if err != nil {
				// File only errors on caller-side validation (empty title/body),
				// which MarkFlagRequired lets through as a present-but-empty flag.
				// Return it so it flows out as a usage error (exit 2) instead of
				// being mis-mapped to the server-side `error`/exit-6 bucket.
				return err
			}
			writeOK(res)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "one-line summary of the SDK problem (required)")
	f.StringVar(&body, "body", "", "details: what you did, expected, and saw — markdown ok (required)")
	f.StringVar(&category, "category", "bug", "triage bucket: bug|docs|enhancement|question")
	f.BoolVar(&confirm, "confirm", false, "actually open the GitHub issue (needs a token); without it you get a draft URL")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}
