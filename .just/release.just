# release.just — versioning and releases through stamp. Synced from ~/coding/just-common.
#
# Needs `stamp` on PATH (https://github.com/p-arndt/stamp). stamp owns the
# version files, the release commit and the tag; never edit them by hand.
# Pushing the tag triggers the release workflow.

# Print the current version.
version:
    @stamp current

# A correction tool; use `just release` to cut a release.
[doc('Write a version without committing (patch|minor|major|x.y.z).')]
set-version BUMP="patch":
    stamp set {{BUMP}}

# Cut a release: bump, commit, tag, push.  just release  |  just release minor
release BUMP="patch":
    stamp release {{BUMP}}

# Show what `just release` would do without changing anything.
release-dry BUMP="patch":
    stamp release {{BUMP}} --dry-run

# Cut a pre-release (beta/rc).  just prerelease  |  just prerelease minor
prerelease BUMP="patch":
    stamp prerelease {{BUMP}}

# Record a user-facing change for the changelog:  just note fixed "crash on empty config"
note KIND +TEXT:
    stamp note {{KIND}} "{{TEXT}}"

# Print the changes noted since the last release.
changelog:
    stamp changelog
