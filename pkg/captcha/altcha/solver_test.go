package altcha_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

// buildChallenge constructs a deterministic challenge whose solution is
// the given target number. Useful for fast, hermetic tests.
func buildChallenge(t *testing.T, salt string, target, maxNumber int64) altcha.Challenge {
	t.Helper()
	sum := sha256.Sum256([]byte(salt + strconv.FormatInt(target, 10)))
	return altcha.Challenge{
		Algorithm: "SHA-256",
		Salt:      salt,
		Challenge: hex.EncodeToString(sum[:]),
		Signature: "test-stub",
		MaxNumber: maxNumber,
	}
}

func TestSolve_FindsTarget(t *testing.T) {
	t.Parallel()
	ch := buildChallenge(t, "deadbeef", 4242, 10_000)
	got, err := altcha.Solve(context.Background(), ch, 4)
	if err != nil {
		t.Fatalf("Solve returned error: %v", err)
	}
	if got.Number != 4242 {
		t.Fatalf("Solve found %d, want 4242", got.Number)
	}
	if got.Took < 0 {
		t.Fatalf("Solve took negative %d", got.Took)
	}
	if got.Algorithm != ch.Algorithm || got.Salt != ch.Salt || got.Challenge != ch.Challenge {
		t.Fatalf("Solve dropped challenge fields: %+v", got)
	}
}

func TestSolve_SingleWorker(t *testing.T) {
	t.Parallel()
	// workers=1 → linear scan; mirrors the TS implementation's behavior.
	ch := buildChallenge(t, "salt-xyz", 137, 1000)
	got, err := altcha.Solve(context.Background(), ch, 1)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got.Number != 137 {
		t.Fatalf("Solve found %d, want 137", got.Number)
	}
}

func TestSolve_NoSolution(t *testing.T) {
	t.Parallel()
	ch := altcha.Challenge{
		Algorithm: "SHA-256",
		Salt:      "salt",
		Challenge: hex.EncodeToString(make([]byte, sha256.Size)), // all-zero digest, never matches
		Signature: "sig",
		MaxNumber: 100,
	}
	_, err := altcha.Solve(context.Background(), ch, 2)
	if err == nil {
		t.Fatal("expected ErrNoSolution, got nil")
	}
}

func TestSolve_UnsupportedAlgorithm(t *testing.T) {
	t.Parallel()
	ch := altcha.Challenge{Algorithm: "MD5", Salt: "s", Challenge: "ab", MaxNumber: 1}
	_, err := altcha.Solve(context.Background(), ch, 1)
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestSolve_MalformedChallengeHex(t *testing.T) {
	t.Parallel()
	ch := altcha.Challenge{Algorithm: "SHA-256", Salt: "s", Challenge: "not-hex", MaxNumber: 1}
	_, err := altcha.Solve(context.Background(), ch, 1)
	if err == nil {
		t.Fatal("expected error for malformed challenge hex")
	}
}

func TestSolve_CancelContext(t *testing.T) {
	t.Parallel()
	// Challenge with all-zero target ensures the search runs the full range.
	ch := altcha.Challenge{
		Algorithm: "SHA-256",
		Salt:      "s",
		Challenge: hex.EncodeToString(make([]byte, sha256.Size)),
		Signature: "sig",
		MaxNumber: 1_000_000,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so the watcher signals stop immediately.
	_, err := altcha.Solve(ctx, ch, 2)
	if err == nil {
		t.Fatal("expected context error after cancel")
	}
}

func TestEncode_RoundTrip(t *testing.T) {
	t.Parallel()
	sol := &altcha.Solution{
		Algorithm: "SHA-256",
		Challenge: "abc",
		Salt:      "def",
		Signature: "sig",
		MaxNumber: 1000,
		Number:    42,
		Took:      123,
	}
	enc, err := altcha.Encode(sol)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	var got altcha.Solution
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if got != *sol {
		t.Fatalf("round-trip mismatch:\ngot:  %+v\nwant: %+v", got, *sol)
	}
}

func TestParseChallenge(t *testing.T) {
	t.Parallel()
	body := []byte(`{"algorithm":"SHA-256","challenge":"ff","salt":"s","signature":"sig","maxNumber":100}`)
	ch, err := altcha.ParseChallenge(body)
	if err != nil {
		t.Fatalf("ParseChallenge: %v", err)
	}
	if ch.Challenge != "ff" || ch.Salt != "s" || ch.MaxNumber != 100 {
		t.Fatalf("ParseChallenge wrong: %+v", ch)
	}
}

func TestParseChallenge_Missing(t *testing.T) {
	t.Parallel()
	_, err := altcha.ParseChallenge([]byte(`{"algorithm":"SHA-256"}`))
	if err == nil {
		t.Fatal("expected error for missing challenge/salt")
	}
}
