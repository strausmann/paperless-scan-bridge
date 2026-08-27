# Lessons Learned

Newest first. Format & process: see [`README.md`](README.md) and `.claude/rules/error-handling.md`.

## 2026-08-27 — A branch cut from another feature branch duplicated a merged change

- **What happened:** `/en/manage/` and `/de/manage/` shipped the "The panel is
  only findable while it has no Wi-Fi" box **twice** — once above the
  `## Connect over Bluetooth` heading and once below it. The operator spotted it
  on the deployed site.
- **Root cause:** `fix/panel-crash-diagnostics` (PR #82) was created with
  `git checkout -b` while the working copy was still on
  `docs/manage-precondition-before-button` (PR #81), not on `main`. PR #82 thus
  carried PR #81's *first* commit (box moved above the heading) but not its
  *second* (box moved below it, fixing the anchor). Squash-merging #81 put one
  box below the heading; squash-merging #82 afterwards re-applied the inherited
  first commit on top, adding a second box above it. Git cannot flag this: the
  two states touch different line ranges, so there is no textual conflict — only
  a semantic one.
- **Impact:** A duplicated paragraph on two published pages. Both PRs were green
  and both bot reviews passed; nothing in CI or review looks at whether a branch
  descends from `main`.
- **Fix / prevention:** Always `git checkout main && git pull` immediately before
  `git checkout -b`. Never run `git checkout -b` from an unverified current
  branch. Before opening a PR, check what it actually contains —
  `git log --oneline main..HEAD` must show only that PR's own commits; a foreign
  commit in that list means the branch point is wrong and the branch must be
  rebased onto `main` before review.

## 2026-08-27 — A commit SHA was quoted in a public review reply without checking it

- **What happened:** Replying to a Copilot finding on PR #82, the reply cited the
  fix as `0b0e2d4`. The actual commit was `15992f3`. The SHA was written from
  expectation, in the same command that created the commit, so it was never read
  back from `git log`. A correction had to be posted publicly.
- **Root cause:** The commit and the reply about the commit were composed in one
  step. Anything referring to an artifact that the same step produces cannot have
  been verified against it: the artifact does not exist yet when the
  reference is written.
- **Impact:** A wrong SHA in a review thread on a public repository. Cosmetic in
  effect, but it points a reader at a commit that does not exist, and it is
  exactly the class of unverified claim R1 exists to prevent.
- **Fix / prevention:** Never write an identifier for an artifact in the same step
  that creates it. Commit first, read the SHA back (`git log --oneline -1`), then
  compose any text that cites it. Generalized: a reference to a SHA, URL, line
  number, file path or version in public text must be copied from a command's
  output, never from memory or expectation.

## 2026-08-26 — The first real end-to-end scan failed on a file mode nobody had documented

- **What happened:** The first authenticated `POST /scan` against the real
  scanner worked: duplex ADF, two pages, PDF assembled, 20 s. The Paperless
  upload then failed with `open /run/secrets/paperless_api_token: permission
  denied`. The secret had been placed at the obvious `chmod 0600` (root-only),
  but `scan-bridge` runs as the distroless image's `nonroot` user (UID 65532),
  so it could not read it.
- **Root cause:** Compose bind-mounts a file-based secret with the host's own
  ownership and mode; nothing translates it for the container user. The compose
  file already reasoned carefully about exactly this for the *sockets*
  (`group_add: ["0", "10010"]`, with a long comment), but the same reasoning was
  never applied to the *secret file*, and no doc stated a required mode. `0600`
  is the natural thing to reach for with a credential, and it is wrong here.
- **Impact:** One failed upload on the first real run. Diagnosable only from the
  error string plus `docker inspect` — the distroless image has no shell, so the
  usual `ls -l /run/secrets/` from inside the container is not available. Cheap
  this time; on an unattended deployment it would look like "scanning works,
  documents silently never arrive".
- **Fix / prevention:** `chmod 0640` with root group ownership, which the
  existing supplementary group 0 already covers — no new privilege. Documented
  in `compose.yaml` next to the secret definition, including the exact error
  string so a search lands on it, and why `0644` is not the answer.

## 2026-08-26 — A deploy success criterion was derived from inference, not from source

- **What happened:** The hand-off instructions for updating the stack on the
  reference host listed five success criteria. One of them —
  "`/version` must no longer report `phase-1.2-task-15`; if it does, the old
  build was deployed" — was wrong. At the time, `VERSION: phase-1.2-task-15`
  was a hardcoded build arg that `compose.yaml` carried on `main` for both
  `scan-bridge` and `sane-runtime`, so a perfectly successful deploy still
  reported that value. (The commit that records this lesson also replaces those
  literals, so the current `compose.yaml` no longer looks like this.) The
  criterion would
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
  named it, and `make stamp` writes the git description into `.env` so the
  ordinary `docker compose` path stamps a real value instead of a second
  constant.

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

## 2026-08-27 — A fix was claimed on a PR before the edit had run

- **What happened:** Replying to a review finding on PR #105, the reply said
  "behoben in `<sha>`" and named the branch head. No fix had been made: the
  edit script had failed its own assertion moments earlier, and the finding
  was in fact already resolved by the branch author's own commit. A public
  correction had to follow.
- **Root cause:** Two mistakes compounded. The branch state was read from
  `gh pr diff`, which shows the difference against `main` and therefore the
  *pre-fix* wording, rather than from the file on the branch. And the reply
  was in the same command block as the edit, so it went out regardless of
  whether the edit succeeded — the block continued past a failed Python
  assertion because only that one interpreter exited non-zero.
- **Impact:** A false claim of authorship and of work done, on a public PR,
  for a second time in one session (see the SHA entry above).
- **Fix / prevention:** Read the branch, not the diff, before asserting what
  a branch contains — `gh pr diff` answers "what does this change" and never
  "what does this file say now". And never put a public statement in the same
  command block as the change it describes: run the edit, verify it landed,
  then post. The existing rule about not naming an artifact in the step that
  creates it extends to this — a claim about a change is such an artifact.
