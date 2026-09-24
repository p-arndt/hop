# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.13.0 - 2026-09-24

### Added

- Copy out of vim with shift+drag
- Go to anything open on any host with ctrl+o space; every host reopens where you left off
- New ctrl+o keys: space go to, tab last host, ←/→ switch host, f files, j terminal panel, t tree, b sidebar
- Terminal panel under your files, and a preview of the file under the cursor

### Changed

- ctrl+b now reaches the remote (e.g. tmux); hide the sidebar with ctrl+o b
- ctrl+o is the leader everywhere, including the file browser; leave with esc esc
- New layout: one sidebar with your hosts, what's open on them and the file tree. esc esc to jump in, enter to go

### Fixed

- A rebound leader key now works everywhere
- esc esc no longer quits hop
- Previewing a link to a pipe no longer freezes hop

### Security

- Directory names with control characters are never typed into a shell
- Remote file and directory names can't send escape sequences to your terminal
