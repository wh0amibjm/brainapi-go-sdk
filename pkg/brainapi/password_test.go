package brainapi_test

import (
	"context"
	"net/http"
	"testing"
)

func TestForgotPassword(t *testing.T) {
	t.Parallel()
	var posted bool
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/password/forgot" || r.Method != http.MethodPost {
			t.Errorf("wrong: %s %s", r.Method, r.URL.Path)
		}
		posted = true
		w.WriteHeader(200)
	})
	if err := cl.ForgotPassword(context.Background(), "x@y.com", "captcha"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if !posted {
		t.Error("server not hit")
	}
}

func TestForgotPassword_MissingEmail(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit")
	})
	if err := cl.ForgotPassword(context.Background(), "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestResetPassword(t *testing.T) {
	t.Parallel()
	var bearer string
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		bearer = r.Header.Get("authorization")
		w.WriteHeader(200)
	})
	if err := cl.ResetPassword(context.Background(), "ey.JWT", "new-pw"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if bearer != "Bearer ey.JWT" {
		t.Errorf("expected Bearer header, got %q", bearer)
	}
}

func TestResetPassword_MissingJWT(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit")
	})
	if err := cl.ResetPassword(context.Background(), "", "pw"); err == nil {
		t.Fatal("expected error")
	}
	if err := cl.ResetPassword(context.Background(), "ey.JWT", ""); err == nil {
		t.Fatal("expected error for empty password")
	}
}
