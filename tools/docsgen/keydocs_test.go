package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"hop/internal/keys"
)

// The docs are prose about the keyboard, and internal/keys is the keyboard. Nothing can
// generate one from the other — a row of the reference says more than a label ever will,
// and half the keys it mentions belong to a card or to the remote program rather than to
// hop. What can be checked is that the prose still covers the keyboard: every key hop
// ships bound is named somewhere in docs/, in whatever spelling the docs use for it.
//
// This is the drift test the generated files already have, aimed one level further back:
// `just docs` keeps README.md and KEYBINDINGS.md in step with docs/*.md, and this keeps
// docs/*.md in step with the code.

// keyToken matches the docs' keycap markup, [[ctrl+o]].
var keyToken = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// docSpelling maps the docs' spelling of a key to the registry's. The docs draw the
// arrows and print "pgdn", because they are read by a person; bubbletea names them "up"
// and "pgdown", because they are read by a switch.
var docSpelling = strings.NewReplacer(
	"↑", "up", "↓", "down", "←", "left", "→", "right",
	"pgdn", "pgdown", "gg", "g g",
)

// rangeKeys are documented as a range ("[[1]] … [[9]]") rather than one key each, and are
// not in the registry at all — a digit addresses a tab by its number, which is not
// something a config could rebind to still mean "the third one".
var rangeKeys = map[string]bool{
	"2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true,
	"alt+2": true, "alt+3": true, "alt+4": true, "alt+5": true,
	"alt+6": true, "alt+7": true, "alt+8": true,
}

// allDocumented reports whether every keystroke of a key or sequence is named in the
// docs, treating the digit ranges as named.
func allDocumented(documented map[string]bool, key string) bool {
	for _, k := range strings.Split(key, " ") {
		if !documented[k] && !rangeKeys[k] {
			return false
		}
	}
	return true
}

// documentedKeys is every key named anywhere in docs/, in the registry's spelling.
func documentedKeys(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no docs found: %v", err)
	}

	out := map[string]bool{}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for _, m := range keyToken.FindAllStringSubmatch(string(src), -1) {
			key := docSpelling.Replace(strings.TrimSpace(m[1]))
			out[keys.Normalize(key)] = true
		}
	}
	return out
}

// A key hop binds but never mentions is a key nobody can find: the docs are the only
// place the whole keyboard is written out in prose, and the help card is read from inside
// hop by someone who already got that far.
func TestEveryBoundKeyIsDocumented(t *testing.T) {
	documented := documentedKeys(t)

	layers := []keys.Layer{
		keys.Global, keys.List, keys.Browser, keys.Pane,
		keys.Scrollback, keys.Editor, keys.Leader, keys.DeadPane,
	}
	for _, l := range layers {
		for _, b := range keys.Defaults().Layer(l, true) {
			for _, k := range b.Keys {
				// A sequence is documented by its keys: "esc esc" is written [[esc]]
				// [[esc]], because that is what the hand does.
				if allDocumented(documented, k) {
					continue
				}
				// The symbol the card draws counts as the key being named: the docs write
				// "shift+k" where the registry says "K".
				if b.Show != "" && documented[keys.Normalize(b.Show)] {
					continue
				}
				t.Errorf("%s binds %q in the %s layer, and no doc names it", b.Action, k, l)
			}
		}
	}
}
