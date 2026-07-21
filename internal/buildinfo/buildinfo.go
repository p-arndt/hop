// Package buildinfo exposes the binary's version metadata.
//
// The values default to a "dev" build and are overridden at release time via
// the Go linker, e.g.
//
//	go build -ldflags "-X hop/internal/buildinfo.Version=1.2.3 \
//	                   -X hop/internal/buildinfo.Commit=abc1234 \
//	                   -X hop/internal/buildinfo.Date=2026-07-01T12:00:00Z"
//
// The release pipeline reads the version from the repo-root VERSION file (the
// single source of truth) and injects it here. See .github/workflows/release.yml.
package buildinfo

var (
	// Version is the semantic version, without a leading "v" (e.g. "1.2.3").
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
	// Date is the RFC 3339 UTC build timestamp.
	Date = "unknown"
)

// String renders the full version line, e.g. "1.2.3 (abc1234, 2026-07-01T…)".
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
