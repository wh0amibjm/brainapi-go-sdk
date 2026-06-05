// Package feedback turns an agent's report of an SDK problem into a GitHub
// issue on the SDK's own repository. It is the feedback channel shared by the
// two agent-facing surfaces — the brainapi CLI (`brainapi feedback`) and the
// brainapi-mcp server (the `report_issue` tool) — so that when an agent driving
// the SDK hits a real defect (a wrong response shape, a mis-classified error, a
// stale doc, a tool that errors unexpectedly) the finding is recorded upstream
// instead of being lost in the chat.
//
// Two modes, picked automatically:
//
//   - github_api: when a token is configured (BRAINAPI_FEEDBACK_TOKEN, else
//     GITHUB_TOKEN, else GH_TOKEN) AND the caller confirms, the issue is filed
//     via POST /repos/{owner}/{repo}/issues and the new issue URL is returned.
//   - draft_url:  otherwise, a prefilled `…/issues/new?title=…&body=…` URL is
//     returned for a human to click. No token, no network — the safe default.
//
// Filing opens an issue on a public tracker, so it is outward-facing and never
// happens implicitly: both surfaces require confirm=true (CLI: --confirm) to
// leave draft mode. If the GitHub call itself fails, File degrades to a draft
// URL rather than dropping the report — the channel always yields *some* way to
// land the feedback.
package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultRepo is the upstream SDK repository feedback lands on. Override with
// BRAINAPI_FEEDBACK_REPO (owner/repo form) when working from a fork.
const DefaultRepo = "wh0amibjm/brainapi-go-sdk"

const (
	defaultAPIBase = "https://api.github.com"
	// marker is an invisible HTML comment so maintainers can filter
	// agent-filed issues with a single saved search without cluttering titles.
	marker = "<!-- brainapi-agent-feedback -->"
	// maxDraftBodyBytes caps the body baked into a draft "new issue" URL. GitHub
	// rejects request URIs past ~8 KB and percent-encoding inflates each byte up
	// to 3× (`%XX`), so a 2 KB cap keeps the worst case (all multi-byte) under
	// ~6 KB encoded. The full text still goes out unabridged on the API-post
	// path, which has no URL limit.
	maxDraftBodyBytes = 2000
)

// validCategories are the triage buckets; anything else normalizes to "bug".
var validCategories = map[string]bool{"bug": true, "docs": true, "enhancement": true, "question": true}

// Report is what the agent provides: a short title, a free-form markdown body
// (what it did, expected, and saw), the triage category, and which surface hit
// the problem.
type Report struct {
	Title    string
	Body     string
	Category string // bug | docs | enhancement | question (default bug)
	Surface  string // cli | mcp | library — the agent path that found it
}

// Env is the build/runtime context auto-appended to every issue body so the
// maintainer sees exactly which binary hit the bug. Built with RuntimeEnv.
type Env struct {
	Version string
	Commit  string
	OS      string
	Arch    string
	Go      string
}

// RuntimeEnv fills an Env from the caller-supplied build identity plus the
// current runtime. Version/commit come from the caller (internal/version) so
// this package stays free of any internal dependency.
func RuntimeEnv(version, commit string) Env {
	return Env{
		Version: version,
		Commit:  commit,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Go:      runtime.Version(),
	}
}

// Issue is the rendered GitHub issue — a pure function of (Report, Env).
type Issue struct {
	Title string
	Body  string
}

// Result is what both surfaces return to the agent.
type Result struct {
	Filed  bool   `json:"filed"`
	Mode   string `json:"mode"`             // "github_api" | "draft_url"
	URL    string `json:"url"`              // issue URL when filed, else a click-to-file draft URL
	Number int    `json:"number,omitempty"` // issue number when filed
	Note   string `json:"note"`
}

// Config resolves where feedback goes and how to authenticate. APIBase and HTTP
// are injection points for tests; both default sensibly.
type Config struct {
	Repo    string       // owner/repo
	Token   string       // GitHub token; empty => draft-only
	APIBase string       // defaults to https://api.github.com
	HTTP    *http.Client // defaults to a 15s-timeout client
}

