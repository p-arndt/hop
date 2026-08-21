package main

import (
	"strings"
	"testing"
)

func mustHTML(t *testing.T, src string, shift int) string {
	t.Helper()
	out, err := RenderHTML(src, shift)
	if err != nil {
		t.Fatalf("RenderHTML(%q): %v", src, err)
	}
	return out
}

func mustMD(t *testing.T, src string, shift int) string {
	t.Helper()
	out, err := RenderMarkdown(src, shift, targetReadme)
	if err != nil {
		t.Fatalf("RenderMarkdown(%q): %v", src, err)
	}
	return out
}

func contains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestParseDocFrontmatter(t *testing.T) {
	doc, err := ParseDoc("---\nid: hostlist\ntitle: The host list\ngroup: Navigation\n---\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "hostlist" || doc.Title != "The host list" || doc.Group != "Navigation" {
		t.Fatalf("parsed %+v", doc)
	}
	if !doc.Site {
		t.Error("site should default to true")
	}
	if doc.NavLabel() != "The host list" {
		t.Errorf("nav label falls back to the title, got %q", doc.NavLabel())
	}
	if doc.Body != "body" {
		t.Errorf("body = %q", doc.Body)
	}
}

func TestParseDocRejectsBadFrontmatter(t *testing.T) {
	for name, src := range map[string]string{
		"no frontmatter": "# hi\n",
		"unknown key":    "---\ntitle: x\nwidht: 3\n---\nbody\n",
		"no title":       "---\nid: x\n---\nbody\n",
	} {
		if _, err := ParseDoc(src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestKbdSyntax(t *testing.T) {
	got := mustHTML(t, "press [[ctrl+o]] then `[[not a key]]`", 0)
	contains(t, got, "<kbd>ctrl+o</kbd>")
	if strings.Contains(got, "<code><kbd>") {
		t.Errorf("a code span must stay literal:\n%s", got)
	}
}

func TestKbdLowersToCodeSpan(t *testing.T) {
	got := mustMD(t, "press [[ctrl+o]]\n\n```\n[[literal]]\n```", 0)
	contains(t, got, "press `ctrl+o`", "[[literal]]")
}

func TestTablesAreWrappedAndKeyTablesTagged(t *testing.T) {
	keys := mustHTML(t, "| Key | Action |\n| --- | --- |\n| [[q]] | quit |", 0)
	contains(t, keys, `<div class="tablewrap">`, `<table class="keytable">`)

	plain := mustHTML(t, "| What | Path |\n| --- | --- |\n| db | `hop.db` |", 0)
	contains(t, plain, `<div class="tablewrap">`)
	if strings.Contains(plain, "keytable") {
		t.Errorf("a table with no keys in its first column is not a key table:\n%s", plain)
	}
}

func TestHeadingShiftLeavesCodeAlone(t *testing.T) {
	got := shiftHeadings("## Real\n\n```\n# a shell comment\n```\n", 1)
	contains(t, got, "### Real", "\n# a shell comment")
}

func TestDirectivesRenderToHTML(t *testing.T) {
	got := mustHTML(t, ":::note\nmind this\n:::", 0)
	contains(t, got, `<div class="note">`, "mind this")

	got = mustHTML(t, ":::why Because [[esc]]\nreasons\n:::", 0)
	contains(t, got, `<details class="why"><summary>Because <kbd>esc</kbd></summary>`, "reasons")

	got = mustHTML(t, ":::cards\n### 🖥️ Shells\nreal terminals\n\n### Tunnels\nforwards\n:::", 0)
	contains(t, got, `<div class="grid">`, `<div class="card"><h4>🖥️ Shells</h4>`, "<p>forwards</p>")

	got = mustHTML(t, `:::figure src="a.png" alt="an a" width="10" height="5" max="4rem"`+"\nthe caption\n:::", 0)
	contains(t, got, `<figure style="max-width:4rem">`, `src="a.png"`, `width="10" height="5"`,
		`loading="lazy"`, "<figcaption>the caption</figcaption>")
}

func TestNestedDirectives(t *testing.T) {
	got := mustHTML(t, "::::columns cols=\"1fr 1fr\"\n:::col\nleft\n:::\n:::col\nright\n:::\n::::", 0)
	contains(t, got, `<div class="shots" style="grid-template-columns:1fr 1fr">`, "<p>left</p>", "<p>right</p>")
}

func TestUnclosedDirectiveIsAnError(t *testing.T) {
	if _, err := RenderHTML(":::note\nno end", 0); err == nil {
		t.Fatal("expected an error for an unclosed directive")
	}
	if _, err := RenderHTML(":::nonsense\nx\n:::", 0); err == nil {
		t.Fatal("expected an error for an unknown directive")
	}
}

func TestModesRenderAsCardsAndStayATable(t *testing.T) {
	src := ":::modes\n| Mode | When | Who | Read next |\n| --- | --- | --- | --- |\n" +
		"| **Navigation** | the list is focused | hop | [Host list](#hostlist) |\n:::"

	html := mustHTML(t, src, 0)
	contains(t, html, `<div class="modes">`, "<b>Navigation</b>", `<span class="who">keys go to hop</span>`,
		`<ul class="modelinks"><li><a href="#hostlist">Host list</a></li></ul>`)
	if strings.Contains(html, "<b><strong>") {
		t.Errorf("the card heading is the emphasis, it should not be doubled:\n%s", html)
	}

	md := mustMD(t, src, 0)
	contains(t, md, "| **Navigation** | the list is focused | hop |")
}

func TestCardsLowerToTheReadmeTable(t *testing.T) {
	got := mustMD(t, ":::cards\n### 🖥️ Shells\nreal\nterminals\n\n### Tunnels\nforwards\n:::", 0)
	contains(t, got, "|  | |", "| 🖥️ **Shells** | real terminals |", "| **Tunnels** | forwards |")
}

func TestNoteLowersToAGitHubAlert(t *testing.T) {
	contains(t, mustMD(t, ":::note\nmind this\n:::", 0), "> [!NOTE]\n> mind this")
}

func TestWhyLowersToDetails(t *testing.T) {
	got := mustMD(t, ":::why The reason\nbecause\n:::", 0)
	contains(t, got, "<details>\n<summary><b>The reason</b></summary>", "because", "</details>")
}

func TestBlocksCanOptOutOfOneOutput(t *testing.T) {
	src := ":::why not=\"readme\" The reason\nbecause\n:::"

	readme, err := RenderMarkdown(src, 0, targetReadme)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readme, "because") {
		t.Errorf("not=readme must not reach the README:\n%s", readme)
	}

	ref, err := RenderMarkdown(src, 0, targetReference)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, ref, "<summary><b>The reason</b></summary>", "because")
	if strings.Contains(ref, "not=") {
		t.Errorf("the attribute must not land in the summary:\n%s", ref)
	}

	contains(t, mustHTML(t, src, 0), "<summary>The reason</summary>", "because")
}

func TestSiteOnlyBlocksLeaveNoTraceInMarkdown(t *testing.T) {
	src := "before\n\n:::figure only=\"site\" src=\"a.png\" alt=\"a\"\ncaption\n:::\n\nafter"
	got := mustMD(t, src, 0)
	contains(t, got, "before", "after")
	if strings.Contains(got, "a.png") || strings.Contains(got, "caption") {
		t.Errorf("only=site must not reach the markdown:\n%s", got)
	}
	contains(t, mustHTML(t, src, 0), "a.png")
}

func TestSearchIndexCoversProseAndKeys(t *testing.T) {
	doc := &Doc{ID: "keys", Title: "The keys", Site: true}
	body := mustHTML(t, "## Second thoughts\n\nsome prose\n\n| Key | Action |\n| --- | --- |\n| [[q]] | quit |", 1)
	entries := BuildSearchIndex([]*Doc{doc}, map[string]string{"keys": body})

	var texts, anchors []string
	for _, e := range entries {
		texts = append(texts, e.Text)
		anchors = append(anchors, e.Anchor)
	}
	joined := strings.Join(texts, "\n")
	contains(t, joined, "The keys", "Second thoughts", "some prose", "q · quit")
	if !strings.Contains(strings.Join(anchors, " "), "#second-thoughts") {
		t.Errorf("a subheading should get its own anchor, got %v", anchors)
	}
}

func TestSearchIndexSkipsNavLinks(t *testing.T) {
	doc := &Doc{ID: "modes", Title: "Modes", Site: true}
	body := `<div class="modes"><div class="mode"><b>Nav</b><ul class="modelinks"><li><a href="#x">Host list</a></li></ul></div></div>`
	for _, e := range BuildSearchIndex([]*Doc{doc}, map[string]string{"modes": body}) {
		if e.Text == "Host list" {
			t.Fatalf("the mode strip's own links are navigation, not content: %+v", e)
		}
	}
}

func TestSlugMatchesGitHubAnchors(t *testing.T) {
	for title, want := range map[string]string{
		"Navigation — the host list": "navigation--the-host-list",
		"The sidebar — [[ctrl+b]]":   "the-sidebar--ctrlb",
		"Copy and paste":             "copy-and-paste",
	} {
		if got := slug(title); got != want {
			t.Errorf("slug(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestCrossReferencesRetargetPerFile(t *testing.T) {
	docs := []*Doc{
		{ID: "vim", Title: "Vim keys", Site: true, Body: "vim"},
		{ID: "hostlist", Title: "The host list", Site: true, Body: "see [vim](#vim) and [self](#hostlist)"},
	}

	// A file that carries both sections links inside itself.
	both, err := RenderMarkdownFile("<!-- docs:hostlist -->\n\n<!-- docs:vim -->\n", docs, "KEYBINDINGS.md", targetReference)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, both, "[vim](#vim-keys)", "[self](#the-host-list)")

	// A section hidden behind <details> has no anchor, so the link leaves for the file that has one.
	one, err := RenderMarkdownFile("<!-- docs:hostlist details=\"Keys\" -->\n", docs, "KEYBINDINGS.md", targetReadme)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, one, "[vim](KEYBINDINGS.md#vim-keys)", "<summary><b>Keys</b></summary>")
}

func TestUnknownSectionIDIsAnError(t *testing.T) {
	_, err := RenderMarkdownFile("<!-- docs:nope -->\n", []*Doc{{ID: "yes", Title: "Yes"}}, "", targetReadme)
	if err == nil {
		t.Fatal("expected an error for an unknown section id")
	}
}

func TestSiteFalseKeepsASectionOffThePage(t *testing.T) {
	docs := []*Doc{{ID: "hidden", Title: "Hidden", Site: false, Body: "secret"}}
	out, err := RenderSite("<!-- docsgen:nav -->\n<!-- docsgen:sections -->\n<!-- docsgen:search -->", docs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "#hidden") {
		t.Errorf("site: false must not render:\n%s", out)
	}
}

// Regression: CRLF checkouts on Windows CI made identical files compare as out of date.
func TestCRLFCheckoutIsStillUpToDate(t *testing.T) {
	const lf = "a\nb\n"
	if got := normalizeEOL("a\r\nb\r\n"); got != lf {
		t.Errorf("normalizeEOL = %q, want %q", got, lf)
	}
	if got := normalizeEOL(lf); got != lf {
		t.Errorf("normalizeEOL must leave LF alone, got %q", got)
	}
}

// Fails when index.html, README.md or KEYBINDINGS.md has drifted from docs/.
func TestGeneratedFilesAreUpToDate(t *testing.T) {
	if err := run("../..", true); err != nil {
		t.Fatalf("%v", err)
	}
}
