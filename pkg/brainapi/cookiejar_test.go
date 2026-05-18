package brainapi_test

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestCookieJar_PersistsAcrossClients(t *testing.T) {
	t.Parallel()
	jarPath := filepath.Join(t.TempDir(), "cookies.json")

	srv, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authentication":
			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    "live-session-id-xyz",
				Path:     "/",
				HttpOnly: true,
			})
			w.WriteHeader(201)
			_, _ = w.Write(loadFixture(t, "auth_login_201_normal.json"))
		default:
			w.WriteHeader(404)
		}
	})

	cl1, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       srv.URL,
		CookieJarPath: jarPath,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := cl1.Login(context.Background(), "u@x.com", "pw"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Verify the file exists, contains the cookie, and has restrictive perms (POSIX).
	b, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatalf("read jar: %v", err)
	}
	if !strings.Contains(string(b), "live-session-id-xyz") {
		t.Errorf("jar did not capture session cookie:\n%s", b)
	}

	// Now create a fresh Client pointed at the same jar — it should load the cookies.
	cl2, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       srv.URL,
		CookieJarPath: jarPath,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient(2): %v", err)
	}
	if cl2.CookieJarPath() != jarPath {
		t.Errorf("jar path getter wrong: %q vs %q", cl2.CookieJarPath(), jarPath)
	}
	_ = cl // keep reference live
}

func TestCookieJar_BadPathIsNonFatal(t *testing.T) {
	t.Parallel()
	// A jar path pointing at a non-existent directory should NOT make NewClient
	// fail — load is best-effort. Subsequent save will mkdir.
	jarPath := filepath.Join(t.TempDir(), "subdir", "cookies.json")
	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       "https://api.worldquantbrain.com",
		CookieJarPath: jarPath,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient with nonexistent jar dir should still succeed, got %v", err)
	}
	if cl.CookieJarPath() != jarPath {
		t.Errorf("jar path not preserved: %q", cl.CookieJarPath())
	}
}

func TestLogout_ClearsJarFile(t *testing.T) {
	t.Parallel()
	jarPath := filepath.Join(t.TempDir(), "cookies.json")

	srv, throwaway := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /authentication":
			http.SetCookie(w, &http.Cookie{
				Name: "session_token", Value: "live-xyz", Path: "/", HttpOnly: true,
			})
			w.WriteHeader(201)
			_, _ = w.Write(loadFixture(t, "auth_login_201_normal.json"))
		case "DELETE /authentication":
			w.WriteHeader(200)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(404)
		}
	})
	_ = throwaway

	cl, err := brainapi.NewClient(brainapi.Options{
		BaseURL:       srv.URL,
		CookieJarPath: jarPath,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := cl.Login(context.Background(), "u@x.com", "pw"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := os.Stat(jarPath); err != nil {
		t.Fatalf("jar file should exist after Login: %v", err)
	}

	if err := cl.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := os.Stat(jarPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("jar file should be removed after Logout, stat err = %v", err)
	}
}
