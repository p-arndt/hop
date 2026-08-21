// Package buildinfo exposes the binary's version metadata, injected at release time via -ldflags.
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
