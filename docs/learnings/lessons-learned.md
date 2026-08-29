# Lessons Learned

Newest first. Format & process: see [`README.md`](README.md) and `.claude/rules/error-handling.md`.

## 2026-08-28 — Three bugs about time, none of which reading could catch

- **What happened:** PR #113 (adaptive firmware-check cadence) took **fourteen
  review rounds**. The bots found more than I did, and three of their findings
  were real bugs of mine:

  1. A wraparound comparison, `(int32_t)(deadline - millis()) > 0`, correct only
     for a deadline within ±24.8 days. A stored deadline left in place after it
     lapsed wraps back to positive, so a panel running a month without a Wi-Fi
     reconnect would have polled at the fast rate for roughly half of every
     49.7-day `millis()` cycle — thirty times the requests, and the touchscreen
     stalls this firmware documents elsewhere as the cost of frequent polling.
  2. A poller stopped in one component's `setup()` and restarted by another's.
     `TemplateText::setup` publishes a restored empty Bridge URL, which ran the
     reset and cleared a "running" flag; `PollingComponent::call_setup` then
     started the poller anyway, later in the same setup pass. The supervisor's
     guarded stop skipped it, so a panel whose URL had been cleared polled
     `bridge.invalid` every 60s forever — exactly the behaviour the guard was
     added to prevent. Not a race, either: ESPHome orders `setup()` by
     descending `setup_priority`, `TemplateText` is `HARDWARE` (800) and
     `HttpRequestUpdate` is `AFTER_WIFI` (200), so the unhelpful order is the
     only order. It would have failed on every boot.
  3. Two latency overclaims, in six files: "a bridge that disappears is noticed
     within the minute" and "picked up again within a minute of returning".
     Detection takes a full poll interval **plus the client timeout** — the
     failing request has to time out before anyone knows it failed — so half an
     hour and a minute; recovery takes the supervisor tick plus a poll interval
     plus the client timeout, about two minutes.

- **Root cause:** one cause, three shapes. **Every one of these was a claim
  about time or ordering that I asserted instead of computed.** In each case a
  single line of arithmetic or one question about ordering would have shown it,
  and in each case I had written a confident comment instead — `wrap-safe`,
  `if (running) stop`, `within the minute`. The comments were not lies; they
  were unexamined. A reviewer reads them, agrees they sound right, and moves on.
  So did I.

  The reason the bots outperformed me here is not that they are better
  reviewers. It is that they did the sum and I did not.

- **Impact:** none shipped — all three were caught in review, on a branch. The
  cost was fourteen rounds on one PR, roughly two and a half hours of
  push-review-fix cycles for findings that a five-minute pass over the diff
  would have produced.

- **Fix / prevention:** new rule **R7 — compute time and order, never assert
  them** (`.claude/rules/00-core.md`, details in
  `.claude/rules/time-and-order.md`). Every claim about when something happens,
  how long it takes, or in what order two components initialise must carry the
  arithmetic next to it; if the chain cannot be written out, the number is not
  known and must not be stated. The rule names the three shapes explicitly — a
  cadence is not a latency, a wrapping counter is meaningful only over half its
  period, cross-component setup order belongs to the framework so the later
  reader must re-assert state rather than trust a flag — and lists the tokens
  in a diff that trigger the pass (`millis()`, `Ticker`, `interval:`, `delay:`,
  `timeout`, `poller`, `setup()`, `restore_value`, any duration in prose).

  Deliberately not a "be more careful" rule. The pass is mechanical and short,
  and its trigger is a grep.

  **A postscript that belongs in the entry.** The first version of R7 got the
  detection formula wrong in the same way it warns about: it wrote
  `detect = one poll interval` and omitted the client timeout, because "the
  poll notices it" reads as an instant. A reviewer caught it on the rule's own
  PR. Writing the chain out is necessary and not sufficient — it has to be read
  back, asking of every line "and how long does *that* take?". The rule now says
  so, with itself as the example.

## 2026-08-28 — A scratch file in /tmp overwrote a source file, and the commit shipped it

**What happened.** While mutation-testing my own change to
`internal/config/config.go`, the save step `cp internal/config/config.go
/tmp/cfg.bak` failed with `permission denied`: `/tmp/cfg.bak` already
existed and belonged to something else. The restore step,
`cp /tmp/cfg.bak internal/config/config.go`, then *succeeded* — copying
that stranger's file over the repository's. It was a `config.go` from an
unrelated project: `fileee-mcp-server` and `gangway` imports, German
comments, a `Config` type with none of this daemon's fields. The package
no longer built. It was committed and pushed.

**Root cause.** Two, and neither is the `cp` itself:

1. A **fixed name in a shared directory** for a scratch file.
   `/tmp/<something>.bak` is a name anything on the machine may already
   own, and `cp` reports that by failing — which is fine, as long as
   somebody reads it.
2. **Committing in the same command block as a step that mutates the
   tree.** The build and the test run happened *before* the mutation;
   the restore and the commit happened after, with nothing in between.
   So the one thing that would have caught it — building after
   restoring — was never run.

**Impact.** One commit on a feature branch with a source file replaced
by an unrelated project's. Caught within minutes by the next `go build`,
before merge. No release, no deployment.

**Fix / prevention.**

- Scratch copies go to a **per-invocation** directory, not a fixed name
  in `/tmp`. A collision is then impossible rather than merely
  reported.
- A mutation cycle **ends with a build and the full suite**, after the
  restore, before anything is staged. "Restore, then verify" — the
  same discipline as "download, verify, then publish" in the code this
  was testing.
- Never put `git commit` in the same block as a step that rewrites
  files. Separate the verification from the mutation, and let the
  verification be the last thing that runs.

Related: the 2026-08-27 entry on claiming a fix before making the edit.
Both are the same shape — a command block whose later steps assume the
earlier ones did what they were supposed to.

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
