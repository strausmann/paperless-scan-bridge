# Lessons Learned

Newest first. Format & process: see [`README.md`](README.md) and `.claude/rules/error-handling.md`.

## 2026-08-26 — A deploy success criterion was derived from inference, not from source

- **What happened:** The hand-off instructions for updating the stack on the
  reference host listed five success criteria. One of them —
  "`/version` must no longer report `phase-1.2-task-15`; if it does, the old
  build was deployed" — was wrong. `VERSION: phase-1.2-task-15` is a hardcoded
  build arg in `compose.yaml` (lines 135 and 203) that `main` still carries, so
  a perfectly successful deploy still reports that value. The criterion would
  have flagged a good deploy as a failure. The operator running the deploy
  checked the source, disproved the criterion, and said so instead of reporting
  a false red.
- **Root cause:** The criterion was built by *inference*, not verification. The
  old stack answered `/version` with `phase-1.2-task-15`, and that was assumed
  to be an artifact of the old build — without opening `compose.yaml` on `main`
  to see where the value actually comes from. One `grep VERSION compose.yaml`
  would have shown it is a literal, identical on both sides, and therefore
  useless as a deploy indicator. This is exactly the failure mode R1
  (`00-core.md`) exists to prevent: a conclusion stated from plausibility rather
  than from the artifact.
- **Impact:** No damage — the operator caught it. Had they trusted the criterion,
  a successful deploy would have been rolled back or rebuilt for nothing. The
  two criteria that did carry information (`/ready` flipping from `501` to `200`,
  and `scan-processor` existing at all) proved the new build independently.
- **Fix / prevention:** Two parts. (1) Behavioural: a success criterion must name
  the artifact it reads and be checked against that artifact on **both** sides
  before it is handed to anyone — captured as a rule in
  `.claude/rules/00-core.md` (R1). (2) Concrete guard: make `/version` actually
  informative instead of a stale literal, so the obvious criterion becomes the
  correct one — `compose.yaml` now passes `VERSION: ${PSB_VERSION:-dev}` rather
  than a hardcoded task label that stopped tracking reality after the task that
  named it.

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
