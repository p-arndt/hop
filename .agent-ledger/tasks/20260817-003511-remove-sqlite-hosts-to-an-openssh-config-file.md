---
id: "20260817-003511-remove-sqlite-hosts-to-an-openssh-config-file"
title: "Remove SQLite: hosts to an OpenSSH config file"
status: "completed"
updated: "2026-08-17T00:36:03+02:00"
base_commit: "8c58950e348b5427b937e7ec7716adbf95cabc08"
branch: "main"
agent: null
tags: ["binary-size", "dependencies", "migration", "store"]
files:
  - "README.md"
  - "TODO.md"
  - "docs/07-files.md"
  - "docs/_readme.md"
  - "go.mod"
  - "go.sum"
  - "index.html"
  - "internal/config/config.go"
  - "internal/config/config_test.go"
  - "internal/store/meta.go"
  - "internal/store/migrate.go"
  - "internal/store/sqlitefile.go"
  - "internal/store/sshconfig.go"
  - "internal/store/sshconfig_test.go"
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/store/testdata/legacy-hop-v1.db"
  - "internal/store/testdata/legacy-hop.db"
  - "internal/tui/hostmgmt_test.go"
  - "internal/tui/model.go"
  - "internal/tui/shells_test.go"
  - "tools/demoserver/demoserver_test.go"
  - "tools/demoserver/main.go"
---

# Remove SQLite: hosts to an OpenSSH config file

## Goal

Drop the modernc.org/sqlite dependency by storing hosts as a real OpenSSH config file that ssh/scp can read, with hop-only metadata in config.json, migrating existing hop.db installs once.

## Scope

- ImportSSHConfig still overwrites proxy_command/proxy_jump/default_dir from the imported config, as it did under SQLite. The caveat recorded in the ProxyCommand/ProxyJump entry is unchanged, not fixed here.

## Discoveries

- The 71 non-test call sites of the store all go through *Store methods and the Host/Forward types, never SQL. Swapping the backend while keeping the exported API identical kept the change inside internal/store; only OpenAt's signature changed (it now takes hostsPath and metaPath).

- Measured dependency cost before committing to a design: an empty Go binary is 1.57 MB, crypto/ssh+agent+knownhosts+ssh_config adds 1.20 MB, and the whole TUI stack (x/vt + bubbletea + lipgloss) adds only 0.41 MB. SQLite plus sftp was 3.6 MB, so almost all of hop's removable binary weight was the store, not the UI.

- hop only ever writes one line to a file it does not own: a prepended Include in ~/.ssh/config. It must be prepended, not appended, because OpenSSH takes the first value it finds for most keywords, so an Include at the bottom is shadowed by any earlier Host block.

## Decisions

- **Decision:** Migrate legacy hop.db with a hand-written, read-only SQLite file-format reader (internal/store/sqlitefile.go) instead of keeping the driver for one release.
  - **Reason:** Importing modernc.org/sqlite purely to migrate would have given back the entire 3 MB the change exists to save. The format is frozen and the needed subset is small: table b-trees, varints, serial types, overflow chains — no indexes, no WAL replay, no writing.
  - **Trade-off:** ~520 LOC of format code hop now owns. Mitigated by being strictly read-only, refusing rather than guessing on anything unrecognised, and keeping the original database as hop.db.bak.

- **Decision:** Split a host across two files: OpenSSH directives into ~/.ssh/hop.config, hop-only fields under the 'hosts' key of config.json.
  - **Reason:** Everything OpenSSH has a keyword for becomes usable by ssh/scp/rsync for free; tags, groups, pins and frecency have no OpenSSH spelling and would corrupt the config if invented. Putting them in config.json rather than a third file keeps them beside the other hop preferences they resemble.
  - **Trade-off:** config.json gains a second writer, so both sides must merge on save.

- **Decision:** hop manages its own config file and adds an Include to ~/.ssh/config, rather than writing Host blocks into ~/.ssh/config directly.
  - **Reason:** hop rewrites its hosts file wholesale on every edit. Doing that to the user's own config would destroy comments, ordering and Match blocks that hop cannot round-trip.
  - **Trade-off:** Two files instead of one, and the Include must land at the top to not be shadowed.

## Failures

- **Approach:** Probing dependency sizes with blank imports only.
  - **Command:** `go build -trimpath -ldflags='-s -w'`
  - **Evidence:** Blank-import probe reported a 3.18 MB floor; a probe that actually calls into the same packages reported 4.4 MB.
  - **Lesson:** The linker dead-code-eliminates unreferenced packages, so blank-import size probes measure a floor nobody ships. Reference the symbols to get a number that means anything.

- **Approach:** Checking the SQLite header's WAL/format version by reading a uint32 at offset 18 and shifting.
  - **Command:** `go test ./internal/store/ -run TestReader`
  - **Evidence:** openSQLite: unsupported file format version — on a perfectly healthy database
  - **Lesson:** Offsets 18 and 19 are two separate single-byte fields (write format, read format), not a u32. Pending WAL/journal data is better detected via the -wal and -journal sidecar files than from header bytes.

## Validation

- go build ./... && go vet ./... && go test ./... — all packages pass

- The SQLite reader was verified against the real driver before the driver was removed: 300 hosts (forcing interior b-tree pages) and a 9000-byte value (forcing overflow pages) written by modernc.org/sqlite, read back field-for-field.

- End-to-end migration against a fake HOME: legacy hop.db converted, 5 hosts listed by 'hop list', Include prepended above the user's own blocks, hop.db.bak kept, metadata in config.json.

- Real OpenSSH parses the generated file: 'ssh -G' resolves hostname/user/port, both LocalForward and RemoteForward, ProxyJump, and a >9000-char ProxyCommand.

## Remaining risks

- config.json now has two independent writers: internal/config (settings UI) and internal/store (host metadata under the 'hosts' key). Both must read-modify-write the JSON object; a plain struct marshal on either side silently drops the other's keys.

- Migration is one-way and untested against databases hop never wrote (hand-edited schemas, WAL-mode files). Those refuse rather than half-import, and hop.db is kept, but such a user is blocked until they downgrade.

- The store rewrites both files on every mutation, including Touch on each connect. Fine at hop's host counts; it is O(hosts) writes per connect, not O(1).

- Hosts hand-written into ~/.ssh/hop.config are picked up, but hop rewrites that file on the next edit and drops any comments in it.

## Handoff

- internal/store/sqlitefile.go exists only for the migration and can be deleted once installs have upgraded — along with the two testdata fixtures and the tests that read them.

- The fixtures in internal/store/testdata were generated by a throwaway program using the old schema and driver; regenerating them needs modernc.org/sqlite temporarily re-added.
