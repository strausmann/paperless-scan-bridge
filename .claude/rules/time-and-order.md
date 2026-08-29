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

    detect  = one poll interval          (the failure happens between polls)
    recover = supervisor tick            (up to 60s to observe the failure)
            + poll interval              (start_poller schedules a full one)
            + client timeout             (the check itself may run to it)

Detection and recovery are different numbers. Stating one as the other is how
`firmware/esp32-panel/README.md` promised "noticed within the minute" for
something that takes up to half an hour.

### 2. A wrapping counter is only meaningful over half its period

`(int32_t)(deadline - millis()) > 0` is wrap-safe **for a deadline within ±24.8
days of now, and for nothing else.** A stored deadline left in place long after
it lapsed wraps back to positive.

So: state the valid range in the comment, and **bound the stored value so it
cannot leave that range** — clear it on expiry, or recompute it. A comment
reading "wrap-safe" without the range is worse than no comment: it asserts a
property nobody has enumerated.

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
