package brainapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestLogin_Normal(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/authentication" || r.Method != http.MethodPost {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		if h := r.Header.Get("authorization"); !strings.HasPrefix(h, "Basic ") {
			t.Errorf("missing Basic auth header: %q", h)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write(loadFixture(t, "auth_login_201_normal.json"))
	})
	sess, err := cl.Login(context.Background(), "user@x.com", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.User == nil || sess.User.ID != "DH52706" {
		t.Errorf("got %+v", sess)
	}
	if len(sess.Permissions) == 0 {
		t.Errorf("expected non-empty permissions")
	}
}

func TestLogin_Persona(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write(loadFixture(t, "auth_login_201_persona.json"))
	})
	_, err := cl.Login(context.Background(), "user@x.com", "pw")
	var pi *brainapi.PersonaInquiryError
	if !errors.As(err, &pi) {
		t.Fatalf("expected PersonaInquiryError, got %T %v", err, err)
	}
	if pi.Inquiry == "" {
		t.Error("inquiry id should be populated")
	}
}

func TestLogin_InvalidCreds(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write(loadFixture(t, "auth_401_invalid.json"))
	})
	_, err := cl.Login(context.Background(), "user@x.com", "pw")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := brainapi.AsAPIError(err)
	if !ok || ae.Status != 401 {
		t.Fatalf("expected APIError 401, got %v", err)
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "auth_get_secondary.json"))
	})
	info, err := cl.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.User.ID != "DT78745" {
		t.Errorf("got %+v", info)
	}
	if len(info.Permissions) != 1 || info.Permissions[0] != "TUTORIAL" {
		t.Errorf("expected [TUTORIAL], got %v", info.Permissions)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	called := false
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		called = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{}"))
	})
	if err := cl.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !called {
		t.Error("server not hit")
	}
}

func TestAutoRelogin_On401(t *testing.T) {
	t.Parallel()
	loginCount := 0
	probeCount := 0
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/authentication":
			loginCount++
			w.WriteHeader(201)
			_, _ = w.Write(loadFixture(t, "auth_login_201_normal.json"))
		case r.Method == http.MethodGet && r.URL.Path == "/authentication":
			probeCount++
			if probeCount == 1 {
				w.WriteHeader(401)
				_, _ = w.Write(loadFixture(t, "auth_401_invalid.json"))
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write(loadFixture(t, "auth_get_secondary.json"))
		}
	})
	cl.SetCredentials("user@x.com", "pw")
	info, err := cl.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info == nil || info.User.ID != "DT78745" {
		t.Fatalf("unexpected probe result: %+v", info)
	}
	if loginCount != 1 {
		t.Errorf("expected 1 re-login, got %d", loginCount)
	}
	if probeCount != 2 {
		t.Errorf("expected 2 probe attempts, got %d", probeCount)
	}
}

// With no cached credentials, a 401 on any endpoint must surface the stable
// ErrNotAuthenticated (kind: not_authenticated) rather than a generic APIError,
// so a first-time, not-logged-in caller gets a consistent "configure creds"
// signal no matter which endpoint it hits first.
func TestNoCredentials_401IsNotAuthenticated(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write(loadFixture(t, "auth_401_no_creds.json"))
	})
	// Client has no Email/Password configured (newTestServerAndClient default).
	_, err := cl.GetAlpha(context.Background(), "qMPjAxnO")
	if !errors.Is(err, brainapi.ErrNotAuthenticated) {
		t.Fatalf("expected ErrNotAuthenticated, got %T %v", err, err)
	}
	if kind, _ := brainapi.Classify(err); kind != "not_authenticated" {
		t.Errorf("Classify kind = %q, want not_authenticated", kind)
	}
}

// A request that stays 401 even AFTER a successful re-login must surface an
// error, not be masked as an empty-body success (which a caller would then
// mis-parse into a zero-value struct).
func TestAutoRelogin_StillUnauthorizedAfterRelogin(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/authentication" {
			w.WriteHeader(201)
			_, _ = w.Write(loadFixture(t, "auth_login_201_normal.json"))
			return
		}
		// The protected GET stays 401 forever — re-login doesn't help.
		w.WriteHeader(401)
		_, _ = w.Write(loadFixture(t, "auth_401_invalid.json"))
	})
	cl.SetCredentials("user@x.com", "pw")
	if _, err := cl.GetAlpha(context.Background(), "qMPjAxnO"); err == nil {
		t.Fatal("expected an error when still 401 after re-login, got nil (masked success)")
	}
}
