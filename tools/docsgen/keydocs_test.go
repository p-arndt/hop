package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"hop/internal/keys"
)

// Guards docs/*.md drifting from the key registry: every bound key must be named in docs/.

// keyToken matches the docs' keycap markup, [[ctrl+o]].
var keyToken = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// docSpelling maps the docs' spelling of a key to the registry's.
var docSpelling = strings.NewReplacer(
	"↑", "up", "↓", "down", "←", "left", "→", "right",
	"pgdn", "pgdown", "gg", "g g",
)

// rangeKeys are documented as a range ("[[1]] … [[9]]") and are not in the registry.
var rangeKeys = map[string]bool{
	"2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true,
	"alt+2": true, "alt+3": true, "alt+4": true, "alt+5": true,
	"alt+6": true, "alt+7": true, "alt+8": true,
}

// allDocumented reports whether every keystroke of a key or sequence is named in the docs.
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

func TestEveryBoundKeyIsDocumented(t *testing.T) {
	documented := documentedKeys(t)

	layers := []keys.Layer{
		keys.Global, keys.List, keys.Browser, keys.Pane,
		keys.Scrollback, keys.Editor, keys.Leader, keys.DeadPane,
	}
	for _, l := range layers {
		for _, b := range keys.Defaults().Layer(l, true) {
			for _, k := range b.Keys {
				// A sequence is documented by its keys: "esc esc" is written [[esc]] [[esc]].
				if allDocumented(documented, k) {
					continue
				}
				// The docs write "shift+k" where the registry says "K".
				if b.Show != "" && documented[keys.Normalize(b.Show)] {
					continue
				}
				t.Errorf("%s binds %q in the %s layer, and no doc names it", b.Action, k, l)
			}
		}
	}
}
