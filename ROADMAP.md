# Roadmap

This document describes the planned evolution of `paperless-scan-bridge`
in four phases. It is updated as work progresses and serves both as a
public statement of intent and as the source of truth for tracked
issues.

## Status legend

- `[ ]` not started
- `[~]` in progress
- `[x]` complete
- `[?]` under evaluation

## Phase 0 — Foundation

**Status: in progress**

The repository exists, the documentation skeleton is in place, and the
project can be cited and linked. This phase produces no working
software but ensures contributors and early users can orient themselves.

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

**Status: planned**

A working stack that successfully scans a document via webhook and
delivers it to Paperless-ngx. No hardware buttons, no Zigbee, no
backup yet. Just the core path from trigger to document.

- `[ ]` `sane-runtime` container, Debian slim base, with SANE and
   the Kodak i1120 verified working
- `[ ]` `scan-bridge` daemon in Go: `POST /scan`, `GET /health`,
   `GET /profiles` endpoints
- `[ ]` `scan-processor` container, basic ImageMagick wrapper,
   atomic NFS write
- `[ ]` Bash bootstrap script that installs Docker, mounts NFS,
   loads udev rules, pulls images
- `[ ]` Reference compose stack for Topology B (NFS direct) as the
   simplest start
- `[ ]` Documentation: getting started, hardware setup, first scan
- `[ ]` Tilt configuration for local development
- `[ ]` GitHub Actions for multi-arch container builds, push to GHCR
- `[ ]` Renovate configuration for dependency updates
- `[ ]` First blog post published, announcing the launch

**Definition of done for Phase 1:** A user with a fresh Pi, a Synology
NAS, and a Paperless-ngx instance can run the bootstrap script, send a
single curl request, and see a PDF in Paperless within 30 seconds.

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
