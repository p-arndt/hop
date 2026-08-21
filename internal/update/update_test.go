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
	"strings"
	"testing"
	"time"

	"github.com/p-arndt/selfupdate"
)

// Covers hop's wiring around selfupdate — asset names and user-facing strings — offline.

// assetNames mirrors .github/workflows/release.yml verbatim.
func assetNames(version string) (archive, binary, checksums string) {
	archive = fmt.Sprintf("hop_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	binary = "hop"
	if runtime.GOOS == "windows" {
		archive = fmt.Sprintf("hop_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
		binary = "hop.exe"
	}
	return archive, binary, fmt.Sprintf("hop_%s_checksums.txt", version)
}

// fakeRelease serves a whole GitHub release from one loopback server and returns its base URL.
func fakeRelease(t *testing.T, tag string, assets map[string][]byte) string {
	t.Helper()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name := strings.TrimPrefix(r.URL.Path, "/assets/"); name != r.URL.Path {
			body, ok := assets[name]
			if !ok {
				http.Error(w, "no such asset", http.StatusNotFound)
				return
			}
			w.Write(body)
			return
		}
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		out := struct {
			Tag    string  `json:"tag_name"`
			Assets []asset `json:"assets"`
		}{Tag: tag}
		for name := range assets {
			out.Assets = append(out.Assets, asset{Name: name, URL: srv.URL + "/assets/" + name})
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// releaseAssets builds what the release job uploads; the sha256 is over the archive.
func releaseAssets(t *testing.T, version string, binary []byte) map[string][]byte {
	t.Helper()
	archiveName, binName, checksumsName := assetNames(version)

	archive := tarGz(t, binName, binary)
	if runtime.GOOS == "windows" {
		archive = zipped(t, binName, binary)
	}
	sum := sha256.Sum256(archive)

	return map[string][]byte{
		archiveName:   archive,
		checksumsName: []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)),
	}
}

func zipped(t *testing.T, name string, content []byte) []byte {
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
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tarGz(t *testing.T, name string, content []byte) []byte {
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
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testUpdater wires a fake release, a throwaway install target and a temp cache.
func testUpdater(t *testing.T, apiBase string) (*selfupdate.Updater, string) {
	t.Helper()

	exe := filepath.Join(t.TempDir(), "hop")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "update-check.json")

	up, err := New(selfupdate.Config{
		APIBase:        apiBase,
		StatePath:      func() (string, error) { return cache, nil },
		ExecutablePath: func() (string, error) { return exe, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return up, exe
}

// The whole path against the workflow's asset names: check, download, verify, swap.
func TestSelfUpdateInstallsNewerRelease(t *testing.T) {
	up, exe := testUpdater(t, fakeRelease(t, "v1.2.0", releaseAssets(t, "1.2.0", []byte("the new binary"))))

	res, err := up.SelfUpdate(context.Background(), "1.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated {
		t.Fatalf("Updated = false, want true (result: %+v)", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "the new binary" {
		t.Errorf("installed binary = %q, want the new binary", got)
	}
}

func TestCheckOnlyDoesNotInstall(t *testing.T) {
	up, exe := testUpdater(t, fakeRelease(t, "v1.2.0", releaseAssets(t, "1.2.0", []byte("the new binary"))))

	res, err := up.SelfUpdate(context.Background(), "1.0.0", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Error("Updated = true in check-only mode")
	}
	if res.Latest != "1.2.0" {
		t.Errorf("Latest = %q, want 1.2.0", res.Latest)
	}
	if got, _ := os.ReadFile(exe); string(got) != "the old binary" {
		t.Error("check-only wrote to the binary")
	}
}

func TestDevBuildIsRefused(t *testing.T) {
	up, exe := testUpdater(t, fakeRelease(t, "v1.2.0", releaseAssets(t, "1.2.0", []byte("the new binary"))))

	if _, err := up.SelfUpdate(context.Background(), "dev", false); err == nil {
		t.Fatal("a dev build was updated")
	}
	if got, _ := os.ReadFile(exe); string(got) != "the old binary" {
		t.Error("a dev build was written over")
	}
}

func TestSameVersionIsNotAnUpdate(t *testing.T) {
	up, _ := testUpdater(t, fakeRelease(t, "v1.0.0", releaseAssets(t, "1.0.0", []byte("the same binary"))))

	res, err := up.SelfUpdate(context.Background(), "1.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated || IsNewer(res.Latest, res.Current) {
		t.Errorf("1.0.0 treated as an update over itself: %+v", res)
	}
}

// noticeUpdater seeds the cache with a fresh check so nothing goes near the network.
func noticeUpdater(t *testing.T, latest string) *selfupdate.Updater {
	t.Helper()

	cache := filepath.Join(t.TempDir(), "update-check.json")
	data, err := json.Marshal(struct {
		LastCheck time.Time `json:"last_check"`
		Latest    string    `json:"latest"`
	}{LastCheck: time.Now(), Latest: latest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, data, 0o600); err != nil {
		t.Fatal(err)
	}

	up, err := New(selfupdate.Config{
		APIBase:   "http://127.0.0.1:1", // unreachable: a fresh check would be a bug
		StatePath: func() (string, error) { return cache, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return up
}

// The library's derived default would say `hop update`, which hop does not have.
func TestNoticeNamesHopSelfUpdate(t *testing.T) {
	var out bytes.Buffer
	noticeUpdater(t, "1.2.0").NotifyIfAvailable(&out, "1.0.0")

	want := "A newer hop is available: 1.2.0 (you have 1.0.0). Run `hop self-update` to upgrade."
	if !strings.Contains(out.String(), want) {
		t.Errorf("notice = %q, want it to contain %q", out.String(), want)
	}
}

func TestNoticeRespectsHopOptOut(t *testing.T) {
	t.Setenv("HOP_NO_UPDATE_CHECK", "1")

	up := noticeUpdater(t, "1.2.0")
	if got := up.DisableEnvName(); got != "HOP_NO_UPDATE_CHECK" {
		t.Errorf("DisableEnvName = %q, want HOP_NO_UPDATE_CHECK", got)
	}

	var out bytes.Buffer
	up.NotifyIfAvailable(&out, "1.0.0")
	if out.Len() != 0 {
		t.Errorf("notice = %q with the opt-out set, want silence", out.String())
	}
	if got := Refresh("1.0.0"); got != "" {
		t.Errorf("Refresh = %q with the opt-out set, want \"\"", got)
	}
}

// Reports nothing when already current.
func TestRefreshReportsNewerVersion(t *testing.T) {
	if got := noticeUpdater(t, "1.2.0").Refresh("1.0.0"); got != "1.2.0" {
		t.Errorf("Refresh = %q, want 1.2.0", got)
	}
	if got := noticeUpdater(t, "1.0.0").Refresh("1.0.0"); got != "" {
		t.Errorf("Refresh = %q on the latest version, want \"\"", got)
	}
}
