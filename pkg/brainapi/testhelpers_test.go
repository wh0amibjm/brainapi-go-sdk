package brainapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// loadFixture returns the bytes of testdata/<name>. testdata lives at the
// repo root; tests resolve via the well-known parent-walking trick.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// repoRoot walks upward from the test file's CWD until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repoRoot: no go.mod found upward from cwd")
		}
		dir = parent
	}
}

// newTestServerAndClient spins up an httptest.Server with the given handler
// and returns a Client pointed at it. The Client is configured with a
// 5-second timeout, fast retry backoffs, and no proxy.
func newTestServerAndClient(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *brainapi.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:           srv.URL,
		Timeout:           5 * time.Second,
		MaxRetries:        1,
		MaxLongPolls:      4,
		MaxConcurrentSims: 1,
		BanThreshold:      3,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return srv, cl
}

// newTestServerAndClientFromMux is the multi-route flavor of the helper.
// Callers register handlers on a mux and we route them through it.
func newTestServerAndClientFromMux(t *testing.T, mux *http.ServeMux) (*httptest.Server, *brainapi.Client) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:    srv.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return srv, cl
}

// drainBodyReq reads a request body in full; helper for handlers that need
// to assert on the posted bytes.
func drainBodyReq(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read req body: %v", err)
	}
	return b
}
