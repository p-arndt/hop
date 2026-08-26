<#
.SYNOPSIS
    hop installer — Windows.

.DESCRIPTION
    Downloads the release archive for this machine, verifies its checksum,
    installs hop.exe and puts the install directory on the user PATH.

.EXAMPLE
    irm https://raw.githubusercontent.com/p-arndt/hop/main/scripts/install.ps1 | iex

.EXAMPLE
    .\scripts\install.ps1 -Version 0.11.0 -Dir C:\tools\bin

.EXAMPLE
    .\scripts\install.ps1 -FromSource    # build this checkout instead
#>
[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$Dir = $(if ($env:HOP_INSTALL_DIR) { $env:HOP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\hop' }),
    [switch]$FromSource,
    [switch]$NoModifyPath
)

$ErrorActionPreference = 'Stop'
$repo = 'p-arndt/hop'

function Die([string]$msg) { Write-Error "install.ps1: $msg"; exit 1 }

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("hop-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    $staged = Join-Path $tmp 'hop.exe'

    if ($FromSource) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            Die '-FromSource needs Go on the PATH (https://go.dev/dl/)'
        }
        # $PSScriptRoot is empty when the script is piped into iex, which is
        # exactly the case where there is no checkout to build.
        if (-not $PSScriptRoot) { Die '-FromSource must run from a hop checkout, not from a piped script' }
        $root = Split-Path -Parent $PSScriptRoot
        if (-not (Test-Path (Join-Path $root 'go.mod'))) { Die '-FromSource must run from a hop checkout' }

        $ver = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
        $commit = (& git -C $root rev-parse --short HEAD 2>$null)
        if (-not $commit) { $commit = 'none' }
        $date = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        $ldflags = "-s -w -X hop/internal/buildinfo.Version=$ver -X hop/internal/buildinfo.Commit=$commit -X hop/internal/buildinfo.Date=$date"

        Write-Host "building hop $ver from $root"
        $env:CGO_ENABLED = '0'
        & go build -C $root -trimpath -ldflags $ldflags -o $staged .
        if ($LASTEXITCODE -ne 0) { Die 'go build failed' }
    }
    else {
        $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
            'AMD64' { 'amd64' }
            'ARM64' { 'arm64' }
            default { Die "unsupported architecture: $env:PROCESSOR_ARCHITECTURE — build from source with -FromSource" }
        }

        $ver = $Version -replace '^v', ''
        if ($ver -eq 'latest') {
            $latest = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
            $ver = $latest.tag_name -replace '^v', ''
            if (-not $ver) { Die 'could not resolve the latest release — pass -Version' }
        }

        $archive = "hop_${ver}_windows_${arch}.zip"
        $base = "https://github.com/$repo/releases/download/v$ver"
        $zip = Join-Path $tmp $archive

        Write-Host "downloading $archive"
        try { Invoke-WebRequest "$base/$archive" -OutFile $zip -UseBasicParsing }
        catch { Die "download failed: $base/$archive" }

        # A missing checksums file is a warning, not a failure — refusing to
        # install would be worse than an unverified install the user was told about.
        $sums = Join-Path $tmp 'checksums.txt'
        try {
            Invoke-WebRequest "$base/hop_${ver}_checksums.txt" -OutFile $sums -UseBasicParsing
            $want = (Select-String -Path $sums -Pattern "^([0-9a-f]{64})\s+\*?$([regex]::Escape($archive))$" |
                Select-Object -First 1).Matches.Groups[1].Value
            if (-not $want) { Die "$archive is not listed in the checksums file" }
            $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($got -ne $want) { Die "checksum mismatch for $archive (got $got, want $want)" }
            Write-Host 'checksum ok'
        }
        catch [System.Net.WebException], [Microsoft.PowerShell.Commands.HttpResponseException] {
            Write-Warning "no checksums file for v$ver — skipping verification"
        }

        Expand-Archive -Path $zip -DestinationPath $tmp -Force
        if (-not (Test-Path $staged)) { Die "$archive did not contain hop.exe" }
    }

    if (-not (Test-Path $Dir)) { New-Item -ItemType Directory -Path $Dir -Force | Out-Null }
    $target = Join-Path $Dir 'hop.exe'

    # A running .exe can be renamed but not overwritten — the same trick hop's
    # own self-update uses, and `hop` cleans the .old file up on next start.
    if (Test-Path $target) {
        $old = "$target.old"
        Remove-Item $old -Force -ErrorAction SilentlyContinue
        try { Move-Item $target $old -Force } catch { Die "hop.exe in $Dir is locked and could not be replaced" }
    }
    Move-Item $staged $target -Force

    Write-Host "installed $(& $target version) -> $target"

    if ($NoModifyPath) { return }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = ($userPath -split ';') | Where-Object { $_ }
    if ($entries -notcontains $Dir) {
        [Environment]::SetEnvironmentVariable('Path', (@($entries) + $Dir) -join ';', 'User')
        # The registry write only reaches new processes, so fix this one too.
        $env:Path = "$env:Path;$Dir"
        Write-Host "added $Dir to your user PATH — open a new terminal for it to take effect"
    }
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
