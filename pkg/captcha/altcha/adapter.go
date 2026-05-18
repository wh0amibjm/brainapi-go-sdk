package altcha

import (
	"context"
	"fmt"
)

// CaptchaAdapter implements the brainapi.CaptchaSolver structural interface
// using the parallel SHA-256 solver in this package.
//
// Wire it up in NewClient:
//
//	client, _ := brainapi.NewClient(brainapi.Options{
//	    CaptchaSolver: altcha.CaptchaAdapter{Workers: 8},
//	    ...
//	})
//
// Workers <= 0 picks runtime.NumCPU(); a reasonable default for production.
type CaptchaAdapter struct {
	Workers int
}

// Solve fetches a challenge via the SDK-supplied callback and runs the PoW.
// Returns the base64-encoded payload ready for auxiliary.captcha.
func (s CaptchaAdapter) Solve(ctx context.Context, fetch func(context.Context) ([]byte, error)) (string, error) {
	if fetch == nil {
		return "", fmt.Errorf("altcha: fetch callback nil")
	}
	body, err := fetch(ctx)
	if err != nil {
		return "", fmt.Errorf("altcha: fetch /captcha: %w", err)
	}
	ch, err := ParseChallenge(body)
	if err != nil {
		return "", err
	}
	sol, err := Solve(ctx, *ch, s.Workers)
	if err != nil {
		return "", err
	}
	return Encode(sol)
}
