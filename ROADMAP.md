# Roadmap

This document describes the planned evolution of `paperless-scan-bridge`
in four phases. It is updated as work progresses and serves both as a
public statement of intent and as the source of truth for tracked
issues.

The bullets tagged `(vision)` below summarise a larger scan-system vision
the operator described on 2026-08-13. The full epic breakdown, with each
feature triaged as ready-to-build or still needing a decision and the
exact open questions for the latter, lives in
[`docs/roadmap/2026-08-13-scan-system-vision.md`](docs/roadmap/2026-08-13-scan-system-vision.md).
This roadmap file stays the single-page overview; that document is the
detail behind it.

## Status legend

- `[ ]` not started
- `[~]` in progress
- `[x]` complete
- `[?]` under evaluation

## Phase 0 — Foundation

**Status: complete except the launch blog post**

The repository exists, the documentation site is live, and the project
can be cited and linked. This phase produces no working software of its
own — that starts in Phase 1 — but ensures contributors and early users
can orient themselves.

- `[x]` Repository created at `github.com/strausmann/paperless-scan-bridge`
- `[x]` MIT license, README, ARCHITECTURE, this ROADMAP
- `[x]` AGENTS.md and CONTRIBUTING.md
- `[x]` CODE_OF_CONDUCT, SECURITY, THREAT_MODEL, DISASTER_RECOVERY
- `[x]` Initial HARDWARE_COMPATIBILITY table with Kodak ScanMate i1120 reference entry
- `[~]` Custom domain `scan-bridge.strausmann.de` configured for GitHub
   Pages — the workflow writes the `CNAME`; the DNS record and the
   Pages source setting are manual steps outside the repository
- `[x]` Zensical site skeleton with EN content, DE placeholder
- `[ ]` First blog post draft on the project motivation
- `[x]` GitHub issue templates for bug, hardware, feature
- `[x]` GitHub Actions for documentation build and deploy
- `[x]` Pre-commit hook configuration for shellcheck, markdownlint, yamllint

**Definition of done for Phase 0:** A visitor lands on the docs site,
understands what the project is, sees a clear roadmap, and can click
through to the architecture and contribution guides.

## Phase 1 — Minimum viable stack

**Status: in progress**

Phase 1 is split into sub-phases. 1.1 (the `scan-bridge` HTTP surface,
configuration and profile loading) is merged. 1.2 (the webhook-triggered
scan path: SANE-net client, job store, PDF output, Paperless upload) is
specified and planned under `docs/superpowers/`, gated on the SANE-net
protocol spike. 1.3 covers image processing and OCR, 1.4 the web UI and
the remaining API surface.

A working stack that successfully scans a document via webhook and
delivers it to Paperless-ngx. No hardware buttons, no Zigbee, no
backup yet. Just the core path from trigger to document.

- `[x]` `sane-runtime` container, Debian slim base, with SANE and
   the Kodak i1120 verified working — proven end to end on 2026-08-26
- `[~]` `scan-bridge` daemon in Go. `GET /health`, `GET /version`,
   `GET /ready`, `GET /profiles`, `GET /profiles/{name}` and
   `POST /scan` are all implemented — `POST /scan` is a real,
   bearer-protected handler that dispatches through `sane-runtime` and
   `scan-processor` to delivery. Only the three `/jobs` endpoints still
   return `501 Not Implemented`, waiting on the Phase 1.4 job store.
- `[x]` `scan-processor` container: deskew, blank-page removal, OCR
   with a confidence gate, PDF assembly, atomic NFS write
- `[x]` Bash bootstrap script that installs Docker, mounts NFS and
   loads udev rules (`deploy/bootstrap/install.sh`, idempotent, with
   `--dry-run`). Not yet run end to end on an unprepared machine.
- `[x]` Reference compose stack for Topology B (NFS direct) as the
   simplest start (`deploy/compose/scan-bridge.yml`, pinned GHCR images)
- `[x]` Documentation: getting started, hardware setup, first scan — live at
   scan-bridge.strausmann.de, English and German
- `[x]` Tilt configuration for local development (`Tiltfile`)
- `[x]` GitHub Actions for multi-arch container builds, push to GHCR (`docker-
   bake.hcl`, amd64 + arm64). CI also builds, lints and tests the Go code,
   which it did not until 2026-08-27
- `[x]` Renovate configuration for dependency updates (`renovate.json`; the
   split with Dependabot is documented in it)
- `[ ]` First blog post published, announcing the launch

**Definition of done for Phase 1:** A user with a fresh Pi, a Synology
NAS, and a Paperless-ngx instance can run the bootstrap script, send a
single curl request, and see a PDF in Paperless within 30 seconds.

### Phase 1.3 — Image processing, OCR, formats, page handling

**Status: essentially complete.** Only the two `Needs clarification` items below
are open; everything `Ready to dev` has shipped. This sub-phase did not have its
own checklist yet;
the items below fill it in, including the profile "Baukasten" (building
block) features from the 2026-08-13 vision. See the vision document for
the full spec and open questions on each `(vision)` item.

