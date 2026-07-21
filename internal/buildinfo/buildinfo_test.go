package buildinfo

import "testing"

// TestStringDefaults verifies the dev-build defaults render as one readable line.
func TestStringDefaults(t *testing.T) {
	if got, want := String(), "dev (none, unknown)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

// TestStringInjectedValues mimics the linker overriding the vars at release time.
func TestStringInjectedValues(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })

	Version, Commit, Date = "1.2.3", "abc1234", "2026-07-01T12:00:00Z"
	if got, want := String(), "1.2.3 (abc1234, 2026-07-01T12:00:00Z)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
