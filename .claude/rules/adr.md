# Architecture Decision Records (ADRs) — top authority

Auto-loaded. ADRs record binding decisions in Nygard format (**Context → Decision → Consequences**,
plus "Options considered"); they live in `docs/decisions/` (`_template.md`, index in `README.md`).

## Authority
On conflict: **accepted ADRs > guidelines/`AGENTS.md`/`ARCHITECTURE.md` > code/README**. If a doc or
the code contradicts an accepted ADR, that's a bug to fix — or it needs a **new, superseding ADR**.
No one (human or AI) silently overrides an accepted ADR. A guideline is the *operative* form of an
ADR decision, not a replacement.

## Create
1. Copy `docs/decisions/_template.md` → `docs/decisions/NNNN-short-slug.md` (zero-padded, sequential).
2. Fill Status (`Proposed`) · Date · Deciders · Context · Decision (positive "We will …") · Options
   considered (chosen + alternatives) · Consequences · References.
3. Add a row to the index in `docs/decisions/README.md`.

Status: `Proposed` · `Accepted` · `Deprecated` · `Superseded by NNNN`.

## Manage
- An accepted ADR is **not edited** afterwards — change = a **new** ADR that supersedes it (old →
  `Superseded by NNNN`).
- Decisions touching endpoints/config/containers: update the matching doc(s) in the same PR.

## For AI agents
- Read relevant accepted ADRs **before** any spec/plan/implementation.
- The review agents check specs & PRs **against the ADRs** and flag deviations as blocking.
- New direction → propose an ADR (`Proposed`) first; don't implement silently.