- `[x]` `scan-processor` deskew, blank-page removal, PDF assembly
- `[x]` `(vision)` Per-profile OCR on/off, plus `ocr.min_confidence` and
   `ocr.languages: [auto]` (Epic A2)
- `[x]` `(vision)` Output format: `png` added to the existing
   `pdf`/`jpeg`/`tiff` set (Epic A3)
- `[x]` `(vision)` Feeder behaviour: `max_pages` caps how many sheets one
   scan pulls; `0` drains the ADF, `1` is the single-sheet case (Epic A5).
   No separate `single_sheet` field — it would mean exactly `max_pages: 1`.
- `[ ]` `(vision)` Multi-page result shape: one combined object vs. one
   object per page, with destination-specific defaults — **Needs
   clarification**, see vision doc Epic A6
- `[ ]` `(vision)` Document type/kind → destination-specific labels and
   actions (e.g. "Eingangsrechnung", "Post", "Verträge") — **Needs
   clarification**, see vision doc Epic A7

### Phase 1.4 — Web UI, profile management, remaining API surface

**Status: planned.** Also did not have its own checklist yet.

- `[ ]` Profile CRUD web UI, drag-and-drop ordering (already scoped in
   issue #9 phase B, deferred there pending this phase)
- `[ ]` Profiles move from YAML to a small database (ADR 0010's explicitly
   deferred follow-up) — needed for `scan_count_total` and `display_order`
- `[ ]` `scan_bridge_jobs_total`, per-stage duration histograms, queue
   depth, per-profile usage metrics (already named as `TODO(phase 1.4)` in
   `internal/metrics/metrics.go`)
- `[ ]` `(vision)` Destination: upload to Paperless-ngx **or** fileee, and
   to which account/target — **Needs clarification**, see vision doc
   Epic A1 (interacts with ADR 0004)
- `[x]` `(vision)` OpenAPI 3.1 spec for scan-bridge, rendered with Scalar
   on the docs site (Epic F1). Hand-written, not generated. Reachable only
   from the published site — the daemon does not serve it, which is a gap
   the Phase 1.4 UI should close.

!!! note "Phase 2 was started before Phase 1 finished"

    The CYD scan-control panel, its browser installer, the BLE
    management surface and firmware OTA are all built — they are listed
    under Phase 2 below and marked accordingly. Losing the scanner's
    hardware button (see the i1120 page) made a panel the primary
    trigger rather than a nice-to-have, so it moved forward.

## Phase 2 — Trigger paths and UI

**Status: planned**

Adds the user-facing trigger paths beyond raw HTTP: hardware buttons,
Zigbee remotes via Home Assistant, n8n workflow integration, and an
optional web UI for manual scans.

- `[ ]` `scanbd` integration in `sane-runtime` for hardware buttons
- `[ ]` Profile-counter (LCD) support for Kodak i1120 verified
- `[ ]` Home Assistant blueprint for IKEA STYRBAR
- `[ ]` Home Assistant blueprint for IKEA SYMFONISK Sound Remote Gen 2
- `[ ]` Home Assistant blueprint for IKEA RODRET
- `[ ]` n8n workflow exports for the alternative trigger path
- `[ ]` `scanservjs` adoption with bridge integration
- `[ ]` Documentation: trigger path comparison, HA blueprint usage,
   n8n setup, scanservjs configuration
- `[ ]` Three additional blog posts: trigger paths, hardware buttons,
   profile system

**Definition of done for Phase 2:** A user can place a document, press
a Zigbee button, and have the document appear in Paperless with the
correct tags and metadata, all without touching a keyboard.

### Phase 2 continued — Panel maturity (v3)

**Status: planned.** The ESP32 panel (issue #9) already shipped ahead of
this phase — a fixed 6-button landscape-only grid is running firmware
today (`firmware/esp32-panel/cyd-scan-panel.yaml`). The items below are
v3 additions from the 2026-08-13 vision, all firmware-side unless noted.

- `[ ]` `(vision)` Configurable grid size (replacing the hard-coded 6
   slots) — **Ready to dev**, see vision doc Epic B1
- `[ ]` `(vision)` Paging buttons when more profiles exist than fit the
   grid — **Ready to dev**, see vision doc Epic B2
- `[ ]` `(vision)` Sorting: alphabetical / manual / usage-frequency /
   mixed (static-pinned + frequency-sorted) — **Needs clarification**, see
   vision doc Epic B3 (depends on the Phase 1.4 profile-database work)
- `[ ]` `(vision)` Display rotation (portrait 240×320 in addition to the
   current landscape 320×240 — already called out as a known gap in the
   firmware's own README) — **Ready to dev**, see vision doc Epic B4
- `[ ]` `(vision)` Scan-status shown prominently until completion
   (extends the existing tap → LED/status-label behaviour) — **Ready to
   dev**, see vision doc Epic B5
- `[ ]` `(vision)` Chain-status indicator (green/red/blue) wired to
   `GET /ready` once that endpoint's dispatch dependency lands (currently
   `501`) — **Ready to dev, blocked on the already-planned `/ready`
   endpoint**, see vision doc Epic B6

### Phase 2 continued — Scanner power management

**Status: planned.** New surface — no existing code or ADR touches power
control today.

- `[ ]` `(vision)` Power the scanner on for a job via a
   Zigbee2MQTT-compatible smart plug (e.g. Tasmota Nous A1/A5) — **Needs
   clarification**, see vision doc Epic C1 (control path, owning
   component, target device model all open)
- `[ ]` `(vision)` Auto-off after a configurable idle period — **Needs
   clarification**, see vision doc Epic C2 (depends on C1)

### Phase 2 continued — Firmware OTA

**Status: planned.** The panel firmware already has passwordless OTA
support (the mechanism to push an update); nothing detects availability
yet.

- `[ ]` `(vision)` On-screen "update available" indicator, tap to update
   — **Needs clarification**, see vision doc Epic E1 (source of the
   "new version exists" signal is undecided, and conflicts with the
   firmware's deliberate no-`api:` design — see Epic D2)
- `[ ]` `(vision)` Scheduled automatic-update window (e.g. nightly at
   4am) — **Ready to dev once E1 is resolved**, see vision doc Epic E2

## Phase 3 — Production hardening

**Status: planned**

Brings the stack to production-grade quality: backup, monitoring,
security, automated updates, and disaster recovery testing.

- `[ ]` `restic` backup scripts with retention policies
- `[ ]` PostgreSQL dump integration in the backup pipeline
- `[ ]` Restore runbook with verified procedure
- `[ ]` Monthly automated restore test in CI (against ephemeral VM)
- `[ ]` Prometheus exporter for scan-bridge metrics
- `[ ]` Custom Grafana dashboard for scan pipeline observability
- `[ ]` `(vision)` Scan-count database + additional metrics readable in
   Home Assistant — **Needs clarification**, see vision doc Epic D1
   (exposure path: HA's native Prometheus scraping of the existing
   metrics endpoint vs. push-based MQTT discovery sensors)
- `[ ]` `(vision)` Smarthome status: firmware update availability,
   version, connection status — **Needs clarification**, see vision doc
   Epic D2 (in tension with the panel firmware's deliberate no-`api:`
   design; likely resolution is that `scan-bridge`, not the panel, is the
   single source of truth for Home Assistant)
- `[ ]` Synthetic health check container that scans a test PDF hourly
- `[ ]` SSH hardening Ansible role for the Pi
- `[ ]` Unattended-upgrades configuration
- `[ ]` Watchtower with explicit allowlist (never auto-update Paperless)
- `[ ]` CrowdSec integration for SSH and web frontends
- `[ ]` SOPS-based secrets management with age keys
- `[ ]` Three additional blog posts: backup strategy, threat model
   walkthrough, monitoring setup

**Definition of done for Phase 3:** A user with this stack can have a
hard drive failure, restore from backup, and have working Paperless
with all documents within 90 minutes. They can also detect a stuck
scanner before they notice it themselves.

## Phase 4 — Maturity and ecosystem

**Status: planned**

Long-term sustainability work. Hardware compatibility expansion,
community PR handling, optional integrations, and the lessons-learned
retrospective.

- `[ ]` Hardware compatibility table populated with at least 15
   verified scanner models
- `[ ]` Module-system-aware Zensical migration if upstream releases the
   module API by then
- `[ ]` Optional cloud variant: S3-compatible backup destination
- `[ ]` Optional Helm chart for Kubernetes users
- `[ ]` Migration guide for users coming from scanservjs-only setups
- `[ ]` Migration guide for users coming from Paperless email-based
   ingestion
- `[ ]` "Six months in production" retrospective blog post
- `[ ]` Community contribution template refinement based on actual PRs
- `[ ]` Translation of all blog posts into German

**Definition of done for Phase 4:** The project is sustainable. New
hardware compatibility reports come from the community, not just from
me. Issues get triaged within a week. The repository is being forked
and adapted for related use cases.

## Beyond Phase 4

Speculative directions, neither promised nor ruled out:

- An optional Next.js or HTMX-based web UI as a richer alternative to
  scanservjs
- A mobile-friendly companion app for triggering scans from a phone
- Integration with self-hosted email services (Stalwart, Mailcow) for
  email-as-trigger
- Multi-tenant support if anyone actually asks for it

## Tracking

All tasks above translate to GitHub issues in the
[paperless-scan-bridge](https://github.com/strausmann/paperless-scan-bridge/issues)
repository. Issues are labeled by phase (`phase:0`, `phase:1`, etc.)
and by component (`component:scan-bridge`, `component:docs`, etc.).

The current phase is also reflected in the milestones list. Closed
milestones represent completed phases.

## How to influence the roadmap

Open an issue. Roadmap changes are decided through public discussion,
not in private. Phase ordering can shift based on:

- Real-world demand (multiple users requesting the same feature)
- Hardware availability (a new scanner generation lands and we need
  compatibility before Phase 4)
- Upstream changes (Zensical module system release, Paperless-ngx
  breaking changes)

The one constant: backup and security never get deprioritized. They
are the floor under everything else.
