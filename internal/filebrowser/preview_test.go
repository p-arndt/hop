package filebrowser

import "testing"

// A late read carrying the right number for another file is dropped: seq starts again in a
// reopened browser, so it alone cannot tell the loads apart.
func TestPreviewDropsAResultForAnotherFile(t *testing.T) {
	b := &Browser{preview: preview{path: "/home/u/notes", seq: 1, loading: true}}

	b.previewLanded(previewLoadedMsg{seq: 1, path: "/etc/big.conf", data: []byte("secret")})
	if b.preview.text != "" || !b.preview.loading {
		t.Fatalf("a result for another file landed: %+v", b.preview)
	}

	b.previewLanded(previewLoadedMsg{seq: 1, path: "/home/u/notes", data: []byte("hello")})
	if b.preview.text != "hello" || b.preview.loading {
		t.Fatalf("the matching result did not land: %+v", b.preview)
	}
}

// A refresh reads the preview again, since the file may have changed under it.
func TestRefreshForgetsThePreview(t *testing.T) {
	b, _ := newTestBrowser(3)
	b.preview = preview{path: "/home/u/f", seq: 4, text: "old"}

	if !b.refresh() {
		t.Fatal("refresh failed")
	}
	if b.preview.path != "" || b.preview.text != "" || b.preview.seq != 4 {
		t.Fatalf("preview after refresh = %+v, want it forgotten with its sequence kept", b.preview)
	}
}
