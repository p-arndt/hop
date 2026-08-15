package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// output is one generated file: a template, where it lands, and how it is
// assembled.
type output struct {
	tmpl     string
	dst      string
	site     bool
	target   string // the name directives aim at with only= / not=
	fallback string // where a section this file does not carry is documented
}

var outputs = []output{
	{tmpl: "docs/_site.html", dst: "index.html", site: true, target: targetSite},
	{tmpl: "docs/_readme.md", dst: "README.md", target: targetReadme, fallback: "KEYBINDINGS.md"},
	{tmpl: "docs/_keybindings.md", dst: "KEYBINDINGS.md", target: targetReference, fallback: "README.md"},
}

func main() {
	root := flag.String("root", ".", "repository root")
	check := flag.Bool("check", false, "fail if a generated file is out of date instead of writing it")
	flag.Parse()

	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, "docsgen:", err)
		os.Exit(1)
	}
}

// normalizeEOL makes a file comparable regardless of how Git checked it out.
func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func run(root string, check bool) error {
	docs, err := LoadDocs(filepath.Join(root, "docs"))
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no sections in docs/")
	}

	var stale []string
	for _, o := range outputs {
		tmpl, err := os.ReadFile(filepath.Join(root, o.tmpl))
		if err != nil {
			return err
		}
		var got string
		if o.site {
			got, err = RenderSite(normalizeEOL(string(tmpl)), docs)
		} else {
			got, err = RenderMarkdownFile(normalizeEOL(string(tmpl)), docs, o.fallback, o.target)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", o.tmpl, err)
		}
		got = strings.TrimRight(got, "\n") + "\n"

		dst := filepath.Join(root, o.dst)
		old, _ := os.ReadFile(dst)
		// Compared after normalising line endings: a Windows checkout has these
		// files as CRLF while docsgen writes LF, and byte equality would call
		// an identical file out of date.
		if normalizeEOL(string(old)) == got {
			continue
		}
		if check {
			stale = append(stale, o.dst)
			continue
		}
		if err := os.WriteFile(dst, []byte(got), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", o.dst)
	}
	if len(stale) > 0 {
		return fmt.Errorf("out of date: %s — run `just docs`", strings.Join(stale, ", "))
	}
	return nil
}
