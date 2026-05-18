// Package version holds build-time version metadata injected via -ldflags.
package version

// Build metadata. Populated by the Makefile's -X flags; "dev"/"unknown" when
// building outside the Makefile (e.g. `go run`).
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns "vVERSION (commit COMMIT, built DATE)".
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
