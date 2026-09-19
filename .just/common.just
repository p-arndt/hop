# common.just — shared by every project. Synced from github.com/p-arndt/just-common;
# edit it there, then run `just sync-common` in each project.
#
# Import this first, then the stack modules the project needs:
#   import '.just/common.just'
#   import '.just/go.just'

# Windows has no `sh` (just's default shell), so recipes run through PowerShell
# there. Recipe bodies are kept to plain command calls that parse the same way in
# both shells; the few that need real shell logic come as [unix]/[windows] pairs.
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# Where `sync-common` gets the modules: GitHub by default, so it works on any
# machine without a checkout. Set JUST_COMMON to a local just-common checkout to
# test unpublished changes, or JUST_COMMON_REF to pin a branch or tag.
JUST_COMMON := env("JUST_COMMON", "")
_JUST_COMMON_URL := "https://raw.githubusercontent.com/p-arndt/just-common/" + env("JUST_COMMON_REF", "main")

# Only modules the project already has are updated, so a project opts in by
# copying a file once.
[unix]
[doc('Refresh the modules in .just/ from just-common (GitHub, or $JUST_COMMON).')]
sync-common:
    @for f in .just/*.just; do n="$(basename "$f")"; if [ -n "{{JUST_COMMON}}" ]; then cp "{{JUST_COMMON}}/$n" "$f.tmp"; else curl -fsSL "{{_JUST_COMMON_URL}}/$n" -o "$f.tmp"; fi && mv "$f.tmp" "$f" && echo "synced $f" || { rm -f "$f.tmp"; echo "failed $f" >&2; exit 1; }; done

[windows]
[doc('Refresh the modules in .just/ from just-common (GitHub, or $env:JUST_COMMON).')]
sync-common:
    @$ErrorActionPreference = "Stop"; Get-ChildItem .just -Filter *.just | ForEach-Object { $tmp = "$($_.FullName).tmp"; if ("{{JUST_COMMON}}") { Copy-Item (Join-Path "{{JUST_COMMON}}" $_.Name) $tmp -Force } else { Invoke-WebRequest -UseBasicParsing -Uri "{{_JUST_COMMON_URL}}/$($_.Name)" -OutFile $tmp }; Move-Item $tmp $_.FullName -Force; "synced .just/$($_.Name)" }