// ConfigFromEnv reads the repo + token from the environment. Token precedence:
// BRAINAPI_FEEDBACK_TOKEN, then GITHUB_TOKEN, then GH_TOKEN.
func ConfigFromEnv() Config {
	return Config{
		Repo:  firstNonEmpty(os.Getenv("BRAINAPI_FEEDBACK_REPO"), DefaultRepo),
		Token: firstNonEmpty(os.Getenv("BRAINAPI_FEEDBACK_TOKEN"), os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN")),
	}
}

// File renders the report and either opens a GitHub issue or returns a draft
// URL. It returns an error only for caller-side problems (empty title/body); a
// GitHub-side failure degrades to a draft URL with the cause in Note, so the
// feedback is never silently lost.
func File(ctx context.Context, r Report, env Env, cfg Config, confirm bool) (Result, error) {
	if strings.TrimSpace(r.Title) == "" {
		return Result{}, errors.New("feedback: title is required")
	}
	if strings.TrimSpace(r.Body) == "" {
		return Result{}, errors.New("feedback: body is required")
	}

	cat := normalizeCategory(r.Category)
	iss := BuildIssue(r, env)

	// draft builds the click-to-file fallback. Computed lazily (only on the
	// branches that return it) so the post-success path doesn't URL-encode the
	// full issue body just to discard it.
	draft := func(note string) Result {
		return Result{Mode: "draft_url", URL: DraftURL(cfg.Repo, iss, cat), Note: note}
	}

	switch {
	case cfg.Token == "":
		return draft("no GitHub token configured (set BRAINAPI_FEEDBACK_TOKEN / GITHUB_TOKEN / GH_TOKEN); returning a draft issue URL for a human to open"), nil
	case !confirm:
		return draft("dry-run: a token is configured but confirm was not set. Re-call with confirm=true to file the issue; the URL above is a click-to-file fallback."), nil
	}

	num, htmlURL, err := postIssue(ctx, cfg, iss)
	if err != nil {
		// Intentional: a GitHub-side failure degrades to a draft URL (with the
		// cause in Note) rather than surfacing as an error and losing the report.
		return draft("GitHub API call failed (" + err.Error() + "); returning a draft issue URL instead so the report is not lost"), nil //nolint:nilerr // graceful degradation, not a swallowed error
	}
	return Result{Filed: true, Mode: "github_api", URL: htmlURL, Number: num, Note: "issue filed"}, nil
}

// BuildIssue renders the issue from the report and env. Pure and deterministic.
func BuildIssue(r Report, env Env) Issue {
	return Issue{
		Title: strings.TrimSpace(r.Title),
		Body:  renderBody(r, normalizeCategory(r.Category), env),
	}
}

// DraftURL builds a GitHub "new issue" URL with the title/body/labels
// prefilled — a click-to-file fallback that needs no token or network. The body
// is capped at maxDraftBodyChars so a long report can't produce a URL past
// GitHub's request-URI limit (the API-post path carries the full body).
func DraftURL(repo string, iss Issue, category string) string {
	body := iss.Body
	if len(body) > maxDraftBodyBytes {
		body = truncate(body, maxDraftBodyBytes) + "\n\n…[truncated for URL length — paste the full report into the issue]"
	}
	q := url.Values{}
	q.Set("title", iss.Title)
	q.Set("body", body)
	if category != "" {
		q.Set("labels", category)
	}
	return "https://github.com/" + repo + "/issues/new?" + q.Encode()
}

// renderBody composes the agent's prose with an auto-collected environment
// table and the invisible filter marker.
func renderBody(r Report, category string, env Env) string {
	var b strings.Builder
	b.WriteString(marker)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(r.Body))
	b.WriteString("\n\n---\n### Environment (auto-collected)\n\n")
	b.WriteString("| field | value |\n|---|---|\n")
	writeRow(&b, "SDK version", env.Version)
	writeRow(&b, "commit", env.Commit)
	writeRow(&b, "surface", firstNonEmpty(r.Surface, "unknown"))
	writeRow(&b, "category", category)
	writeRow(&b, "OS/arch", env.OS+"/"+env.Arch)
	writeRow(&b, "Go", env.Go)
	b.WriteString("\n> Filed via the brainapi agent-feedback channel.\n")
	return b.String()
}

func writeRow(b *strings.Builder, k, v string) {
	if v == "" {
		v = "—"
	}
	fmt.Fprintf(b, "| %s | %s |\n", k, v)
}

// postIssue opens the issue via the GitHub REST API. Labels are intentionally
// omitted from the POST: a repo missing a label would make the create call 422
// and break the whole channel, so the category travels in the body instead.
func postIssue(ctx context.Context, cfg Config, iss Issue) (int, string, error) {
	endpoint := firstNonEmpty(cfg.APIBase, defaultAPIBase) + "/repos/" + cfg.Repo + "/issues"
	payload, err := json.Marshal(map[string]string{"title": iss.Title, "body": iss.Body})
	if err != nil {
		return 0, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "brainapi-feedback")

	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, "", fmt.Errorf("github responded %d: %s", resp.StatusCode, summarize(body))
	}

	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, "", fmt.Errorf("decode github response: %w", err)
	}
	return out.Number, out.HTMLURL, nil
}

func normalizeCategory(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if validCategories[c] {
		return c
	}
	return "bug"
}

// summarize trims an error body to a single short line for the returned error.
func summarize(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		s = truncate(s, 300) + "…"
	}
	return s
}

// truncate cuts s to at most max bytes without splitting a UTF-8 rune.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
