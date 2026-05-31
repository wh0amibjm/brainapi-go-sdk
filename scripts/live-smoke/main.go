// live-smoke runs fourteen minimum-budget calls against real BRAIN to
// prove the Go SDK's TLS impersonation + protocol decoding still match
// what BRAIN actually returns today. The CI scheduled workflow runs this
// weekly so silent BRAIN-side schema upgrades (the v0.1.1/v0.1.2 class
// of bugs) get caught at fixture-refresh time instead of in production.
//
// Steps:
//
//  1. POST /authentication                       (Login)                 — Cloudflare/edge accept our JA3?
//  2. GET  /authentication                       (Probe)                 — session cookie working?
//  3. GET  /operators                            (Operators)             — bare array decode
//  4. GET  /users/self                           (Self)                  — User struct still matches
//  5. GET  /users/self/competitions              (Competitions)          — Competition / Leaderboard / Team
//  6. GET  /users/self/activities/...            (Activities)            — ActivityStream + RecordSetBlock
//  7. GET  /users/self/alphas                    (ListAlphas)            — Page[Alpha] including team/origin
//  8. GET  /alphas/{first}                       (GetAlpha)              — single Alpha detail (skipped if empty)
//  9. GET  /alphas/{first}/check                 (CheckAlpha)            — IsBlock decode (skipped if empty)
//  10. GET  /alphas/{first}/recordsets/pnl        (AlphaPnL)              — PnLSeries decode (skipped if empty)
//  11. GET  /alphas/{first}/correlations/self     (AlphaSelfCorrelation)  — SelfCorrelationBlock — pre-submit gate (skipped if empty)
//  12. GET  /data-fields                          (DataFields)            — DataFieldsPage / NamedRef
//  13. GET  /users/self/messages                  (Messages)              — Page[Message] notification feed (type/tags/read)
//  14. POST /authentication/logout                (Logout)                — session teardown
//
// It does NOT submit alphas or run simulations (those consume daily
// budget and have visible side effects against the main account).
//
// Recommended account: a dedicated test account, NOT the main one.
// Live-smoke is a canary -- it runs weekly on CI and on ad-hoc demand,
// so quota/rate-limit pressure adds up. A separate account also exercises
// a different permission envelope (fewer perms, fewer data-fields),
// making the test slightly more representative.
// NEVER point this at the main account -- one bad week of CI and you
// burn through the precious daily budgets.
//
// Usage:
//
//	$env:BRAINAPI_USER = "test-account@example.com"
//	$env:BRAINAPI_PASS = "..."
//	go run ./scripts/live-smoke
//
// Exit codes:
//
//	0  all fourteen calls succeeded (steps 8-11 may legitimately skip on empty accounts)
//	1  any call failed (details in stderr)
//	2  missing credentials
//
// The script picks the deterministic browser profile for the supplied
// email, so a profile/fingerprint regression will surface here.
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
	"github.com/wh0amibjm/brainapi-go-sdk/scripts/internal/uxlog"
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
	logger.Info("live-smoke starting", "email", uxlog.Redact(email), "profile", profile, "steps", 14)

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

	// 300s budget: typical run is well under 60s but adding the long-poll
	// endpoints (CheckAlpha up to 30 polls, AlphaPnL up to 6, /correlations/self
	// server-cached but cold on a fresh test account) raises the cold-cache
	// worst case past the old 180s ceiling.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 1/14 — login.
	sess, err := cl.Login(ctx, email, pass)
	if err != nil {
		fail(logger, "1/14 Login", err)
		return
	}
	if sess.User == nil {
		fail(logger, "1/14 Login", fmt.Errorf("no user record in session: %+v", sess))
		return
	}
	logger.Info("1/14 login ok", "user_id", sess.User.ID, "permissions", len(sess.Permissions))

	// 2/14 — probe.
	info, err := cl.Probe(ctx)
	if err != nil {
		fail(logger, "2/14 Probe", err)
		return
	}
	if info.User.ID == "" {
		fail(logger, "2/14 Probe", fmt.Errorf("empty user id from probe"))
		return
	}
	logger.Info("2/14 probe ok", "user_id", info.User.ID, "expiry_seconds", info.Token.Expiry)

	// 3/14 — operators.
	ops, err := cl.Operators(ctx)
	if err != nil {
		fail(logger, "3/14 Operators", err)
		return
	}
	if len(ops) == 0 {
		fail(logger, "3/14 Operators", fmt.Errorf("operator catalog is empty"))
		return
	}
	logger.Info("3/14 operators ok", "count", len(ops), "first", ops[0].Name)

	// 4/14 — self (User struct).
	user, err := cl.Self(ctx)
	if err != nil {
		fail(logger, "4/14 Self", err)
		return
	}
	if user.ID == "" {
		fail(logger, "4/14 Self", fmt.Errorf("empty user id"))
		return
	}
	logger.Info("4/14 self ok", "user_id", user.ID, "verified", user.Verified)

	// 5/14 — competitions. Exercises Competition.Team and Leaderboard.University
	// — both of which BRAIN silently upgraded from string to object on
	// 2026-05-18, breaking SDK v0.1.0. This step is the canary.
	comps, err := cl.Competitions(ctx)
	if err != nil {
		fail(logger, "5/14 Competitions", err)
		return
	}
	logger.Info("5/14 competitions ok", "count", comps.Count, "page_size", len(comps.Results))

	// 6/14 — activities (ActivityStream + RecordSetBlock heterogeneous tuples).
	activ, err := cl.Activities(ctx, brainapi.ActivitySubmissions)
	if err != nil {
		fail(logger, "6/14 Activities", err)
		return
	}
	logger.Info("6/14 activities ok", "type", string(activ.Type))

	// 7/14 — alphas list. Exercises Alpha.Team / Alpha.Color / Alpha.Category
	// (the other half of the v0.1.1 schema-drift fix).
	page, err := cl.ListAlphas(ctx, brainapi.ListAlphasOptions{Limit: 3})
	if err != nil {
		fail(logger, "7/14 ListAlphas", err)
		return
	}
	logger.Info("7/14 alphas list ok", "count", page.Count, "page_size", len(page.Results))

	// 8-11/14 — per-alpha detail probes. Skipped (with warning) when the
	// account has no alphas yet — common for a fresh test account, not a
	// regression. All four reuse the same first alpha so we exercise the
	// full read-side decoding surface (Alpha / IsBlock / PnLSeries /
	// SelfCorrelationBlock) with one budget-free fan-out.
	if len(page.Results) > 0 {
		first := page.Results[0].ID

		// 8/14 — GetAlpha (single Alpha detail).
		a, err := cl.GetAlpha(ctx, first)
		if err != nil {
			fail(logger, "8/14 GetAlpha", err)
			return
		}
		logger.Info("8/14 alpha detail ok", "id", a.ID, "status", a.Status)

		// 9/14 — CheckAlpha (IsBlock decode — pre-submit deterministic gates).
		ib, err := cl.CheckAlpha(ctx, first)
		if err != nil {
			fail(logger, "9/14 CheckAlpha", err)
			return
		}
		logger.Info("9/14 check ok", "id", first, "checks", len(ib.Checks))

		// 10/14 — AlphaPnL (PnLSeries decode).
		pnl, err := cl.AlphaPnL(ctx, first)
		if err != nil {
			fail(logger, "10/14 AlphaPnL", err)
			return
		}
		logger.Info("10/14 pnl ok", "id", first, "rows", len(pnl.Records))

		// 11/14 — AlphaSelfCorrelation. The pre-submit gate referenced by
		// the SubmitAlpha workflow; schema drift here would silently
		// invalidate budget-spending decisions. Cached server-side so
		// returns immediately on warm alphas.
		sc, err := cl.AlphaSelfCorrelation(ctx, first)
		if err != nil {
			fail(logger, "11/14 AlphaSelfCorrelation", err)
			return
		}
		logger.Info("11/14 self-correlation ok", "id", first, "max", derefFloat(sc.Max), "min", derefFloat(sc.Min))
	} else {
		logger.Warn("8-11/14 alpha-detail steps SKIPPED — account has zero alphas (not a failure)")
	}

	// 12/14 — data-fields (DataFieldsPage + NamedRef inside Category/Dataset/Subcategory).
	dfPage, err := cl.DataFields(ctx, brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY",
		Region:         "USA",
		Universe:       "TOP3000",
		Delay:          1,
		Limit:          3,
	})
	if err != nil {
		fail(logger, "12/14 DataFields", err)
		return
	}
	logger.Info("12/14 data-fields ok", "count", dfPage.Count, "page_size", len(dfPage.Results))

	// 13/14 — messages (notification feed). Exercises Page[Message] decode and
	// the Message struct (type/tags/read), where new-dataset announcements
	// surface. No type filter so the mixed ANNOUNCEMENT/NOTIFICATION stream is
	// decoded. Empty on a fresh account — like alphas, absence is not a failure.
	msgs, err := cl.Messages(ctx, brainapi.ListMessagesOptions{Limit: 3, Order: "-dateCreated"})
	if err != nil {
		fail(logger, "13/14 Messages", err)
		return
	}
	logger.Info("13/14 messages ok", "count", msgs.Count, "page_size", len(msgs.Results))

	// 14/14 — logout. MUST be last; invalidates the session cookie for any
	// subsequent calls. Verifies session-teardown path didn't regress.
	if err := cl.Logout(ctx); err != nil {
		fail(logger, "14/14 Logout", err)
		return
	}
	logger.Info("14/14 logout ok")

	logger.Info("ALL 14 STEPS PASSED — TLS impersonation + 14-endpoint decoding verified against real BRAIN")
}

func fail(logger *slog.Logger, step string, err error) {
	logger.Error("live-smoke FAILED", "step", step, "err", err.Error())
	os.Exit(1)
}

// derefFloat returns the underlying value of a *float64, or the literal
// string "nil" when the pointer is nil, so slog renders something readable
// instead of the pointer address.
func derefFloat(p *float64) any {
	if p == nil {
		return "nil"
	}
	return *p
}
