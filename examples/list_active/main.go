// list_active prints every ACTIVE alpha visible to the configured account
// as JSON lines on stdout. Demonstrates the library reuse pattern: a
// separate `main` package that imports `pkg/brainapi`.
//
// Usage:
//
//	BRAINAPI_USER=me@x.com BRAINAPI_PASS=pw go run ./examples/list_active
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
		log.Fatal("set BRAINAPI_USER and BRAINAPI_PASS to run this example")
	}

	cl, err := brainapi.NewClient(brainapi.Options{
		Email:         email,
		Password:      pass,
		Profile:       brainapi.ProfileChrome131,
		Timeout:       30 * time.Second,
		CaptchaSolver: altcha.CaptchaAdapter{Workers: runtime.NumCPU()},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if _, err := cl.Login(ctx, email, pass); err != nil {
		log.Fatal("login:", err)
	}

	out, errs := cl.ListAlphasAll(ctx, brainapi.ListAlphasOptions{
		Status: "ACTIVE",
		Order:  "-dateCreated",
		Limit:  100,
	})

	enc := json.NewEncoder(os.Stdout)
	count := 0
	for out != nil || errs != nil {
		select {
		case a, ok := <-out:
			if !ok {
				out = nil
				continue
			}
			if err := enc.Encode(a); err != nil {
				log.Fatal(err)
			}
			count++
		case e, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if e != nil {
				log.Fatal(e)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "drained %d ACTIVE alphas\n", count)
}
