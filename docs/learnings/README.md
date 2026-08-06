# Learnings / Lessons Learned

Fed by the **error-culture** process (`.claude/rules/error-handling.md`): every mistake is documented
here so it does not repeat.

- **Log:** [`lessons-learned.md`](lessons-learned.md) — newest first.
- **When:** as soon as a mistake is identified + analyzed (automatically, unprompted).
- **Where the guard goes:** behaviour → rule/memory · decision → ADR (`docs/decisions/`) · code →
  test/guard · hard automation → hook/CI.

## Entry format
```markdown
## YYYY-MM-DD — <short title>
- **What happened:** …
- **Root cause:** …
- **Impact:** …
- **Fix / prevention:** … (which rule/ADR/test/hook was added)
```
