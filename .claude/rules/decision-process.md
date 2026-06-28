# Decision process — 95% clarity before specs/plans

Applies to every new feature/change (and across all our projects).

## 1. Clarify to ≥95% before any spec or plan
Before writing a spec or implementation plan, ask clarifying questions until it is **≥95% clear**:
(a) **WHAT** is required and (b) **HOW** it should be built. **No spec/plan is written before that bar
is met.** This is how we avoid running in the wrong direction.

## 2. Document the Q&A in the issue
Record every clarifying **question and its answer in the GitHub issue** — so it is always traceable
*when*, and *via which question*, a decision was made. When 95% is reached, the resolved decisions are
also captured in an **ADR** (`docs/decisions/`).

## 3. Conflict with an accepted ADR
If a new idea/decision contradicts an accepted ADR, do **not** just proceed. First clarify the
**consequences** and the **pros/cons**, then decide whether it becomes a **new, superseding ADR**
(see `adr.md`). Only then implement.
