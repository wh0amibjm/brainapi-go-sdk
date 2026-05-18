// submit_one submits a single alpha id (passed as the first CLI argument)
// and prints the long-polled verdict as JSON on stdout.
//
// Usage:
//
//	BRAINAPI_USER=me@x.com BRAINAPI_PASS=pw go run ./examples/submit_one qMPjAxnO
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
	if len(os.Args) < 2 {
		log.Fatal("usage: submit_one <alpha-id>")
	}
	email := os.Getenv("BRAINAPI_USER")
	pass := os.Getenv("BRAINAPI_PASS")
	if email == "" || pass == "" {
		log.Fatal("set BRAINAPI_USER and BRAINAPI_PASS to run this example")
	}

	cl, err := brainapi.NewClient(brainapi.Options{
		Email:         email,
		Password:      pass,
		Timeout:       30 * time.Second,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
		DailyBudget:   brainapi.DailyBudget{Submits: 3}, // protect against runaway loops
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if _, err := cl.Login(ctx, email, pass); err != nil {
		log.Fatal("login:", err)
	}

	v, err := cl.SubmitAlpha(ctx, os.Args[1])
	if err != nil {
		log.Fatal("submit:", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatal(err)
	}
}
