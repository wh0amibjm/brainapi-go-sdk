package altcha_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/captcha/altcha"
)

func buildBenchChallenge(target, maxN int64) altcha.Challenge {
	salt := "bench-salt-0xdeadbeef"
	sum := sha256.Sum256([]byte(salt + strconv.FormatInt(target, 10)))
	return altcha.Challenge{
		Algorithm: "SHA-256",
		Salt:      salt,
		Challenge: hex.EncodeToString(sum[:]),
		Signature: "sig",
		MaxNumber: maxN,
	}
}

// BenchmarkSolve_1Worker is the single-threaded linear-scan baseline.
func BenchmarkSolve_1Worker(b *testing.B) {
	ch := buildBenchChallenge(50_000, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := altcha.Solve(context.Background(), ch, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSolve_AllCores measures the speedup from parallel solving.
// On a typical 8-core machine should be 4-7x faster than the 1-worker case.
func BenchmarkSolve_AllCores(b *testing.B) {
	ch := buildBenchChallenge(50_000, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := altcha.Solve(context.Background(), ch, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSolve_Production simulates BRAIN's production payload size
// (maxNumber=1_000_000) at the target the SDK is most likely to encounter.
func BenchmarkSolve_Production(b *testing.B) {
	ch := buildBenchChallenge(750_000, 1_000_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := altcha.Solve(context.Background(), ch, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}
