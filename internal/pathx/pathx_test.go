package pathx

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	for _, tc := range []struct{ in, want string }{
		{"~", home},
		{"~/dl", filepath.Join(home, "dl")},
		{`~\dl`, filepath.Join(home, "dl")},
		{"~/a/b", filepath.Join(home, "a", "b")},
		// Not a leading element, so not ours to expand.
		{"~user/x", "~user/x"},
		{"~foo", "~foo"},
		{"/abs/~", "/abs/~"},
		{"", ""},
		{"relative/path", "relative/path"},
	} {
		if got := ExpandHome(tc.in); got != tc.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
