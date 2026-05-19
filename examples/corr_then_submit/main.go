// corr_then_submit shows the pre-submit correlation gate introduced
// in v0.2.0: call AlphaSelfCorrelation first, refuse to submit when
// the maximum correlation against already-submitted peers is >= 0.7,
// otherwise call SubmitAlpha. The threshold matches BRAIN's own
// post-submit SELF_CORRELATION check, but the gate is free of daily
// submit-budget cost.
//
// Usage:
//
//	BRAINAPI_USER=me@x.com BRAINAPI_PASS=pw go run ./examples/corr_then_submit qMPjAxnO
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

const corrThreshold = 0.7

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: corr_then_submit <alpha-id>")
	}
	email := os.Getenv("BRAINAPI_USER")
	pass := os.Getenv("BRAINAPI_PASS")
	if email == "" || pass == "" {
		log.Fatal("set BRAINAPI_USER and BRAINAPI_PASS")
	}

	cl, err := brainapi.NewClient(brainapi.Options{
		Email:       email,
		Password:    pass,
		Timeout:     30 * time.Second,
		DailyBudget: brainapi.DailyBudget{Submits: 3}, // belt-and-braces
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if _, err := cl.Login(ctx, email, pass); err != nil {
		log.Fatal("login:", err)
	}

	alphaID := os.Args[1]

	// 1) Pre-submit gate. The SDK long-polls past BRAIN's "still
	//    computing" signal (either 503+Retry-After or 200+empty body
	//    on TUTORIAL tier) until it gets the terminal {min,max,records}.
	block, err := cl.AlphaSelfCorrelation(ctx, alphaID)
	if err != nil {
		log.Fatal("corr:", err)
	}

	// 2) Decide. Fresh accounts with no submitted peers come back with
	//    Max == nil — interpret that as "0 peers, safe to submit".
	if block.Max != nil && *block.Max >= corrThreshold {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"submitted": false,
			"reason":    fmt.Sprintf("max correlation %.3f >= threshold %.2f", *block.Max, corrThreshold),
			"corr":      block,
		})
		return
	}

	// 3) Submit. SubmitAlpha posts then long-polls the verdict.
	v, err := cl.SubmitAlpha(ctx, alphaID)
	if err != nil {
		log.Fatal("submit:", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"submitted": true,
		"verdict":   v,
		"corr_max":  block.Max,
	})
}
