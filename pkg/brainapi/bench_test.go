package brainapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// BenchmarkClient_Self measures the SDK's per-call overhead end-to-end
// against an httptest server (no real network — just SDK + loopback TCP).
func BenchmarkClient_Self(b *testing.B) {
	body := []byte(`{"id":"DT78745","email":"x@y.com","verified":true,"approved":true}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, MaxRetries: 0})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cl.Self(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClient_AlphaGet exercises the larger-body decode path so we can
// track regressions in the typed Alpha unmarshal cost.
func BenchmarkClient_AlphaGet(b *testing.B) {
	body := benchFixture(b, "alpha_detail.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: srv.URL, MaxRetries: 0})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cl.GetAlpha(ctx, "qMPjAxnO"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProfileForEmail measures the deterministic per-email profile
// hash. Hot on any code path that constructs a per-account Client.
func BenchmarkProfileForEmail(b *testing.B) {
	emails := []string{"alice@x.com", "bob@y.com", "carol@z.com"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = brainapi.ProfileForEmail(emails[i%len(emails)])
	}
}

func benchFixture(b *testing.B, name string) []byte {
	b.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			b.Fatal("no go.mod found")
		}
		dir = parent
	}
	out, err := os.ReadFile(filepath.Join(dir, "testdata", name))
	if err != nil {
		b.Fatal(err)
	}
	return out
}
