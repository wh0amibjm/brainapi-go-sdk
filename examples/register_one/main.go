// register_one shows the minimal embedding wiring needed to call
// Client.Register: build a RegisterInput with the corrected v0.2.0
// field names (graduationYear, no address.zip), plug in the Altcha
// solver, and POST /users.
//
// Captcha is auto-fetched + solved when auxiliary.captcha is empty.
//
// Usage:
//
//	BRAINAPI_REG_EMAIL=new@example.com BRAINAPI_REG_PASS=Strong! go run ./examples/register_one
//
// Side effect: creates a real BRAIN account. The companion live-test
// runner (scripts/register) auto-generates the email for you and
// also runs the post-register login/probe/self verification leg.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

func main() {
	email := os.Getenv("BRAINAPI_REG_EMAIL")
	pass := os.Getenv("BRAINAPI_REG_PASS")
	if email == "" || pass == "" {
		log.Fatal("set BRAINAPI_REG_EMAIL (preferably under @example.com) and BRAINAPI_REG_PASS")
	}

	cl, err := brainapi.NewClient(brainapi.Options{
		Profile:       brainapi.ProfileForEmail(email),
		Timeout:       30 * time.Second,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
	})
	if err != nil {
		log.Fatal(err)
	}

	in := brainapi.RegisterInput{
		Email:     email,
		FirstName: "Test",
		LastName:  "Account",
		FullName:  "Test Account",
		Gender:    "MALE",
		Address:   brainapi.Address{Country: "US"},
		Education: brainapi.Education{
			University:     "MIT",
			Major:          "CS",
			Degree:         "BACHELORS",
			GraduationYear: 2020,
		},
		Auxiliary: brainapi.Auxiliary{Password: pass},
	}

	// Per-request timeout comes from Options.Timeout; no need to wrap
	// ctx with a deadline that would conflict with log.Fatal's exit.
	u, err := cl.Register(context.Background(), in)
	if err != nil {
		log.Fatal("register:", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	// 201 body is opaque upstream; u may be nil. Print whatever we got
	// so the caller can confirm the call succeeded.
	if err := enc.Encode(map[string]any{
		"registered": true,
		"user":       u,
	}); err != nil {
		log.Fatal(err)
	}
}
