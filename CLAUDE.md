# General

- At the end of implementation run the `/repo-ledger` skill to update the ledger with the changes made in the repository.
- At the end of implementation tick the TODO.md checklist items that have been completed.
- Write tests for any new functionality added, and update existing tests if necessary.

# Domain model

The bounded contexts, and the words each one owns, live in [`domain/`](domain/README.md).

- **Before changing code**, read the context that owns it: `domain/contexts/<name>/README.md`
  and `language.md`. Use those words in code, tests, commits and PR text.
- **If the right word is missing, add it to `language.md` before you use it.** A term
  invented in code and never written down is how the language dies.
- **If a business rule you are implementing is not under "Business decisions"**, it is
  either new (write it) or someone else's (you are in the wrong context).
- **Check [`domain/glossary.md`](domain/glossary.md) before renaming anything that
  appears in more than one package.** `Session`, `Client`, `Pane`, `Action`, `Forward`
  and `Mode` mean different things in different contexts *on purpose*. Do not unify them.
- **After the change**, update the sections it touched and re-verify that context, in the
  same commit as the code.

| Code | Context |
|---|---|
| `internal/store`, `internal/config` | [fleet](domain/contexts/fleet/README.md) |
| `internal/sshx` | [connection](domain/contexts/connection/README.md) |
| `internal/terminal` | [pane](domain/contexts/pane/README.md) |
| `internal/filebrowser`, `internal/sftpx` | [files](domain/contexts/files/README.md) |
| `internal/keys` | [keyboard](domain/contexts/keyboard/README.md) |
| `internal/tui` | [workspace](domain/contexts/workspace/README.md) — but several files there belong to the contexts above; check the `code:` anchors |
