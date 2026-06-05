package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

var testEnv = Env{Version: "v0.5.1", Commit: "abc1234", OS: "darwin", Arch: "arm64", Go: "go1.26.3"}

func TestBuildIssueRendersBodyAndEnv(t *testing.T) {
	t.Parallel()
	iss := BuildIssue(Report{
		Title:    "  data_fields returns wrong shape  ",
		Body:     "Expected {count,results}, got a bare array.",
		Category: "bug",
		Surface:  "mcp",
	}, testEnv)

	if iss.Title != "data_fields returns wrong shape" {
		t.Errorf("title not trimmed: %q", iss.Title)
	}
	for _, want := range []string{
		marker, "Expected {count,results}", "SDK version", "v0.5.1",
		"abc1234", "mcp", "darwin/arm64", "go1.26.3", "agent-feedback channel",
	} {
		if !strings.Contains(iss.Body, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, iss.Body)
		}
	}
}

func TestNormalizeCategoryDefaultsToBug(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"bug", "bug"},
		{"DOCS", "docs"},
		{" Enhancement ", "enhancement"},
		{"question", "question"},
		{"", "bug"},
		{"garbage", "bug"},
	}
	for _, c := range cases {
		if got := normalizeCategory(c.in); got != c.want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDraftURLRoundTrips(t *testing.T) {
	t.Parallel()
	iss := BuildIssue(Report{Title: "a & b", Body: "line one\nline two", Surface: "cli"}, testEnv)
	raw := DraftURL("owner/repo", iss, "bug")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("draft URL not parseable: %v", err)
	}
	if u.Host != "github.com" || u.Path != "/owner/repo/issues/new" {
		t.Errorf("unexpected base: %s%s", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("title") != "a & b" {
		t.Errorf("title param = %q", q.Get("title"))
	}
	if !strings.Contains(q.Get("body"), "line one") || q.Get("labels") != "bug" {
		t.Errorf("body/labels params wrong: body=%q labels=%q", q.Get("body"), q.Get("labels"))
	}
}

func TestDraftURLCapsLongBody(t *testing.T) {
	t.Parallel()
	// A multi-byte rune straddling the cut point must not produce invalid UTF-8,
	// and the resulting URL must stay well under GitHub's request-URI limit.
	long := strings.Repeat("世", 5000) // 5000 runes × 3 bytes = 15000 bytes > cap
	iss := BuildIssue(Report{Title: "huge", Body: long, Surface: "cli"}, testEnv)
	raw := DraftURL("owner/repo", iss, "bug")

	if _, err := url.Parse(raw); err != nil {
		t.Fatalf("capped draft URL not parseable: %v", err)
	}
	if !utf8.ValidString(raw) {
		t.Error("draft URL is not valid UTF-8 (rune split at the cap boundary)")
	}
	if len(raw) > 8000 {
		t.Errorf("draft URL length %d exceeds the ~8KB safety budget", len(raw))
	}
	if !strings.Contains(raw, url.QueryEscape("[truncated for URL length")) {
		t.Error("capped draft URL missing the truncation notice")
	}
}

func TestSummarizeIsValidUTF8AtCut(t *testing.T) {
	t.Parallel()
	// 400 multi-byte runes; the 300-byte cut lands mid-rune unless rune-aware.
	out := summarize([]byte(strings.Repeat("世", 400)))
	if !utf8.ValidString(out) {
		t.Errorf("summarize produced invalid UTF-8: %q", out)
	}
}

func TestFileRequiresTitleAndBody(t *testing.T) {
	t.Parallel()
	if _, err := File(context.Background(), Report{Body: "x"}, testEnv, Config{}, true); err == nil {
		t.Error("missing title should error")
	}
	if _, err := File(context.Background(), Report{Title: "x"}, testEnv, Config{}, true); err == nil {
		t.Error("missing body should error")
	}
}

func TestFileNoTokenReturnsDraft(t *testing.T) {
	t.Parallel()
	res, err := File(context.Background(),
		Report{Title: "t", Body: "b", Surface: "cli"}, testEnv,
		Config{Repo: "owner/repo"}, true)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if res.Filed || res.Mode != "draft_url" {
		t.Errorf("no token should yield a draft, got %+v", res)
	}
	if !strings.Contains(res.URL, "owner/repo/issues/new") {
		t.Errorf("draft URL wrong: %s", res.URL)
	}
}

func TestFileTokenButNoConfirmIsDryRun(t *testing.T) {
	t.Parallel()
	res, err := File(context.Background(),
		Report{Title: "t", Body: "b"}, testEnv,
		Config{Repo: "owner/repo", Token: "tok"}, false)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if res.Filed || res.Mode != "draft_url" || !strings.Contains(res.Note, "dry-run") {
		t.Errorf("token without confirm should be a dry-run draft, got %+v", res)
	}
}

func TestFileFilesIssueWithTokenAndConfirm(t *testing.T) {
	t.Parallel()
	var gotAuth, gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/owner/repo/issues/42"}`))
	}))
	defer srv.Close()

	res, err := File(context.Background(),
		Report{Title: "boom", Body: "details", Surface: "mcp", Category: "bug"}, testEnv,
		Config{Repo: "owner/repo", Token: "secret", APIBase: srv.URL, HTTP: srv.Client()}, true)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if !res.Filed || res.Mode != "github_api" || res.Number != 42 {
		t.Errorf("expected filed issue #42, got %+v", res)
	}
	if res.URL != "https://github.com/owner/repo/issues/42" {
		t.Errorf("issue URL = %s", res.URL)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotPath != "/repos/owner/repo/issues" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "boom" || !strings.Contains(gotBody["body"], "details") {
		t.Errorf("posted body wrong: %+v", gotBody)
	}
}

func TestFileDegradesToDraftOnAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	res, err := File(context.Background(),
		Report{Title: "t", Body: "b", Surface: "cli"}, testEnv,
		Config{Repo: "owner/repo", Token: "secret", APIBase: srv.URL, HTTP: srv.Client()}, true)
	if err != nil {
		t.Fatalf("File should degrade, not error: %v", err)
	}
	if res.Filed || res.Mode != "draft_url" {
		t.Errorf("API failure should degrade to a draft, got %+v", res)
	}
	if !strings.Contains(res.Note, "GitHub API call failed") {
		t.Errorf("note should explain the failure: %q", res.Note)
	}
}

func TestFileDegradesOnMalformed2xxBody(t *testing.T) {
	t.Parallel()
	// A 2xx with a non-JSON body exercises the decode-failure branch, which must
	// also degrade to a draft rather than surface an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	res, err := File(context.Background(),
		Report{Title: "t", Body: "b", Surface: "cli"}, testEnv,
		Config{Repo: "owner/repo", Token: "secret", APIBase: srv.URL, HTTP: srv.Client()}, true)
	if err != nil {
		t.Fatalf("File should degrade, not error: %v", err)
	}
	if res.Filed || res.Mode != "draft_url" || !strings.Contains(res.Note, "GitHub API call failed") {
		t.Errorf("malformed 2xx body should degrade to a draft, got %+v", res)
	}
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: simulated transport failure")
}

func TestFileDegradesOnTransportError(t *testing.T) {
	t.Parallel()
	// hc.Do returning an error (DNS/conn/TLS) is the third degradation branch.
	res, err := File(context.Background(),
		Report{Title: "t", Body: "b", Surface: "cli"}, testEnv,
		Config{Repo: "owner/repo", Token: "secret", HTTP: &http.Client{Transport: errRoundTripper{}}}, true)
	if err != nil {
		t.Fatalf("File should degrade, not error: %v", err)
	}
	if res.Filed || res.Mode != "draft_url" || !strings.Contains(res.Note, "GitHub API call failed") {
		t.Errorf("transport error should degrade to a draft, got %+v", res)
	}
}
