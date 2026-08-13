# 0021 — `Destination.Deliver` returns `(DeliveryResult, error)`, not `error` alone

- **Status:** Accepted
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** destinations

## Context

ADR [0016](0016-destination-routing-pluggable-interface.md) created the pluggable `Destination`
interface and registry, but explicitly left the exact method signature undecided
("the exact `Destination` interface signature ... are implementation details for the plan that
implements this ADR, not decided here" — 0016, Consequences). Its illustrative aside in the same
section ("a `Destination.Send(ctx, doc) error`-shaped interface") was never a decision, only an
example of the shape of seam being created.

Task 2 (`internal/destinations/destination.go`, #42) and Task 4 (the Paperless-ngx module, #44)
implemented `Deliver(ctx, doc, meta, cfg) error`, matching the 2026-08-13 design doc's §5.2 sketch
and §11.4 recommendation at the time. That signature turned out to be insufficient: Paperless-ngx's
`POST /api/documents/post_document/` returns `{"task_id": "<uuid>"}` (design doc §2, §5.3), and the
design doc's own §8 end-to-end sequence and `/scan` response shape
(`destinations: [{name, status, task_id}]`) require that `task_id` to reach the caller. With only
an `error` return, the Paperless module validated and then discarded the `task_id` — it had no
channel to return it through, so `destinationResult.TaskID` in the `/scan` response was always
empty, contradicting §8. This was found and fixed while wiring `handleScan` through the
destinations registry (#48, #50).

## Decision

We will change `Destination.Deliver`'s signature to
`Deliver(ctx context.Context, doc Document, meta Metadata, cfg ProfileDestinationConfig) (DeliveryResult, error)`,
where:

```go
type DeliveryResult struct {
	Status    string // destination-defined outcome, e.g. "submitted"
	Reference string // destination-specific identifier, e.g. Paperless-ngx's task_id
}
```

A nil `error` means the destination accepted the document; `DeliveryResult` carries its
status/reference. A non-nil `error` means `DeliveryResult` is the zero value and must not be used.
The Paperless-ngx module now returns the `task_id` it already validates as `Reference` instead of
discarding it, so the design doc §8 response shape is fully honoured end to end.

This is not a reversal of anything ADR 0016 decided — 0016's decision (pluggable interface +
registry, adding a destination = new package + registration call) is unchanged and this ADR does
not supersede it. It resolves the signature question 0016 itself left open.

## Options considered

- **Option A — `Deliver(...) (DeliveryResult, error)` (chosen):** the destination's own delivery
  reference (task ID, message ID, ...) has a typed channel back to the caller; a destination with
  nothing to report returns the zero-value `DeliveryResult` alongside a nil error. Matches the
  design doc's §8 response contract without inventing a second call or a side-channel.
- **Option B — keep `Deliver(...) error`, add a separate `Reference() string` method or an
  out-parameter:** avoids touching the primary return, but requires either a second interface
  method every module must implement pointlessly (destinations with no reference to report) or a
  mutable out-parameter, which is a worse Go idiom than a second return value — rejected.
- **Option C — stuff the reference into the existing `error` (e.g. a typed error wrapping the ID)
  even on success:** overloads `error` to mean "not necessarily an error", contradicts Go
  convention (`err != nil` means failure) and every caller would need a type-assertion just to read
  a successful reference — rejected.

## Consequences

- **Positive:** the design doc §8 response shape (`destinations: [{name, status, task_id}]`) is now
  actually reachable from a destination module, not just documented as intent. Destinations with no
  reference to offer (most NFS/SMB writes, for example) return an empty `Reference` — the field is
  optional, not every module needs to populate it.
- **Negative / trade-offs:** every future destination module (NFS, SMB, generic HTTP-POST, fileee —
  none built yet, ADR 0016 §5.1) implements the two-return-value signature from the start; this is a
  small, one-time addition to the interface contract each of those modules must satisfy, not a
  migration of existing code (only the Paperless module and its tests existed when this changed,
  #50).
- **Neutral / follow-ups:** `docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`
  §5.2 and §11.4 describe the pre-#50 `Deliver(...) error` sketch; that design doc is a point-in-time
  record of the plan as understood before the response-shape gap in §8 was found during
  implementation and is not corrected retroactively (same "don't edit a settled record silently"
  principle this ADR itself follows) — this ADR and the shipped code in
  `components/scan-bridge/internal/destinations/destination.go` are the current source of truth for
  the interface shape.

## References

- ADR [0016](0016-destination-routing-pluggable-interface.md) (created the seam; left the exact
  signature undecided — Consequences, "Neutral / follow-ups").
- `docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md` §5.2 (pre-#50 sketch), §7–8
  (the response shape this ADR makes reachable), §11.4 (flagged recommendation, superseded by this
  decision).
- `components/scan-bridge/internal/destinations/destination.go` (`Document`, `Metadata`,
  `DeliveryResult`, `Destination` — current, shipped shape).
- PRs: #42 (registry, pre-#50 signature), #44 (Paperless module, pre-#50 signature), #48 (wired
  `handleScan` through the registry), #50 (this change — `Deliver` returns `DeliveryResult`).
- Issue #19.
