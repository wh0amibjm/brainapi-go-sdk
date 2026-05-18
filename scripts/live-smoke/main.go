// live-smoke runs three minimum-budget calls against real BRAIN to prove
// the Go SDK's TLS impersonation + protocol decoding actually work end-to-end:
//
//  1. POST /authentication (Basic auth) — does Cloudflare/edge accept our JA3?
//  2. GET  /authentication (probe)      — did we get a session cookie?
//  3. GET  /operators                   — does a real BRAIN GET decode?
//
// It does NOT submit alphas or run simulations (those consume daily budget
// against the main account). It does NOT exercise the persona or registration
// flows.
//
// Usage:
//
//	$env:BRAINAPI_USER = "me@example.com"
//	$env:BRAINAPI_PASS = "..."
//	go run ./scripts/live-smoke
//
// Exit codes:
//
//	0  all three calls succeeded
//	1  any call failed (details in stderr)
//	2  missing credentials
//
// The script picks the deterministic browser profile for the supplied email
// (matches what production secondary accounts use), so a fingerprint mismatch from
// the production bridge will surface here.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

func main() {
	email := os.Getenv("BRAINAPI_USER")
	pass := os.Getenv("BRAINAPI_PASS")
	if email == "" || pass == "" {
		fmt.Fprintln(os.Stderr, "live-smoke: set BRAINAPI_USER and BRAINAPI_PASS")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	profile := brainapi.ProfileForEmail(email)
	logger.Info("live-smoke starting", "email", redact(email), "profile", profile)

	cl, err := brainapi.NewClient(brainapi.Options{
		Profile:       profile,
		Timeout:       30 * time.Second,
		MaxRetries:    2,
		Logger:        logger,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
		Email:         email,
		Password:      pass,
	})
	if err != nil {
		fail(logger, "NewClient", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1 — login.
	sess, err := cl.Login(ctx, email, pass)
	if err != nil {
		fail(logger, "Login", err)
		return
	}
	if sess.User == nil {
		fail(logger, "Login", fmt.Errorf("no user record in session: %+v", sess))
		return
	}
	logger.Info(
		"login ok",
		"user_id", sess.User.ID,
		"permissions", sess.Permissions,
	)

	// Step 2 — probe.
	info, err := cl.Probe(ctx)
	if err != nil {
		fail(logger, "Probe", err)
		return
	}
	if info.User.ID == "" {
		fail(logger, "Probe", fmt.Errorf("empty user id from probe"))
		return
	}
	logger.Info(
		"probe ok",
		"user_id", info.User.ID,
		"expiry_seconds", info.Token.Expiry,
		"permissions", info.Permissions,
	)

	// Step 3 — operators.
	ops, err := cl.Operators(ctx)
	if err != nil {
		fail(logger, "Operators", err)
		return
	}
	if len(ops) == 0 {
		fail(logger, "Operators", fmt.Errorf("operator catalog is empty"))
		return
	}
	logger.Info(
		"operators ok",
		"count", len(ops),
		"first", ops[0].Name,
	)

	logger.Info("ALL THREE CALLS SUCCEEDED — TLS impersonation + decoding verified against real BRAIN")
}

func fail(logger *slog.Logger, step string, err error) {
	logger.Error("live-smoke FAILED", "step", step, "err", err.Error())
	os.Exit(1)
}

// redact keeps just the @domain part of an email for log breadcrumbs.
func redact(email string) string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return "***" + email[i:]
		}
	}
	return "***"
}
