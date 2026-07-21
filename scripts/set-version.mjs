// Stamp / bump hop's version.
//
//   node scripts/set-version.mjs 0.2.0     # set an explicit version
//   node scripts/set-version.mjs patch     # bump 0.1.3 -> 0.1.4
//   node scripts/set-version.mjs minor     # bump 0.1.3 -> 0.2.0
//   node scripts/set-version.mjs major     # bump 0.1.3 -> 1.0.0
//
// The repo-root VERSION file is the single source of truth. It's injected into
// the binary at build time via -ldflags (see .github/workflows/release.yml and
// internal/buildinfo), so no Go source needs editing — only VERSION is stamped.
//
// Also exports readVersion / bumpVersion / resolveVersion / setVersion for
// scripts/release.mjs.

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

// Repo root is one level up from this script's scripts/ directory.
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const versionFile = join(root, "VERSION");

/** Read the current version (e.g. "0.1.0") from the VERSION file. */
export function readVersion() {
  return readFileSync(versionFile, "utf8").trim();
}

/** Bump a semver string by "patch" | "minor" | "major". */
export function bumpVersion(current, kind) {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(current);
  if (!m) throw new Error(`current version is not plain semver: ${current}`);
  let [major, minor, patch] = m.slice(1).map(Number);
  if (kind === "major") [major, minor, patch] = [major + 1, 0, 0];
  else if (kind === "minor") [minor, patch] = [minor + 1, 0];
  else if (kind === "patch") patch++;
  else throw new Error(`unknown bump "${kind}" (use patch|minor|major)`);
  return `${major}.${minor}.${patch}`;
}

/** Resolve a CLI argument to a concrete version: a bump keyword or explicit x.y.z. */
export function resolveVersion(arg) {
  return ["patch", "minor", "major"].includes(arg)
    ? bumpVersion(readVersion(), arg)
    : arg;
}

/** Write `version` into the VERSION file (no trailing newline). */
export function setVersion(version) {
  if (!/^\d+\.\d+\.\d+/.test(version))
    throw new Error(`invalid version "${version}" (expected x.y.z)`);
  writeFileSync(versionFile, version);
  console.log(`Stamped version ${version}.`);
}

// CLI entry point (only when run directly, not when imported).
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const arg = process.argv[2];
  if (!arg) {
    console.error("usage: node scripts/set-version.mjs <patch|minor|major|x.y.z>");
    process.exit(1);
  }
  setVersion(resolveVersion(arg));
}
