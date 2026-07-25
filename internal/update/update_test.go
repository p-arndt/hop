package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.1", "0.2.1", 0},
		{"v0.2.2", "0.2.1", 1},
		{"0.2.1", "0.2.2", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1}, // numeric, not lexical
		{"1.2.0", "1.2", 0},    // missing field treated as 0
		{"0.2.2", "0.2.2-pre.1", 1},
		{"0.2.2-pre.1", "0.2.2", -1},
		{"0.2.2-pre.2", "0.2.2-pre.1", 1},
		{"0.2.2-pre.10", "0.2.2-pre.2", 1}, // numeric prerelease identifiers
		{"0.2.2+build.5", "0.2.2", 0},      // build metadata ignored
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("0.2.2", "0.2.1") {
		t.Error("0.2.2 should be newer than 0.2.1")
	}
	if IsNewer("0.2.1", "0.2.1") {
		t.Error("equal versions are not newer")
	}
	if IsNewer("0.2.2", "dev") {
		t.Error("dev builds never see an update")
	}
}

// The names must match exactly what .github/workflows/release.yml publishes —
// a drift here means `hop self-update` looks for an asset no release has.
func TestArchiveAndBinaryNames(t *testing.T) {
	if got := ArchiveName("0.2.2", "linux", "amd64"); got != "hop_0.2.2_linux_amd64.tar.gz" {
		t.Errorf("linux archive name = %q", got)
	}
	if got := ArchiveName("0.2.2", "windows", "arm64"); got != "hop_0.2.2_windows_arm64.zip" {
		t.Errorf("windows archive name = %q", got)
	}
	if got := ChecksumsName("0.2.2"); got != "hop_0.2.2_checksums.txt" {
		t.Errorf("checksums name = %q", got)
	}
	if got := BinaryName("windows"); got != "hop.exe" {
		t.Errorf("windows binary name = %q", got)
	}
	if got := BinaryName("darwin"); got != "hop" {
		t.Errorf("unix binary name = %q", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("pretend archive bytes")
	sum := sha256.Sum256(archive)
	name := "hop_0.2.2_linux_amd64.tar.gz"
	good := fmt.Sprintf("%s  %s\notherhash  other.txt\n", hex.EncodeToString(sum[:]), name)

	if err := verifyChecksum(archive, name, []byte(good)); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	// binary-mode "*" prefix on the name must be tolerated
	star := fmt.Sprintf("%s *%s\n", hex.EncodeToString(sum[:]), name)
	if err := verifyChecksum(archive, name, []byte(star)); err != nil {
		t.Errorf("star-prefixed name rejected: %v", err)
	}
	if err := verifyChecksum([]byte("tampered"), name, []byte(good)); err == nil {
		t.Error("tampered archive should fail checksum")
	}
	if err := verifyChecksum(archive, "missing.tar.gz", []byte(good)); err == nil {
		t.Error("missing name should fail")
	}
}

// makeTarGz builds a gzip'd tar containing one file.
func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// makeZip builds a zip containing one file.
func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	content := []byte("#!binary-content")

	tgz := makeTarGz(t, "hop", content)
	got, err := extractBinary(tgz, "hop", false)
	if err != nil {
		t.Fatalf("tar.gz extract: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("tar.gz content = %q, want %q", got, content)
	}

	zipped := makeZip(t, "hop.exe", content)
	got, err = extractBinary(zipped, "hop.exe", true)
	if err != nil {
		t.Fatalf("zip extract: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("zip content = %q, want %q", got, content)
	}

	// A binary that isn't present must be an error, not empty success.
	if _, err := extractBinary(makeTarGz(t, "README.md", content), "hop", false); err == nil {
		t.Error("expected error when binary absent from archive")
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hop"+exeSuffix())
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := []byte("brand new binary")
	if err := replaceExecutable(exe, newBin); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Errorf("after replace, content = %q, want %q", got, newBin)
	}
}

// On Windows the running binary is renamed aside rather than overwritten, and
// the leftover is swept up on the next start — so the swap must leave exactly
// one usable hop behind, and CleanupLeftovers must not touch anything else.
func TestReplaceExecutableLeavesNoStaleCopyOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the .old file is expected on windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "hop")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the replaced binary, got %d entries", len(entries))
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// TestSelfUpdateEndToEnd exercises the full check → download → verify → install
// path against a fake GitHub, swapping a real temp "binary" on disk.
func TestSelfUpdateEndToEnd(t *testing.T) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	binName := BinaryName(goos)
	newContent := []byte("the updated hop binary")

	var archive []byte
	if goos == "windows" {
		archive = makeZip(t, binName, newContent)
	} else {
		archive = makeTarGz(t, binName, newContent)
	}
	version := "9.9.9"
	archiveName := ArchiveName(version, goos, goarch)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/p-arndt/hop/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		rel := Release{
			Tag: "v" + version,
			Assets: []Asset{
				{Name: archiveName, URL: base + "/dl/archive"},
				{Name: ChecksumsName(version), URL: base + "/dl/sums"},
			},
		}
		json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/dl/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(checksums)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	c := &Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}

	// Point the updater at a temp file standing in for the running binary.
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, binName)
	if err := os.WriteFile(fakeExe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := executablePath
	t.Cleanup(func() { executablePath = orig })
	executablePath = func() (string, error) { return fakeExe, nil }

	res, err := c.SelfUpdate(context.Background(), "0.2.1", false)
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !res.Updated {
		t.Fatal("expected Updated=true")
	}
	if res.Latest != version {
		t.Errorf("Latest = %q, want %q", res.Latest, version)
	}
	got, err := os.ReadFile(fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("binary not swapped: got %q", got)
	}
}

