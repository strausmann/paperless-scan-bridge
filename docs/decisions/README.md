# Architecture Decision Records (ADRs)

Binding decisions for paperless-scan-bridge live here, one file per decision, in the
[Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions)
(Context → Decision → Consequences, plus "Options considered").

`AGENTS.md` and `CONTRIBUTING.md` reference ADRs; this is their home. Authority and process:
see `.claude/rules/adr.md`. **Precedence on conflict: ADR > guidelines/`AGENTS.md` > code/README.**

## Rules
- **One file per decision**, `NNNN-short-slug.md` (zero-padded, sequential).
- A file **stays once accepted** — to change a decision, write a **new** ADR that supersedes it and
  set the old one's status to `Superseded by NNNN`.
- Copy [`_template.md`](_template.md) for new ADRs.

## Status values
`Proposed` · `Accepted` · `Deprecated` · `Superseded by NNNN`

## Index
| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-changelog-emoji-sections.md) | Emoji section headers in changelog & release notes | Proposed |

<!-- ADR numbers are assigned in creation order; the architecture-backfill candidates below get 0002+. -->

<!-- Candidate ADRs to backfill from the existing concept docs:
  0001 container-first / host-thin
  0002 three custom images (scan-bridge / sane-runtime / scan-processor)
  0003 Synology as single source of truth for documents
  0004 trigger-source-agnostic HTTP + bearer auth
  0005 Conventional Commits + release-only semantic-release (manual Keep-a-Changelog kept)
-->
