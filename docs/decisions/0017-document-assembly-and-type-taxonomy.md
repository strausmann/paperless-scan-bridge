# 0017 — Document assembly and document-type taxonomy are per-profile, destination-interpreted

- **Status:** Proposed
- **Date:** 2026-08-13
- **Deciders:** strausmann
- **Tags:** profiles, scan-processor, tag

## Context

Two related, previously open questions from the 2026-08-13 vision
([`docs/roadmap/2026-08-13-scan-system-vision.md`](../roadmap/2026-08-13-scan-system-vision.md),
Epics A6 and A7) need a combined answer because they touch the same profile-to-destination
boundary that ADR [0016](0016-destination-routing-pluggable-interface.md) just created:

1. **Multi-page result shape** — when a profile scans several pages, should the result be one
   multi-page object (e.g. a single PDF) or one object per page?
2. **Document type/kind → labels and actions** — a profile might represent "Eingangsrechnung"
   (incoming invoice), "Post" (mail), or "Verträge" (contracts), and each type should drive
   destination-specific labels/tags and actions (Paperless tags/correspondent; fileee labels).

Today's `MetadataTemplate` (`components/scan-bridge/internal/profiles/profiles.go`) only knows
`PaperlessTags`/`PaperlessCorrespondent` — there is no page-grouping field and no destination-
agnostic type concept.

## Decision

We will add, **per profile**:

1. A **page-grouping setting** (one multi-page object vs. one object per page) that
   `scan-processor`'s assembly step reads to decide whether to combine captured pages into a
   single document or emit them individually.
2. A **document type** field whose value maps, via **per-profile config**, to **destination-
   specific labels/tags and actions** — for Paperless-ngx this extends the existing
   `PaperlessTags`/`PaperlessCorrespondent` shape; for fileee it drives fileee's own label
   mechanism. The **taxonomy itself and the label/action mapping live in the profile's config**,
   not in a central enum baked into the core — each destination module (ADR 0016) interprets the
   mapping for the fields it actually understands. The dispatch/assembly core does not need to
   know what "Eingangsrechnung" means; it only passes the resolved type/labels through to the
   chosen destination module.

## Options considered

- **Option A — per-profile fields, destination-module-interpreted mapping (chosen):** keeps the
  core destination-agnostic (consistent with ADR 0016), lets each destination module define its
  own label/action semantics without a shared enum that has to satisfy every destination at once.
- **Option B — a single global taxonomy enum baked into the core:** would give cross-profile
  consistency for free, but couples the core to Paperless/fileee-specific label semantics and does
  not scale to a future destination with a different label model — rejected.
- **Option C — runtime-signal-driven type selection (e.g. a barcode/QR read chooses the type
  dynamically):** `CONCEPT.md`'s splitting fields already gesture at a similar mechanism for a
  different purpose, and this could be a natural extension later, but it is out of scope here —
  deferred to a future ADR if and when a concrete need appears.

## Consequences

- **Positive:** the existing `MetadataTemplate.PaperlessTags`/`PaperlessCorrespondent` fields
  extend naturally into the new type-mapping shape rather than being replaced; a new destination
  module can define its own action model without any change to `profiles.go`'s core validation
  beyond generic string/map fields.
- **Negative / trade-offs:** the taxonomy is scattered per-profile rather than centrally
  enumerated — nothing in code enforces that "Eingangsrechnung" is spelled the same way across
  profiles. For v1 this is a documentation/convention concern, not a code-enforced one.
- **Neutral / follow-ups:** the fileee **account/auth** mechanism referenced by a fileee-targeted
  label mapping remains an open sub-question (shared with ADR 0016, tracked in the roadmap triage
  doc, not resolved here). Per-destination defaults for page-grouping (e.g. Paperless defaulting to
  combined, fileee defaulting to per-page) are an implementation detail for the plan that
  implements this ADR, not decided here — fileee's actual multi-page/multi-object model needs a
  concrete look at its API before a default is chosen.

## References

- `docs/roadmap/2026-08-13-scan-system-vision.md`, Epics A6 and A7.
- ADR [0016](0016-destination-routing-pluggable-interface.md) (destination modules interpret the
  mapping).
- `components/scan-bridge/internal/profiles/profiles.go` (`MetadataTemplate`, current shape).
- `CONCEPT.md` (splitting-field precedent for runtime-signal-driven behaviour, Option C).
