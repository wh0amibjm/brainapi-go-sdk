// CLI integration tests. Builds the binary into a temp dir, then exec's it
// against a real httptest.Server replaying captured BRAIN responses. Asserts
// the {ok, data}/{ok, error} JSON envelope and the documented exit codes.
//
// These tests are gated on `go test -short` (they skip in short mode because
// they invoke `go build`, which is the slow path).
package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "brainapi-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Env = os.Environ()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func runCLI(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	stdoutBuf, _ := cmd.StdoutPipe()
	stderrBuf, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	stdoutAll := readAll(stdoutBuf)
	stderrAll := readAll(stderrBuf)
	err := cmd.Wait()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err == nil:
		code = 0
	default:
		t.Fatalf("wait: %v", err)
	}
	return stdoutAll, stderrAll, code
}

func readAll(r interface{ Read([]byte) (int, error) }) string {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			return string(out)
		}
	}
}

func loadFixtureCLI(t *testing.T, name string) []byte {
	t.Helper()
	// Walk up from cwd to find repo root (go.mod).
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found")
		}
		dir = parent
	}
	b, err := os.ReadFile(filepath.Join(dir, "testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestCLI_SchemaOperators(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode (go build is slow)")
	}
	bin := buildBinary(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operators" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixtureCLI(t, "operators.json"))
	}))
	defer srv.Close()

	stdout, _, code := runCLI(t, bin, []string{"BRAINAPI_BASE_URL=" + srv.URL}, "schema", "operators")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, stdout)
	}
	var env struct {
		OK   bool             `json:"ok"`
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("parse envelope: %v\nstdout=%s", err, stdout)
	}
	if !env.OK || len(env.Data) != 2 {
		t.Errorf("unexpected envelope: %+v", env)
	}
}

func TestCLI_ErrorExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode")
	}
	bin := buildBinary(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write(loadFixtureCLI(t, "drf_validation_400.json"))
	}))
	defer srv.Close()

	stdout, _, code := runCLI(t, bin,
		[]string{"BRAINAPI_BASE_URL=" + srv.URL},
		"email", "reverify", "--email", "x@y.com", "--recaptcha", "stub")
	if code != exitDRF {
		t.Errorf("expected exit %d, got %d", exitDRF, code)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error *struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("parse envelope: %v\nstdout=%s", err, stdout)
	}
	if env.OK || env.Error == nil || env.Error.Kind != "drf_validation" {
		t.Errorf("expected drf_validation envelope: %+v", env)
	}
}

// TestCLI_FeedbackUsageExitCode: a present-but-empty --title is a caller-side
// usage error, so it must exit 2 (USAGE) with no JSON on stdout — not the
// server-side exit 6 the `error`/api bucket reserves.
func TestCLI_FeedbackUsageExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode")
	}
	bin := buildBinary(t)
	stdout, _, code := runCLI(t, bin, nil, "feedback", "--title", "", "--body", "x")
	if code != exitUsage {
		t.Errorf("expected exit %d, got %d (stdout=%q)", exitUsage, code, stdout)
	}
	if stdout != "" {
		t.Errorf("usage error must not emit a JSON envelope on stdout, got %q", stdout)
	}
}

// TestCLI_FeedbackDraftEnvelope: without --confirm the command never hits the
// network (dry-run), so it deterministically returns a draft envelope — and that
// envelope omits `number` (it's present only on a filed result).
func TestCLI_FeedbackDraftEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode")
	}
	bin := buildBinary(t)
	stdout, _, code := runCLI(t, bin, nil, "feedback", "--title", "t", "--body", "b")
	if code != 0 {
		t.Fatalf("exit=%d, stdout=%s", code, stdout)
	}
	var env struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	if !env.OK || env.Data["mode"] != "draft_url" {
		t.Errorf("expected a draft envelope, got %+v", env)
	}
	if _, ok := env.Data["number"]; ok {
		t.Errorf("draft envelope must not carry a `number` key, got %+v", env.Data)
	}
}

func TestCLI_VersionAlwaysOK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode")
	}
	bin := buildBinary(t)
	stdout, _, code := runCLI(t, bin, nil, "version")
	if code != 0 {
		t.Errorf("exit=%d, stdout=%s", code, stdout)
	}
	var env struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	if !env.OK || env.Data["version"] == nil {
		t.Errorf("unexpected: %+v", env)
	}
}

func TestResolveMaxLongPolls(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, defaultMaxLongPolls},  // unset → default
		{-5, defaultMaxLongPolls}, // negative → default
		{60, 60},                  // explicit default
		{360, 360},                // raised for slow multi-sims
		{1, 1},                    // any positive value is honored
	}
	for _, c := range cases {
		if got := resolveMaxLongPolls(c.in); got != c.want {
			t.Errorf("resolveMaxLongPolls(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestRootCmd_MaxLongPollsFlag(t *testing.T) {
	f := newRootCmd().PersistentFlags().Lookup("max-long-polls")
	if f == nil {
		t.Fatal("--max-long-polls persistent flag is not registered")
	}
	if f.DefValue != "60" {
		t.Errorf("--max-long-polls default = %q, want \"60\"", f.DefValue)
	}
}