// check-update must report the newer version without touching the binary — it
// is the half of the feature users run before they are ready to swap.
func TestSelfUpdateCheckOnlyDoesNotInstall(t *testing.T) {
	version := "9.9.9"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/p-arndt/hop/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Tag: "v" + version})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}

	// If checkOnly leaked into the install path it would fail here looking for
	// assets the release doesn't have — the assertion is that it returns clean.
	res, err := c.SelfUpdate(context.Background(), "0.2.1", true)
	if err != nil {
		t.Fatalf("SelfUpdate(checkOnly): %v", err)
	}
	if res.Updated {
		t.Error("check-only must not install")
	}
	if res.Latest != version {
		t.Errorf("Latest = %q, want %q", res.Latest, version)
	}
}

func TestSelfUpdateAlreadyLatest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/p-arndt/hop/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Tag: "v0.2.1"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}

	res, err := c.SelfUpdate(context.Background(), "0.2.1", false)
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if res.Updated {
		t.Error("should not update when already on latest")
	}
}

func TestSelfUpdateRejectsDevBuild(t *testing.T) {
	c := NewClient(nil)
	if _, err := c.SelfUpdate(context.Background(), "dev", false); err == nil {
		t.Error("dev build should be refused before any network call")
	}
}

func TestSelfUpdateChecksumMismatch(t *testing.T) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	binName := BinaryName(goos)
	var archive []byte
	if goos == "windows" {
		archive = makeZip(t, binName, []byte("content"))
	} else {
		archive = makeTarGz(t, binName, []byte("content"))
	}
	version := "9.9.9"
	archiveName := ArchiveName(version, goos, goarch)
	badSums := fmt.Sprintf("%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", archiveName)

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/p-arndt/hop/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Tag: "v" + version, Assets: []Asset{
			{Name: archiveName, URL: base + "/dl/archive"},
			{Name: ChecksumsName(version), URL: base + "/dl/sums"},
		}})
	})
	mux.HandleFunc("/dl/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(badSums)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	c := &Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}

	dir := t.TempDir()
	fakeExe := filepath.Join(dir, binName)
	os.WriteFile(fakeExe, []byte("old binary"), 0o755)
	orig := executablePath
	t.Cleanup(func() { executablePath = orig })
	executablePath = func() (string, error) { return fakeExe, nil }

	if _, err := c.SelfUpdate(context.Background(), "0.2.1", false); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	// The original binary must be untouched after a failed verify.
	got, _ := os.ReadFile(fakeExe)
	if string(got) != "old binary" {
		t.Errorf("binary changed despite failed update: %q", got)
	}
}
