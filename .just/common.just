# common.just — shared by every project. Synced from ~/coding/just-common;
# edit it there, then run `just sync-common` in each project.
#
# Import this first, then the stack modules the project needs:
#   import '.just/common.just'
#   import '.just/go.just'

# Windows has no `sh` (just's default shell), so recipes run through PowerShell
# there. Recipe bodies are kept to plain command calls that parse the same way in
# both shells; the few that need real shell logic come as [unix]/[windows] pairs.
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# Where `sync-common` copies the modules from.
JUST_COMMON := env("JUST_COMMON", home_directory() / "coding" / "just-common")

# Default: show the recipe list. [default] is needed because just otherwise runs
# the first recipe of the importing justfile, not of this file.
[default]
default:
    @just --list

# Refresh every module in .just/ from the just-common checkout. Only modules the
# project already has are updated, so a project opts in by copying a file once.
[unix]
sync-common:
    @for f in .just/*.just; do cp "{{JUST_COMMON}}/$(basename "$f")" "$f" && echo "synced $f"; done

[windows]
sync-common:
    @Get-ChildItem .just -Filter *.just | ForEach-Object { Copy-Item (Join-Path "{{JUST_COMMON}}" $_.Name) $_.FullName -Force; "synced .just/$($_.Name)" }
