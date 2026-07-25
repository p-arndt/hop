// Package update handles self-updating the hop binary from GitHub Releases and
// the lightweight "a newer version is available" notice.
//
// The release pipeline publishes one archive per platform named
// hop_<version>_<goos>_<goarch>.{tar.gz,zip} plus a checksums file (see
// .github/workflows/release.yml). Update downloads the archive matching the
// running platform, verifies its SHA-256 against that checksums file, extracts
// the binary, and atomically swaps it in place of the running executable.
//
// Everything that touches the network goes through Client so tests can point at
// a local server; the version math, checksum verification, and archive
// extraction are pure functions.
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
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo coordinates for the published releases. Kept here so both the updater and
// the notice point at the same place.
const (
	repoOwner = "p-arndt"
	repoName  = "hop"
)

// maxAsset caps how many bytes we accept from a release asset — both the
// downloaded archive and the binary extracted from it — so a hostile or corrupt
// response can't exhaust memory. Exceeding it is always an error, never a silent
// truncation: installing a truncated binary would brick the user's hop.
// Release binaries are a few MB. A var only so tests can shrink it.
var maxAsset = int64(64 << 20) // 64 MiB

// maxChecksums caps the checksums file, which is a few hundred bytes in a
// genuine release — there is no reason to buffer megabytes of "checksums" a
// hostile release asset serves up.
const maxChecksums = int64(1 << 20) // 1 MiB

// Client talks to the GitHub Releases API. The zero value is not usable; use
// NewClient. APIBase and HTTP are overridable in tests.
type Client struct {
	HTTP    *http.Client
	APIBase string // e.g. "https://api.github.com"
	Owner   string
	Repo    string
}

// NewClient returns a Client pointed at github.com with the given HTTP client
// (nil gets a fresh client with a sane timeout — never http.DefaultClient, which
// has no timeout and is a shared global we must not mutate). The client is given
// a redirect policy that keeps every hop on https: the checksum only vouches for
// the download if the whole chain is authenticated, because the checksums file
// travels over the same channel an attacker would use to swap the archive.
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	if hc.CheckRedirect == nil {
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return allowedURL(req.URL.String())
		}
	}
	return &Client{HTTP: hc, APIBase: "https://api.github.com", Owner: repoOwner, Repo: repoName}
}

// allowedURL rejects any download URL that isn't https to a GitHub-controlled
// host. The release metadata — including asset URLs — is input, not truth:
// following a plain-http URL (or a redirect hop onto one) would let an on-path
// attacker substitute both the archive and the checksums file that vouches for
// it, making verification meaningless. Plain http is tolerated only for
// loopback, so tests can run a local server. The error deliberately doesn't
// echo the URL: it's untrusted bytes that would otherwise land on the user's
// terminal.
func allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("release asset URL is unparsable")
	}
	switch {
	case u.Scheme == "https" && isGitHubHost(u.Hostname()):
		return nil
	case u.Scheme == "https":
		return fmt.Errorf("refusing to download a release asset from a non-GitHub host")
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("refusing to download a release asset over a non-https URL")
}

// isGitHubHost reports whether host is one GitHub serves releases from: the API,
// the release download path on github.com, and the *.githubusercontent.com CDN
// hosts those downloads redirect to. Matching is on whole labels — a lookalike
// like "evilgithubusercontent.com" or "github.com.evil.example" must not pass.
func isGitHubHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch host {
	case "github.com", "api.github.com":
		return true
	}
	return strings.HasSuffix(host, ".githubusercontent.com")
}

// isLoopback reports whether host is localhost or a loopback IP.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validVersion reports whether v is a plausible release version: bounded length,
// starts with a digit, and only semver characters. Everything else — including
// terminal escape sequences smuggled into a tag name — is rejected at ingress,
// because versions get printed to the user's terminal (and into a TUI that is
// itself drawing escape sequences), cached on disk, and interpolated into asset
// file names.
func validVersion(v string) bool {
	if len(v) == 0 || len(v) > 64 || v[0] < '0' || v[0] > '9' {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z',
			c == '.' || c == '-' || c == '+':
		default:
			return false
		}
	}
	return true
}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the slice of the GitHub release payload we care about.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// LatestRelease fetches the latest non-prerelease, non-draft release. GitHub's
// /releases/latest endpoint already excludes both.
func (c *Client) LatestRelease(ctx context.Context) (*Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(c.APIBase, "/"), c.Owner, c.Repo)
	if err := allowedURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hop-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// StatusCode, not Status: the status line is server-supplied text and
		// this message reaches the terminal.
		return nil, fmt.Errorf("github returned HTTP %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	// The tag is untrusted input that flows into terminal output, the on-disk
	// cache, and asset-name matching — reject anything that isn't a clean
	// version before it goes anywhere.
	if !validVersion(strings.TrimPrefix(rel.Tag, "v")) {
		return nil, fmt.Errorf("release tag is not a plausible version — refusing it")
	}
	return &rel, nil
}

