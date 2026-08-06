# 0001 — Emoji section headers in changelog & release notes

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** release, docs

## Context
We want consistent, scannable **icons** in changelog/release-note sections across all projects (a
confirmed preference). This repo keeps a **manual Keep-a-Changelog** `CHANGELOG.md` and uses
**release-only** semantic-release (it does not generate the changelog).

## Decision
We will use a fixed emoji set for: (1) the **GitHub release-note** section headers (via the
conventionalcommits `presetConfig.types` in `@semantic-release/release-notes-generator`), and (2) the
**manual Keep-a-Changelog category headers** in `CHANGELOG.md`.

## Options considered
- **Option A — emoji in release-notes presetConfig + manual category headers (chosen):** consistent
  icons in both surfaces; keeps the manual changelog hand-curated; no commit-convention change.
- **Option B — gitmoji-style commit prefixes:** rejected (changes Conventional Commits).
- **Option C — no icons:** rejected (explicit preference).

## Consequences
- Release-note sections: `feat` ✨ · `fix` 🐛 · `perf` ⚡ · `refactor` ♻️ · `docs` 📝 · `build`/`ci`
  🔧 · `revert` ⏪; breaking via the preset's ⚠ section.
- Manual `CHANGELOG.md` categories: ✨ Added · ♻️ Changed · ⚠️ Deprecated · 🗑️ Removed · 🐛 Fixed ·
  🔒 Security · 🔧 Compatibility · 📝 Documentation.
- `CHANGELOG.md` remains hand-curated; semantic-release only adds emoji to **GitHub Release notes**,
  not to the file (consistent with the release-only decision).
- Already-published entries are not rewritten.

## References
- `.releaserc.json` (release-notes-generator `presetConfig.types`), `CHANGELOG.md` (categories).
- Consistent with BambuBridge ADR 0001; confirmed in chat 2026-06-28.
