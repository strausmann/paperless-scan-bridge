# Time and order — compute them, never assert them

Auto-loaded. Applies to any change that touches a timer, an interval, a poll, a
timeout, a counter that wraps, or the order in which two components initialise —
and to any sentence, in code or in docs, that says *when* something happens or
*how long* it takes.

## The rule

**Every claim about timing or ordering must carry the arithmetic or the ordering
that produces it, written next to the claim.** If you cannot write the chain out,
you do not know the number, and you must not state it.

This is not about being careful. It is about a specific failure that reading
cannot catch: code whose wrongness only appears after minutes, days or weeks. A
reviewer sees the comment, agrees it sounds right, and moves on. So does the
author. The only thing that catches it is doing the sum.

## Three shapes it takes

### 1. A cadence is not a latency

"It polls every 60 s" does **not** mean "it notices within 60 s". Write the chain
of everything that must elapse before the effect is observable:

    detect  <= settled poll interval  30min  (the failure happens between
                                              polls, and the cadence is still
                                              the slow one at that point)
             + client timeout          55s    (a failing request may run to
                                              its timeout — or return at once,
                                              on DNS failure or ECONNREFUSED)
             = 30min 55s

    recover <= supervisor tick        60s    (to observe the failure)
             + fast poll interval     60s    (the cadence is the fast one by
                                              now; start_poller schedules a
                                              full interval)
             + client timeout         55s    (same caveat)
             = 175s

Note that the two chains take **different** poll intervals — 30 min for
detection, 60 s for recovery — because the cadence changes in between. Naming
the term "one poll interval" in both, as the first version did, produced a chain
whose own arithmetic contradicted its conclusion.

Detection and recovery are different numbers. Stating one as the other is how
`firmware/esp32-panel/README.md` promised "noticed within the minute" for
something that can take half an hour and a minute.

**Note the `<=`.** A chain of maxima gives a bound, not a duration, and the
difference is not pedantry: most of those terms have a fast path. So every
timing claim must also say *which* number it is — at most, at least, or
typically. A bare figure is not a computed claim, it is an unlabelled one.

Both of those corrections came from reviewers, on this rule's own PR. The first
version wrote `detect = one poll interval` and omitted the timeout entirely; the
second added it as a certainty and thereby overstated every future claim derived
from the rule. Writing the chain out is necessary and not sufficient. Read it
back twice — once asking "and how long does *that* take?", once asking "and is
that the most, the least, or the usual?"

And then **add it up and write the total down.** The `= 175s` above was absent
for three drafts, during which the prose beside it said "about two minutes" —
a chain nobody sums is decoration.

### 2. A wrapping counter is only meaningful over half its period

`(int32_t)(deadline - millis()) > 0` is wrap-safe **for a deadline within ±24.8
days of now, and for nothing else.** A stored deadline left in place long after
it lapsed wraps back to positive.

So: state the valid range in the comment, and **bound the stored value so it
cannot leave that range**. Two ways, and the shorter one is a trap:

- **Recompute** the deadline instead of storing it. Nothing to age.
- **Clear it on expiry — and test the sentinel separately, before the
  subtraction.** Clearing alone does not help: `(int32_t)(0 - millis())` is
  positive again after 24.8 days of uptime, so a zeroed deadline reads as
  "in the future" exactly as the stale one did. It has to be
  `if (deadline != 0 && (int32_t)(deadline - millis()) > 0)`, never the
  subtraction on its own.

A comment reading "wrap-safe" without the range is worse than no comment: it
asserts a property nobody has enumerated. This paragraph is itself the example
— its first version said only "clear it on expiry", which a reader following it
literally would have implemented as the bug it warns about.

### 3. Cross-component setup order is not yours to assume — look it up

State written during one component's `setup()` and read during another's is
ordered by the framework, not by the file. And "I do not know the order" is the
wrong conclusion: the order is written down, so look it up.

The case that produced this rule was not a race. ESPHome orders `setup()` by
descending `setup_priority`, and:

    TemplateText      HARDWARE    800   publishes the restored Bridge URL,
                                        which cleared a "poller running" flag
    HttpRequestUpdate AFTER_WIFI  200   starts the poller

So the text **always** runs first and the poller **always** starts afterwards —
the order is deterministic, and it is deterministically the unhelpful one. It
would have bitten on every boot. Treating it as "probably fine" was the error;
two greps would have settled it.

Then: **make the later reader re-assert the desired state rather than trust a
flag.** An idempotent "stop it if it should be stopped" survives any order; a
guarded "stop it only if my flag says it is running" does not.

## When this applies

Before pushing, run this pass yourself if the diff contains any of:

`millis()` · `Ticker` · `time.After` · `interval:` · `delay:` · `timeout` ·
`update_interval` · `poller` · `setup()` · `restore_value` · any duration quoted
in prose or a comment.

It is a short pass. Fourteen review rounds on PR #113 produced three real bugs
of exactly these three shapes, all of which the arithmetic would have shown in a
minute. See `docs/learnings/lessons-learned.md`, 2026-08-28.

## Relationship to the other rules

This is R1 (verify before concluding) applied to the one class of claim where
"it looks right" is systematically misleading, and R0's guard for the 2026-08-28
entry. A timing claim is a conclusion; it needs evidence like any other.
