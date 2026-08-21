package main

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// md is the shared markdown parser; auto heading IDs let search deep-link subheadings.
var md = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.Typographer, kbdExt{}),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// ---- [[key]] -> <kbd>key</kbd> ----------------------------------------------

type kbdExt struct{}

func (kbdExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(kbdParser{}, 100)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(kbdRenderer{}, 100)))
}

type kbdNode struct {
	ast.BaseInline
	Key string
}

var kbdKind = ast.NewNodeKind("Kbd")

func (n *kbdNode) Kind() ast.NodeKind         { return kbdKind }
func (n *kbdNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type kbdParser struct{}

func (kbdParser) Trigger() []byte { return []byte{'['} }

// An inline parser, not a text substitution: only a parser knows it is not inside a code span.
func (kbdParser) Parse(_ ast.Node, block gtext.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if !bytes.HasPrefix(line, []byte("[[")) {
		return nil
	}
	end := bytes.Index(line, []byte("]]"))
	if end < 2 {
		return nil
	}
	key := string(line[2:end])
	if key == "" || strings.ContainsAny(key, "[]") {
		return nil
	}
	block.Advance(end + 2)
	return &kbdNode{Key: key}
}

type kbdRenderer struct{}

func (kbdRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kbdKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			fmt.Fprintf(w, "<kbd>%s</kbd>", html.EscapeString(node.(*kbdNode).Key))
		}
		return ast.WalkContinue, nil
	})
}

// ---- markdown -> HTML --------------------------------------------------------

// RenderHTML renders a doc body, moving every heading down shift levels.
func RenderHTML(src string, shift int) (string, error) {
	src, blocks, err := extractDirectives(src, shift)
	if err != nil {
		return "", err
	}
	src = shiftHeadings(src, shift)

	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	out := buf.String()
	out = wrapTables(out)
	out = markCodeComments(out)
	out = spliceDirectives(out, blocks)
	return strings.TrimSpace(out), nil
}

func spliceDirectives(in string, blocks []string) string {
	for i, b := range blocks {
		in = strings.ReplaceAll(in, "<p>"+placeholder(i)+"</p>", b)
		in = strings.ReplaceAll(in, placeholder(i), b)
	}
	return in
}

func placeholder(i int) string { return fmt.Sprintf("docsgen-block-%d", i) }

var (
	fenceRe     = regexp.MustCompile("(?m)^(?:```|~~~)")
	headingRe   = regexp.MustCompile(`^(#{1,5}) `)
	tableRe     = regexp.MustCompile(`(?s)<table>.*?</table>`)
	commentRe   = regexp.MustCompile(`(?m)(^|\s)(#[^\n]*)$`)
	preRe       = regexp.MustCompile(`(?s)<pre><code[^>]*>.*?</code></pre>`)
	firstCellRe = regexp.MustCompile(`(?s)<tr>\s*<td[^>]*>(.*?)</td>`)
)

// shiftHeadings pushes ATX headings down, leaving fenced code alone.
func shiftHeadings(src string, shift int) string {
	if shift <= 0 {
		return src
	}
	lines := strings.Split(src, "\n")
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			lines[i] = strings.Repeat("#", len(m[1])+shift) + line[len(m[1]):]
		}
	}
	return strings.Join(lines, "\n")
}

// wrapTables adds the scroll wrapper the stylesheet expects; key tables get a class.
func wrapTables(in string) string {
	return tableRe.ReplaceAllStringFunc(in, func(t string) string {
		class := ""
		if strings.Contains(firstColumn(t), "<kbd>") {
			class = ` class="keytable"`
		}
		return `<div class="tablewrap">` + strings.Replace(t, "<table>", "<table"+class+">", 1) + `</div>`
	})
}

func firstColumn(table string) string {
	var b strings.Builder
	for _, m := range firstCellRe.FindAllStringSubmatch(table, -1) {
		b.WriteString(m[1])
	}
	return b.String()
}

// markCodeComments dims trailing "# …" comments in shell snippets.
func markCodeComments(in string) string {
	return preRe.ReplaceAllStringFunc(in, func(block string) string {
		if !strings.Contains(block, "language-bash") && !strings.Contains(block, "language-powershell") &&
			!strings.Contains(block, "language-sh") && !strings.Contains(block, "language-console") {
			return block
		}
		return commentRe.ReplaceAllString(block, `$1<span class="c">$2</span>`)
	})
}
