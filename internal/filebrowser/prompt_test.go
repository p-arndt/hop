package filebrowser

import (
	"strings"
	"testing"
)

// A question's label carries remote filenames — the delete confirm, the two overwrite
// confirms, the copy collision — so a name full of escape sequences must not reach the
// terminal. The typed text was already stripped; the label was not.
func TestOverlayLabelStripsControlCharacters(t *testing.T) {
	o := overlay{kind: overlayConfirm, label: "delete \x1b]0;pwned\x07evil.txt? (y/n)"}

	got := o.view(80)

	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("view = %q, want the escape sequences out of the label", got)
	}
	if !strings.Contains(got, "evil.txt") {
		t.Fatalf("view = %q, want the name itself kept", got)
	}
}
