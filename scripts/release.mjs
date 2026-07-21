// Cut a release: resolve the version, stamp VERSION, commit (if it changed),
// tag, and push. Pushing the tag triggers the "Build and Publish Release" Action.
//
//   node scripts/release.mjs           # patch bump (default)
//   node scripts/release.mjs minor
//   node scripts/release.mjs major
//   node scripts/release.mjs 1.4.0     # explicit version
//
// Safety: refuses to run on a dirty working tree (so the release commit holds
// only the version bump) and refuses to clobber an existing tag.

import { execSync } from "node:child_process";
import { readVersion, resolveVersion, setVersion } from "./set-version.mjs";

const git = (args, opts = {}) =>
  execSync(`git ${args}`, { encoding: "utf8", ...opts }).trim();

function fail(msg) {
  console.error(`error: ${msg}`);
  process.exit(1);
}

const bump = process.argv[2] ?? "patch";

// 1. Clean tree — the release commit must contain only the version bump.
if (git("status --porcelain")) {
  fail("working tree is not clean — commit or stash your changes first.");
}

const current = readVersion();
const next = resolveVersion(bump);
const tag = `v${next}`;

// 2. Don't reuse an existing tag.
if (git("tag --list").split(/\r?\n/).includes(tag)) {
  fail(`tag ${tag} already exists.`);
}

console.log(`Releasing ${tag}  (${current} -> ${next})\n`);

// 3. Stamp the VERSION file.
setVersion(next);

// 4. Commit the bump — unless VERSION is already at the target (e.g. the very
//    first release, where the version is already in the file), in which case
//    there's nothing to commit and we simply tag the current HEAD.
if (git("status --porcelain VERSION")) {
  git("add VERSION");
  git(`commit -m "release: ${tag}"`);
} else {
  console.log(`VERSION already at ${next} — tagging the current commit.`);
}

// 5. Annotated tag on HEAD.
git(`tag -a ${tag} -m ${tag}`);

// 6. Push the current branch together with the new tag.
const branch = git("rev-parse --abbrev-ref HEAD");
console.log(`\nPushing ${branch} + ${tag} ...`);
execSync(`git push origin ${branch} --follow-tags`, { stdio: "inherit" });

console.log(
  `\nDone. ${tag} pushed — the "Build and Publish Release" workflow is now running.`,
);
