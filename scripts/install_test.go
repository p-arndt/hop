// Package scripts holds the repo's build/install scripts. It has no Go code —
// only tests that keep the scripts in step with the release workflow, which is
// what silently breaks them: a renamed archive is only noticed by a user.
package scripts

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func contains(t *testing.T, path, haystack, needle, why string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s does not contain %q (%s)", path, needle, why)
	}
}

// The installers download by name, so they have to agree with the workflow that
// uploads those names: hop_<version>_<os>_<arch>.{zip,tar.gz} plus one checksums file.
func TestInstallersMatchReleaseAssetNames(t *testing.T) {
	wf := read(t, "../.github/workflows/release.yml")
	contains(t, "release.yml", wf, `name="hop_${VERSION}_${GOOS}_${GOARCH}"`, "archive name the installers rebuild")
	contains(t, "release.yml", wf, `hop_${VERSION}_checksums.txt`, "checksums name the installers rebuild")
	contains(t, "release.yml", wf, `zip -q "../dist/$name.zip"`, "windows archives are zips")
	contains(t, "release.yml", wf, `tar -czf "dist/$name.tar.gz"`, "unix archives are tarballs")

	sh := read(t, "install.sh")
	contains(t, "install.sh", sh, `archive="hop_${VERSION}_${os}_${arch}.tar.gz"`, "must match the workflow's tarball name")
	contains(t, "install.sh", sh, `hop_${VERSION}_checksums.txt`, "must match the workflow's checksums name")

	ps := read(t, "install.ps1")
	contains(t, "install.ps1", ps, `$archive = "hop_${ver}_windows_${arch}.zip"`, "must match the workflow's zip name")
	contains(t, "install.ps1", ps, `hop_${ver}_checksums.txt`, "must match the workflow's checksums name")
}

// The release lives under one owner/repo, and internal/update already hard-codes it.
func TestInstallersPointAtTheReleaseRepo(t *testing.T) {
	up := read(t, "../internal/update/update.go")
	if !strings.Contains(up, `cfg.Owner = "p-arndt"`) || !strings.Contains(up, `cfg.Repo = "hop"`) {
		t.Fatal("internal/update no longer names p-arndt/hop; the installers need the same change")
	}
	contains(t, "install.sh", read(t, "install.sh"), `REPO="p-arndt/hop"`, "same repo as internal/update")
	contains(t, "install.ps1", read(t, "install.ps1"), `$repo = 'p-arndt/hop'`, "same repo as internal/update")
}

// `just install` is the from-source entry point on both platforms.
func TestJustInstallRunsTheScripts(t *testing.T) {
	jf := read(t, "../justfile")
	contains(t, "justfile", jf, "sh scripts/install.sh --from-source", "unix install recipe")
	contains(t, "justfile", jf, "pwsh.exe -NoLogo -NoProfile -File scripts/install.ps1 -FromSource", "windows install recipe")
}

// A parse error in a shipped installer is the one bug a Go test can catch cheaply.
func TestInstallShellScriptParses(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	if out, err := exec.Command(sh, "-n", "install.sh").CombinedOutput(); err != nil {
		t.Fatalf("sh -n install.sh: %v\n%s", err, out)
	}
}

func TestInstallPowerShellScriptParses(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("no pwsh on PATH")
	}
	const script = `$errs = $null
[System.Management.Automation.Language.Parser]::ParseFile('install.ps1', [ref]$null, [ref]$errs) > $null
if ($errs) { $errs | ForEach-Object { $_.ToString() }; exit 1 }`
	if out, err := exec.Command(pwsh, "-NoLogo", "-NoProfile", "-Command", script).CombinedOutput(); err != nil {
		t.Fatalf("parse install.ps1: %v\n%s", err, out)
	}
}
