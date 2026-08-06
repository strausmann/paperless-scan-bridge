# 0012 — Release-only semantic-release; manual Keep-a-Changelog retained

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** ci, release

## Context
We want automated, traceable SemVer + GitHub Releases from Conventional Commits, **without** giving up
the existing hand-curated Keep-a-Changelog `CHANGELOG.md`.

## Decision
We will use **release-only semantic-release**: compute SemVer + create the git tag + GitHub Release
from Conventional Commits, with **no changelog/git plugins** — `CHANGELOG.md` stays hand-curated.
Commits are enforced by commitlint. Releases run **weekly (Sunday) or manual dispatch**, never on
push. The project is **0.x / alpha** until a deliberate stable cut.

## Options considered
- **Option A — release-only (chosen):** automation + keeps the manual Keep-a-Changelog (issue #10).
- **Option B — full semantic-release auto-changelog:** would replace the manual changelog; rejected.
- **Option C — fully manual versioning:** no automation; rejected.

## Consequences
- GitHub Release notes are auto-generated (with emoji sections, ADR 0001); `CHANGELOG.md` is manual.
- First cut is `0.1.0` (seed `v0.0.0`); 1.0.0 only when deliberately declared.

## References
- issue #10 (option a chosen); `.releaserc.json`, `.github/workflows/release.yml`,
  `commitlint.config.cjs`; PR #11.
