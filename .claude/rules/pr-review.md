# PR review findings (Gemini / Copilot) — resolve every one

Auto-loaded. Every PR is reviewed by AI bots; **each finding must get a status** — none left open:
- **Fixed** → commit the fix and note it on the finding thread (what/where, optionally the SHA).
- **False positive / intentionally not applied** → reply on the thread **with reasoning**.

Silent dismissal is forbidden (cf. error-culture R0).

## Procedure (also for AI agents)
1. Fetch all threads before merge: `gh pr view <n> --repo <owner>/<repo> --comments` and inline:
   `gh api repos/<owner>/<repo>/pulls/<n>/comments`.
2. Order by severity; real bugs/security first.
3. Per finding: fix **or** justify as false positive — and **reply**.
4. Merge only when every finding has a status and CI is green.

Reply to an inline finding:
```bash
gh api repos/<owner>/<repo>/pulls/<n>/comments/<comment_id>/replies -f body="<reply>"
```
General PR comment: `gh pr comment <n> --repo <owner>/<repo> --body "<text>"`.
