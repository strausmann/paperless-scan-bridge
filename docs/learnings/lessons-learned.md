# Lessons Learned

Newest first. Format & process: see [`README.md`](README.md) and `.claude/rules/error-handling.md`.

## 2026-08-06 — Plan↔ADR reconciliation missed a third conflict (profiles storage)

- **What happened:** The Phase 1.2 reconciliation (issue #19, PR #20) aligned the
  plan to ADRs 0005 (trigger endpoint) and 0009 (transport) but did not audit the
  plan against the *full* ADR set. A third load-bearing conflict — plan wants
  profiles in SQLite + CRUD (Tasks 3/4/5) while ADR 0010 mandates declarative
  YAML and explicitly defers a DB to Phase 1.4 — was only caught later, at the
  moment of starting implementation.
- **Root cause:** Reconciliation was driven by the conflicts already spotted, not
  by a systematic pass over every accepted/proposed ADR that the plan touches.
  Verification was scoped to known issues instead of the whole surface.
- **Impact:** PR #20 shipped a reconciliation that read as complete but wasn't;
  had implementation started on the plan's data layer, it would have violated
  ADR 0010. No code was written, so no regression — caught before the wrong turn.
- **Fix / prevention:** Before implementing against any plan, run a **full
  plan-vs-ADR audit** — enumerate every ADR (by scope/topic the plan touches) and
  mark each consistent / conflicting, not just the first conflicts noticed. Guard
  added as a checklist step in `.claude/rules/decision-process.md`.
