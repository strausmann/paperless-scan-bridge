# Core rules (binding)

Auto-loaded for every (human or AI) contributor. These complement `AGENTS.md` and the architectural
principles in `CLAUDE.md` — they do not override the container-first / three-image constraints.

## R0 — Error culture (top rule)
On any mistake (wrong assumption, broken command, wrong conclusion, regression): **analyze the root
cause → document it in `docs/learnings/lessons-learned.md` → add a concrete guard** (rule / ADR /
test / hook). Name mistakes openly; never hide or silently fix them. Details: `error-handling.md`.

## R1 — Verify before concluding; public only when proven
Complete the full verification **before** stating a conclusion — and especially before posting
anything public (GitHub issues/PRs/comments, upstream reports). Check the actual evidence (releases,
versions, dates, code/artifacts), not inference. Don't present a half-verified conclusion and rely on
the reviewer to catch gaps.

## R2 — No AI attribution in version control
Never add `Co-Authored-By`, `Claude-Session`, "Generated with …", or any AI self-attribution to
commits, pushes, PRs, or issue/PR bodies. Write plain, factual messages.

## R3 — ADRs are top authority
Precedence on conflict: **ADR (`docs/decisions/`) > guidelines/`AGENTS.md` > code/README**. Decisions
are amended by superseding, never by editing. Details: `adr.md`.

## R4 — Issue-driven
Larger changes start as a GitHub issue (CONTRIBUTING). One issue → one branch → one PR.

## R5 — Conventional Commits
`type(scope): subject`; scope mandatory, from `.github/SCOPES.md`. Versioning is automated
(release-only semantic-release); the **manual `CHANGELOG.md` (Keep a Changelog) stays hand-curated**.

## R6 — Resolve every AI review finding
Each Gemini/Copilot PR finding is **fixed** or **replied to as a false positive with reasoning** —
never silently ignored. Details: `pr-review.md`.
