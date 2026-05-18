// live-smoke runs nine minimum-budget calls against real BRAIN to prove
// the Go SDK's TLS impersonation + protocol decoding still match what
// BRAIN actually returns today. The CI scheduled workflow runs this
// weekly so silent BRAIN-side schema upgrades (the v0.1.1/v0.1.2 class
// of bugs) get caught at fixture-refresh time instead of in production.
//
// Steps:
//
//  1. POST /authentication                (Login)        — Cloudflare/edge accept our JA3?
//  2. GET  /authentication                (Probe)        — session cookie working?
//  3. GET  /operators                     (Operators)    — bare array decode
//  4. GET  /users/self                    (Self)         — User struct still matches
//  5. GET  /users/self/competitions       (Competitions) — Competition / Leaderboard / Team
//  6. GET  /users/self/activities/...     (Activities)   — ActivityStream + RecordSetBlock
//  7. GET  /users/self/alphas             (ListAlphas)   — Page[Alpha] including team/origin
//  8. GET  /alphas/{first}                (GetAlpha)     — single Alpha detail (skipped if empty)
//  9. GET  /data-fields                   (DataFields)   — DataFieldsPage / NamedRef
//
// It does NOT submit alphas or run simulations (those consume daily
// budget and have visible side effects against the main account).
//
// Recommended account: a secondary account, NOT the main one. Live-smoke is a
// canary -- it runs weekly on CI and on ad-hoc demand, so quota/rate-limit
// pressure adds up. secondary accounts also exercise a different permission
// envelope (fewer perms, fewer data-fields), making the test slightly
// more representative of what production secondary account workers see. A
// dedicated test account is ideal; pulling one from an existing pool
// (e.g. the secondary account store status='active' rows) works too.
// NEVER point this at the main account -- one bad week of CI and you
// burn through the precious daily budgets.
//
// Usage:
//
//	$env:BRAINAPI_USER = "secondary account@example.com"
//	$env:BRAINAPI_PASS = "..."
//	go run ./scripts/live-smoke
//
// Exit codes:
//
//	0  all nine calls succeeded
//	1  any call failed (details in stderr)
//	2  missing credentials
//
// The script picks the deterministic browser profile for the supplied
// email, so a fingerprint mismatch from the production bridge will surface here.
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
	logger.Info("live-smoke starting", "email", redact(email), "profile", profile, "steps", 9)

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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 1/9 — login.
	sess, err := cl.Login(ctx, email, pass)
	if err != nil {
		fail(logger, "1/9 Login", err)
		return
	}
	if sess.User == nil {
		fail(logger, "1/9 Login", fmt.Errorf("no user record in session: %+v", sess))
		return
	}
	logger.Info("1/9 login ok", "user_id", sess.User.ID, "permissions", len(sess.Permissions))

	// 2/9 — probe.
	info, err := cl.Probe(ctx)
	if err != nil {
		fail(logger, "2/9 Probe", err)
		return
	}
	if info.User.ID == "" {
		fail(logger, "2/9 Probe", fmt.Errorf("empty user id from probe"))
		return
	}
	logger.Info("2/9 probe ok", "user_id", info.User.ID, "expiry_seconds", info.Token.Expiry)

	// 3/9 — operators.
	ops, err := cl.Operators(ctx)
	if err != nil {
		fail(logger, "3/9 Operators", err)
		return
	}
	if len(ops) == 0 {
		fail(logger, "3/9 Operators", fmt.Errorf("operator catalog is empty"))
		return
	}
	logger.Info("3/9 operators ok", "count", len(ops), "first", ops[0].Name)

	// 4/9 — self (User struct).
	user, err := cl.Self(ctx)
	if err != nil {
		fail(logger, "4/9 Self", err)
		return
	}
	if user.ID == "" {
		fail(logger, "4/9 Self", fmt.Errorf("empty user id"))
		return
	}
	logger.Info("4/9 self ok", "user_id", user.ID, "verified", user.Verified)

	// 5/9 — competitions. Exercises Competition.Team and Leaderboard.University
	// — both of which BRAIN silently upgraded from string to object on
	// 2026-05-18, breaking SDK v0.1.0. This step is the canary.
	comps, err := cl.Competitions(ctx)
	if err != nil {
		fail(logger, "5/9 Competitions", err)
		return
	}
	logger.Info("5/9 competitions ok", "count", comps.Count, "page_size", len(comps.Results))

	// 6/9 — activities (ActivityStream + RecordSetBlock heterogeneous tuples).
	activ, err := cl.Activities(ctx, brainapi.ActivitySubmissions)
	if err != nil {
		fail(logger, "6/9 Activities", err)
		return
	}
	logger.Info("6/9 activities ok", "type", string(activ.Type))

	// 7/9 — alphas list. Exercises Alpha.Team / Alpha.Color / Alpha.Category
	// (the other half of the v0.1.1 schema-drift fix).
	page, err := cl.ListAlphas(ctx, brainapi.ListAlphasOptions{Limit: 3})
	if err != nil {
		fail(logger, "7/9 ListAlphas", err)
		return
	}
	logger.Info("7/9 alphas list ok", "count", page.Count, "page_size", len(page.Results))

	// 8/9 — alpha detail. Skipped (with warning) when the account has no
	// alphas yet — common for a fresh test account, not a regression.
	if len(page.Results) > 0 {
		first := page.Results[0].ID
		a, err := cl.GetAlpha(ctx, first)
		if err != nil {
			fail(logger, "8/9 GetAlpha", err)
			return
		}
		logger.Info("8/9 alpha detail ok", "id", a.ID, "status", a.Status)
	} else {
		logger.Warn("8/9 alpha detail SKIPPED — account has zero alphas (not a failure)")
	}

	// 9/9 — data-fields (DataFieldsPage + NamedRef inside Category/Dataset/Subcategory).
	dfPage, err := cl.DataFields(ctx, brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY",
		Region:         "USA",
		Universe:       "TOP3000",
		Delay:          1,
		Limit:          3,
	})
	if err != nil {
		fail(logger, "9/9 DataFields", err)
		return
	}
	logger.Info("9/9 data-fields ok", "count", dfPage.Count, "page_size", len(dfPage.Results))

	logger.Info("ALL 9 STEPS PASSED — TLS impersonation + 9-endpoint decoding verified against real BRAIN")
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