// download fetches an asset into memory. It refuses URLs that fail allowedURL
// (the asset URL comes from untrusted release metadata) and errors — rather
// than silently truncating — when the body exceeds limit. what names the asset
// in errors, since the URL itself is untrusted bytes we won't echo to the
// terminal.
func (c *Client) download(ctx context.Context, url, what string, limit int64) ([]byte, error) {
	if err := allowedURL(url); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hop-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", what, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds its %d-byte size limit", what, limit)
	}
	return data, nil
}

// BinaryName is the executable's file name inside the archive for a given OS.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "hop.exe"
	}
	return "hop"
}

// ArchiveName is the release archive file name for a platform and version,
// matching what the release workflow produces.
func ArchiveName(version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("hop_%s_%s_%s.%s", version, goos, goarch, ext)
}

// ChecksumsName is the checksums file name for a version.
func ChecksumsName(version string) string {
	return fmt.Sprintf("hop_%s_checksums.txt", version)
}

// findAsset returns the asset with the given name, or an error naming what was
// looked for (helps diagnose a platform the release didn't build for).
func findAsset(assets []Asset, name string) (Asset, error) {
	for _, a := range assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release has no asset %q (this platform may not be published)", name)
}

// verifyChecksum confirms archive's SHA-256 appears against archiveName in a
// sha256sum-format checksums file ("<hex>  <name>" per line).
func verifyChecksum(archive []byte, archiveName string, checksums []byte) error {
	sum := sha256.Sum256(archive)
	want := hex.EncodeToString(sum[:])
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		// Only well-formed lines (64 hex chars + name) can match at all. This is
		// also what keeps the mismatch message below safe to print: the checksums
		// file is untrusted, and a non-hex "hash" could smuggle terminal escapes
		// into the error output.
		if len(fields) != 2 || !isHex64(fields[0]) {
			continue
		}
		// sha256sum prefixes the name with "*" in binary mode; tolerate it.
		if strings.TrimPrefix(fields[1], "*") == archiveName {
			if strings.EqualFold(fields[0], want) {
				return nil
			}
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", archiveName, want, fields[0])
		}
	}
	return fmt.Errorf("no checksum listed for %s", archiveName)
}

// isHex64 reports whether s is exactly 64 hexadecimal characters (a SHA-256).
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// extractBinary pulls the named binary out of a release archive. isZip selects
// zip (Windows) vs tar.gz (everything else).
func extractBinary(archive []byte, binName string, isZip bool) ([]byte, error) {
	if isZip {
		return extractFromZip(archive, binName)
	}
	return extractFromTarGz(archive, binName)
}

func extractFromZip(archive []byte, binName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("reading zip: %w", err)
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != binName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return readAllCapped(rc)
	}
	return nil, fmt.Errorf("%s not found in archive", binName)
}

func extractFromTarGz(archive []byte, binName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if path.Base(hdr.Name) == binName {
			return readAllCapped(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binName)
}

// readAllCapped reads r up to maxAsset bytes and errors if there is more. The
// checksum covers the compressed archive, not what it inflates to — so a binary
// that decompresses past the cap must fail here rather than be silently cut off
// and installed as a corrupt executable over the user's working one.
func readAllCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAsset+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxAsset {
		return nil, fmt.Errorf("binary in archive exceeds the %d MiB limit — refusing a truncated install", maxAsset>>20)
	}
	return data, nil
}

// replaceExecutable atomically swaps the file at exePath for newBin.
//
// The new binary is written to a temp file in the same directory (so the final
// rename stays on one filesystem and is atomic). On Windows a running .exe
// cannot be deleted or overwritten, but it can be renamed, so the current file
// is moved aside to "<exe>.old" first; that leftover is cleaned up on the next
// run (see CleanupLeftovers). Windows is hop's first-class platform, so this
// path is the one that has to be right.
func replaceExecutable(exePath string, newBin []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".hop-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (%w) — reinstall hop or run with sufficient permissions", dir, err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any failure before the final rename succeeds.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		old := exePath + ".old"
		os.Remove(old) // a stale one from a previous update would block the rename
		if err := os.Rename(exePath, old); err != nil {
			return fmt.Errorf("cannot move current binary aside (%w) — is another hop running?", err)
		}
		if err := os.Rename(tmpName, exePath); err != nil {
			// Put the original back so the user isn't left without a binary.
			os.Rename(old, exePath)
			return err
		}
	} else if err := os.Rename(tmpName, exePath); err != nil {
		return fmt.Errorf("cannot replace %s (%w) — reinstall hop or run with sufficient permissions", exePath, err)
	}

	success = true
	return nil
}

