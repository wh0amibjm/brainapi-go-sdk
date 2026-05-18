package brainapi

import "time"

// Observer is the pluggable instrumentation hook. Implementations can wire
// the SDK to Prometheus, OpenTelemetry, or any structured logging system.
//
// All methods MUST be non-blocking (or block only briefly) and goroutine-safe.
// The SDK calls them synchronously on the request hot path.
//
// Pass an implementation via Options.Observer. The default is a no-op.
type Observer interface {
	// ObserveRequest is called exactly once per finished HTTP round-trip,
	// AFTER status classification but BEFORE any retry sleep. status==0
	// means the call failed at the transport layer (no HTTP response).
	ObserveRequest(method, path string, status int, dur time.Duration, err error)

	// ObserveRetry is called when the do() loop decides to retry. attempt
	// is 0-indexed (the number of retries already attempted).
	ObserveRetry(method, path string, status, attempt int, kind RetryKind, sleep time.Duration)

	// ObserveLongPoll is called once per long-poll iteration on /submit,
	// /simulations, /check, /recordsets/pnl.
	ObserveLongPoll(method, path string, iter int, sleep time.Duration)
}

// RetryKind discriminates the reason for a retry.
type RetryKind string

const (
	RetryKindUnauthorized RetryKind = "unauthorized"
	RetryKindForbidden    RetryKind = "forbidden"
	RetryKindRateLimit    RetryKind = "rate_limit"
	RetryKindServer       RetryKind = "server_error"
	RetryKindNetwork      RetryKind = "network"
	RetryKindCooldown     RetryKind = "cooldown_set"
)

// noopObserver is the default — every method is a no-op.
type noopObserver struct{}

func (noopObserver) ObserveRequest(string, string, int, time.Duration, error) {}
func (noopObserver) ObserveRetry(string, string, int, int, RetryKind, time.Duration) {
}
func (noopObserver) ObserveLongPoll(string, string, int, time.Duration) {}

// DefaultObserver returns the no-op observer.
func DefaultObserver() Observer { return noopObserver{} }
