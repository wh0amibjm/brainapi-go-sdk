// Package altcha implements BRAIN's Altcha-style SHA-256 proof-of-work captcha.
//
// BRAIN migrated from reCAPTCHA v2 to this PoW some time before 2026-05-17.
// The full flow used by the BRAIN registration path:
//
//  1. GET /captcha -> { algorithm, challenge, salt, signature, maxNumber }
//  2. Find n in [0, maxNumber] such that hex(sha256(salt + str(n))) == challenge
//  3. POST /users with auxiliary.captcha = base64(JSON({...challenge, number, took}))
//
// On a modern CPU the linear single-threaded solve median is ~250ms for
// maxNumber=1_000_000. The parallel solver here splits the [0, maxNumber]
// range across runtime.NumCPU() goroutines and races them — typically 4-8x
// faster on multi-core hardware.
package altcha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Challenge is the body BRAIN's /captcha endpoint returns.
type Challenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
	MaxNumber int64  `json:"maxNumber"`
}

// Solution embeds the challenge and adds the found Number plus solve duration
// in milliseconds (Altcha's wire format expects "took" in ms).
type Solution struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
	MaxNumber int64  `json:"maxNumber"`
	Number    int64  `json:"number"`
	Took      int64  `json:"took"`
}

// ErrNoSolution is returned when no n in [0, MaxNumber] satisfies the challenge.
// BRAIN's server only issues challenges with a known solution in range; this
// error means the challenge has been tampered with or the algorithm field
// doesn't match what we can solve.
var ErrNoSolution = errors.New("altcha: no solution in range")

// Solve searches [0, ch.MaxNumber] for an n where hex(sha256(salt+str(n))) == challenge.
// workers <= 0 picks runtime.NumCPU().
//
// The context can cancel the search; on ctx.Done() it returns ctx.Err().
func Solve(ctx context.Context, ch Challenge, workers int) (*Solution, error) {
	if ch.Algorithm != "" && ch.Algorithm != "SHA-256" {
		return nil, fmt.Errorf("altcha: unsupported algorithm %q (only SHA-256)", ch.Algorithm)
	}
	if ch.MaxNumber < 0 {
		return nil, fmt.Errorf("altcha: invalid maxNumber %d", ch.MaxNumber)
	}
	target, err := hex.DecodeString(ch.Challenge)
	if err != nil || len(target) != sha256.Size {
		return nil, fmt.Errorf("altcha: malformed challenge hex %q", ch.Challenge)
	}

	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if int64(workers) > ch.MaxNumber+1 {
		workers = int(ch.MaxNumber + 1)
	}
	if workers < 1 {
		workers = 1
	}

	start := time.Now()
	var (
		found int64 = -1
		stop  atomic.Bool
		wg    sync.WaitGroup
	)

	// Watch the context; signal stop on cancel.
	doneCh := make(chan struct{})
	defer close(doneCh)
	go func() {
		select {
		case <-ctx.Done():
			stop.Store(true)
		case <-doneCh:
		}
	}()

	// Striped iteration: worker w handles n where n % workers == w.
	// This keeps each goroutine's slice of work contiguous in salt+digit-count
	// terms, so they all see similar SHA-256 cost per iteration.
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(start int64) {
			defer wg.Done()
			buf := make([]byte, 0, len(ch.Salt)+20)
			h := sha256.New()
			step := int64(workers)
			for n := start; n <= ch.MaxNumber; n += step {
				if stop.Load() {
					return
				}
				buf = append(buf[:0], ch.Salt...)
				buf = strconv.AppendInt(buf, n, 10)
				h.Reset()
				h.Write(buf)
				sum := h.Sum(nil)
				equal := true
				for i := 0; i < sha256.Size; i++ {
					if sum[i] != target[i] {
						equal = false
						break
					}
				}
				if equal {
					// First-writer-wins; later finds get ignored.
					if atomic.CompareAndSwapInt64(&found, -1, n) {
						stop.Store(true)
					}
					return
				}
			}
		}(int64(w))
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if found < 0 {
		return nil, ErrNoSolution
	}
	return &Solution{
		Algorithm: ch.Algorithm,
		Challenge: ch.Challenge,
		Salt:      ch.Salt,
		Signature: ch.Signature,
		MaxNumber: ch.MaxNumber,
		Number:    found,
		Took:      time.Since(start).Milliseconds(),
	}, nil
}

// ParseChallenge decodes a /captcha JSON body into a Challenge.
func ParseChallenge(body []byte) (*Challenge, error) {
	var ch Challenge
	if err := json.Unmarshal(body, &ch); err != nil {
		return nil, fmt.Errorf("altcha: parse challenge: %w", err)
	}
	if ch.Challenge == "" || ch.Salt == "" {
		return nil, errors.New("altcha: challenge body missing challenge/salt")
	}
	return &ch, nil
}