// CleanupLeftovers best-effort removes the "<exe>.old" file left by a prior
// Windows self-update. Called once at startup; a no-op if there's nothing to do
// or the file is still locked.
func CleanupLeftovers() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	os.Remove(exe + ".old")
}

// executablePath resolves the running binary to a real, symlink-free path so the
// swap targets the actual file rather than a symlink pointing at it. It's a var
// so tests can point the swap at a throwaway file.
var executablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Result reports the outcome of an update attempt.
type Result struct {
	Current string // version before the attempt
	Latest  string // latest available version
	Updated bool   // whether the binary was replaced
	ExePath string // the binary that was (or would be) replaced
}

// SelfUpdate checks for a newer release and, unless checkOnly, downloads,
// verifies, and installs it in place of the running binary. current is the
// running version (without a leading "v"). It refuses to act on "dev"/source
// builds, which have no release to compare against.
func (c *Client) SelfUpdate(ctx context.Context, current string, checkOnly bool) (*Result, error) {
	if isDevVersion(current) {
		return nil, fmt.Errorf("this is a %q build, not an installed release — build from source or download a release binary to get updates", current)
	}

	rel, err := c.LatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	res := &Result{Current: current, Latest: latest}

	if !IsNewer(latest, current) {
		return res, nil
	}
	if checkOnly {
		return res, nil
	}

	exe, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the running binary: %w", err)
	}
	res.ExePath = exe

	goos, goarch := runtime.GOOS, runtime.GOARCH
	archiveName := ArchiveName(latest, goos, goarch)
	archiveAsset, err := findAsset(rel.Assets, archiveName)
	if err != nil {
		return nil, err
	}
	sumsAsset, err := findAsset(rel.Assets, ChecksumsName(latest))
	if err != nil {
		return nil, err
	}

	archive, err := c.download(ctx, archiveAsset.URL, "release archive", maxAsset)
	if err != nil {
		return nil, err
	}
	sums, err := c.download(ctx, sumsAsset.URL, "checksums file", maxChecksums)
	if err != nil {
		return nil, err
	}
	if err := verifyChecksum(archive, archiveName, sums); err != nil {
		return nil, err
	}

	bin, err := extractBinary(archive, BinaryName(goos), goos == "windows")
	if err != nil {
		return nil, err
	}
	if len(bin) == 0 {
		return nil, fmt.Errorf("extracted binary is empty")
	}
	if err := replaceExecutable(exe, bin); err != nil {
		return nil, err
	}

	res.Updated = true
	return res, nil
}

// isDevVersion reports whether v is a non-release build (the buildinfo default,
// or a bare "go run"/"go install" with no injected version).
func isDevVersion(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "dev" || v == "(devel)"
}

// IsNewer reports whether latest is a strictly newer version than current.
func IsNewer(latest, current string) bool {
	if isDevVersion(current) {
		return false
	}
	return CompareVersions(latest, current) > 0
}

// CompareVersions compares two semver-ish versions, returning -1, 0, or 1.
// A leading "v" and build metadata ("+...") are ignored. A version with a
// pre-release suffix ("-pre.1") sorts below the same version without one.
func CompareVersions(a, b string) int {
	a = normalizeVersion(a)
	b = normalizeVersion(b)
	aCore, aPre := splitPrerelease(a)
	bCore, bPre := splitPrerelease(b)

	if c := compareCore(aCore, bCore); c != 0 {
		return c
	}
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "": // release > pre-release
		return 1
	case bPre == "":
		return -1
	default:
		return comparePrerelease(aPre, bPre)
	}
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 { // drop build metadata
		v = v[:i]
	}
	return v
}

func splitPrerelease(v string) (core, pre string) {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

// compareCore compares dot-separated numeric cores (MAJOR.MINOR.PATCH), missing
// fields treated as 0.
func compareCore(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := max(len(as), len(bs))
	for i := 0; i < n; i++ {
		if c := compareNumericField(field(as, i), field(bs, i)); c != 0 {
			return c
		}
	}
	return 0
}

func field(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return "0"
}

func compareNumericField(a, b string) int {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

// comparePrerelease applies semver precedence to dot-separated pre-release
// identifiers: numeric compared as numbers, numeric below alphanumeric, and a
// shorter set below a longer one when the shared prefix is equal.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := min(len(as), len(bs))
	for i := 0; i < n; i++ {
		ai, aErr := strconv.Atoi(as[i])
		bi, bErr := strconv.Atoi(bs[i])
		aNum, bNum := aErr == nil, bErr == nil
		switch {
		case aNum && bNum:
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		case aNum: // numeric identifiers have lower precedence than alphanumeric
			return -1
		case bNum:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}
