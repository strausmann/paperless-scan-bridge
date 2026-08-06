# Error culture — automatic post-mortem (top rule)

Auto-loaded; applies always. Making a mistake is fine; **repeating the same one is not.** As soon as
an error occurs, automatically and without being asked:

1. **Analyze** the **root cause** (not just the symptom).
2. **Document** an entry in `docs/learnings/lessons-learned.md` (What happened · Root cause · Impact ·
   Fix/Prevention).
3. **Prevent recurrence** with a concrete measure:
   - behaviour/process → a new/extended **rule** (`.claude/rules/`);
   - decision/architecture → an **ADR** (`docs/decisions/`);
   - code/regression → a **test** or guard;
   - hard automation → a **hook** / CI check.

Name mistakes openly, including your own — transparency over silent correction. This rule has
**precedence**: handle a detected error (analyze + document + guard) before just moving on. Closely
related to verify-before-concluding (`00-core.md` R1).
