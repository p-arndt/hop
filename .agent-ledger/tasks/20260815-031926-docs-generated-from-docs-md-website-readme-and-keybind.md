---
id: "20260815-031926-docs-generated-from-docs-md-website-readme-and-keybind"
title: "Docs generated from docs/*.md: website, README and KEYBINDINGS from one source"
status: "completed"
updated: "2026-08-15T03:20:16+02:00"
base_commit: "ac4c98ce55954408c4cfac97bed5bbb3dff931e9"
branch: "main"
agent: null
tags: ["docs", "docsgen", "pages", "search", "website"]
files: []
---

# Docs generated from docs/*.md: website, README and KEYBINDINGS from one source

## Goal

Make docs/*.md the only source for the website, the README and the keybinding reference; replace the in-browser LLM assistant on the site with an offline search.

## Scope

## Discoveries

- index.html was hand-written and had drifted badly from the code: it documented alt+0/alt+arrow pane keys that the leader rework replaced, and knew nothing of tunnels, ProxyCommand/ProxyJump, reconnect, the mouse or copy/paste. KEYBINDINGS.md was the only accurate reference, so it — not the old HTML — was the source of truth when writing docs/.

- Three outputs, one source: docs/*.md holds the sections, docs/_site.html, docs/_readme.md and docs/_keybindings.md hold the chrome and pull sections in by id (<!-- docs:hostlist level=3 details="..." -->). The README's GitHub look (centred header, badges, emoji headings, stills table) lives in its template, not in the sections.

- Anchors differ per output: on the site an id IS the anchor; in markdown GitHub derives it from the heading text. retargetLinks() rewrites [..](#id) to the GitHub slug when the file renders that section under a heading, and to KEYBINDINGS.md#slug when it does not (README hides most key sections inside <details>, which have no anchor).

## Decisions

- **Decision:** Fenced directives (:::cards, :::why, :::note, :::figure, :::columns, :::modes) with an HTML rendering AND a plain-markdown lowering, rather than raw HTML in the markdown.
  - **Reason:** The same section has to read well as a styled web page and as GitHub markdown; raw HTML would render as a wall of tags in the README.
  - **Trade-off:** A small bespoke syntax to learn, and every new directive needs both renderings or one output silently loses content.

- **Decision:** [[key]] is parsed by a goldmark inline parser, not by a regex over the source.
  - **Reason:** Only a parser knows it is not inside a code span; a text substitution would rewrite [[...]] in fenced examples.

- **Decision:** The search index is built from the RENDERED HTML of each section, one entry per paragraph, list item, table row and heading, embedded in the page as JSON.
  - **Reason:** What a reader can see is then exactly what search can find, including every key table row ('ctrl+o o · out — back to hop'). No network, no service worker; works over file://.
  - **Trade-off:** The index is ~40 KB of the 154 KB page. Nav-only links (ul.modelinks) had to be stripped explicitly, and sidebar labels are indexed as aliases so 'scrollback' finds the section titled 'Scrolling back through history'.

- **Decision:** Directives can opt out of one output: only="site" renders on the website alone, not="readme" everywhere but there. The eight :::why rationale blocks carry not="readme".
  - **Reason:** Pulling whole sections into the README made its Keys section 45% the length of KEYBINDINGS.md — the rationale belongs in the reference, not the front page.
  - **Trade-off:** Same fact now renders differently per output, so a section has to be read with a target in mind.

- **Decision:** The in-browser WebLLM assistant (Qwen3.5-0.8B, ~600 MB download, WebGPU) is deleted rather than kept alongside search.
  - **Reason:** Explicit user request; a 0.8B model answering from the page is strictly worse than finding the line on the page.

## Failures

- **Approach:** Rendering a directive's body straight into the markdown stream so goldmark would parse it.
  - **Lesson:** goldmark treats an HTML block as raw and stops parsing markdown inside it. Directives are instead extracted first, rendered recursively, replaced by a placeholder word that survives as its own paragraph, and spliced back afterwards — which is also why directive HTML never has to be escaped past goldmark.

## Validation

- Guard rail: tools/docsgen -check re-renders and diffs against the committed files, and TestGeneratedFilesAreUpToDate runs it from the test suite — so plain 'go test ./...' in CI already fails when index.html/README.md/KEYBINDINGS.md drift from docs/. No CI workflow change was needed.

- go test ./... — passed (incl. TestGeneratedFilesAreUpToDate); go vet ./... clean; gofmt clean. index.html tag-balance checked with a parser; rendered in Chromium and read: hero, feature cards, key tables, search overlay in light and dark. Live site verified after deploy: HTTP 200, title 'hop — documentation', 28 sections, 377 search entries, 0 hits for askbtn/web-llm.

## Remaining risks

- GitHub Pages legacy builds fail on a branch whose name contains a slash: the same commit errored as docs/html-docs-page + / and built fine as main + /. Only symptom is 'Page build failed.' with no log — build_type is legacy, so nothing is exposed. If a preview from a feature branch is ever wanted, use a branch without a slash or switch build_type to workflow.

- The markdown lowering of [[key]] skips fenced code but not inline code spans, so a literal [[...]] inside backticks in a doc body would still be rewritten in README/KEYBINDINGS (the HTML path is safe — it uses a real parser).

- docsgen renders the whole site on every run and has no incremental mode; fine at 28 sections, and the -check diff is what CI relies on.

## Handoff

- Do not hand-edit index.html, README.md or KEYBINDINGS.md — they are generated. Edit docs/*.md (content) or docs/_*.{html,md} (chrome) and run 'just docs'.

- GitHub Pages serves main / root; .nojekyll at the repo root keeps Pages from running Jekyll over docs/*.md and shipping them as pages of their own.

- If the README's Keys section is still felt to be too long, the next lever is dropping the tunnels/reconnect placeholders from docs/_readme.md — retargetLinks then points those references at KEYBINDINGS.md by itself.
