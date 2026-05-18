package altcha_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

func TestCaptchaAdapter_Solve(t *testing.T) {
	t.Parallel()

	// Build a deterministic challenge that the adapter must fetch + solve.
	salt := "deadbeef"
	target := int64(99)
	sum := sha256.Sum256([]byte(salt + strconv.FormatInt(target, 10)))
	body, _ := json.Marshal(map[string]any{
		"algorithm": "SHA-256",
		"salt":      salt,
		"challenge": hex.EncodeToString(sum[:]),
		"signature": "sig",
		"maxNumber": 1000,
	})
	fetch := func(_ context.Context) ([]byte, error) { return body, nil }

	a := altcha.CaptchaAdapter{Workers: 2}
	payload, err := a.Solve(context.Background(), fetch)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var sol altcha.Solution
	if err := json.Unmarshal(raw, &sol); err != nil {
		t.Fatalf("parse solution: %v", err)
	}
	if sol.Number != target {
		t.Errorf("got number %d, want %d", sol.Number, target)
	}
}

func TestCaptchaAdapter_FetchError(t *testing.T) {
	t.Parallel()
	want := errors.New("fetch fail")
	a := altcha.CaptchaAdapter{Workers: 1}
	_, err := a.Solve(context.Background(), func(_ context.Context) ([]byte, error) { return nil, want })
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped fetch error, got %v", err)
	}
}

func TestCaptchaAdapter_NilFetch(t *testing.T) {
	t.Parallel()
	a := altcha.CaptchaAdapter{}
	if _, err := a.Solve(context.Background(), nil); err == nil {
		t.Fatal("expected nil-fetch error")
	}
}
