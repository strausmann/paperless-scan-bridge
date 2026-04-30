# Concept Document — paperless-scan-bridge

> **Status:** Draft v1.0
> **Last updated:** 2026-04-30
> **Author:** Björn Strausmann
> **Audience:** Anyone deciding whether to use, contribute to, or fork
> this project. Also future-me when I have forgotten why a particular
> decision was made.

## Purpose of this document

This is the master concept document. Its job is to answer four
questions in one place:

1. **What** is this project, in language both technical and
   non-technical readers can understand
2. **Why** does it exist — what problem does it solve, and why is the
   existing landscape insufficient
3. **How** does it work at a level above implementation but below
   marketing — the actual decisions and their reasoning
4. **What is at risk** — known limitations, open questions, things
   that could go wrong

Detailed specifications live elsewhere: technical architecture in
`ARCHITECTURE.md`, the implementation phases in `ROADMAP.md`, the
threat analysis in `THREAT_MODEL.md`, the recovery procedures in
`DISASTER_RECOVERY.md`. This document is the umbrella under which
those live.

If you read only one document in this repository, read this one.

---

## Table of contents

1. [Executive summary](#1-executive-summary)
2. [Project context and motivation](#2-project-context-and-motivation)
3. [Vision and goals](#3-vision-and-goals)
4. [Scope and non-goals](#4-scope-and-non-goals)
5. [Target users](#5-target-users)
6. [Core use cases](#6-core-use-cases)
7. [Architectural concept](#7-architectural-concept)
8. [Technology decisions](#8-technology-decisions)
9. [Component overview](#9-component-overview)
10. [Data flow and lifecycle](#10-data-flow-and-lifecycle)
11. [Storage strategy](#11-storage-strategy)
12. [Security concept](#12-security-concept)
13. [Operations concept](#13-operations-concept)
14. [Quality concept](#14-quality-concept)
15. [Documentation concept](#15-documentation-concept)
16. [Community and contribution model](#16-community-and-contribution-model)
17. [Phased delivery plan](#17-phased-delivery-plan)
18. [Risks and open questions](#18-risks-and-open-questions)
19. [Decision log](#19-decision-log)
20. [Glossary](#20-glossary)

---

## 1. Executive summary

`paperless-scan-bridge` is an open-source, container-first software
stack that turns any SANE-compatible USB document scanner attached to
a Raspberry Pi into a hands-free ingestion pipeline for Paperless-ngx.
Documents are stored on a Synology NAS, leveraging existing backup
and snapshot infrastructure.

The project distinguishes itself from the dozens of fragmentary
scanner-and-Paperless tutorials by being a complete, production-grade
reference implementation. It ships three custom container images, a
minimal Pi bootstrap script, importable Home Assistant blueprints,
n8n workflow exports, restic-based backup, Prometheus monitoring,
threat modelling, and disaster recovery runbooks. Everything is MIT
licensed.

The reference platform is a Kodak ScanMate i1120 on a Raspberry Pi 5
with Ubuntu Server 24.04 arm64, but any SANE-supported sheet-fed
scanner with ADF works. Multi-profile scanning (private/business,
duplex/simplex, color/grayscale) is selectable through three trigger
paths: scanner hardware buttons, Zigbee remotes via Home Assistant,
or HTTP webhooks from any system.

The project is delivered in four phases over an estimated 14-18
weeks of part-time work. Phase 0 (foundation documents) completes at
launch. Phases 1-4 add the working stack, trigger paths, production
hardening, and ecosystem maturity respectively. Maintenance continues
indefinitely, driven by community demand.

### Key facts at a glance

| Attribute             | Value                                              |
| --------------------- | -------------------------------------------------- |
| License               | MIT                                                |
| Primary language      | Go (daemon, processor); Bash (bootstrap)           |
| Runtime model         | Container-first; Pi runs only Docker, NFS, udev    |
| Documentation         | Zensical, multi-language (EN primary, DE secondary)|
| Reference scanner     | Kodak ScanMate i1120                               |
| Reference platform    | Raspberry Pi 5 + Ubuntu Server 24.04 arm64         |
| Storage hub           | Synology NAS via NFS (3 topologies supported)      |
| Trigger sources       | HTTP webhook, Zigbee+HA, scanner hardware buttons  |
| Container registry    | GitHub Container Registry (ghcr.io)                |
| Documentation hosting | GitHub Pages, custom domain `scan-bridge.strausmann.de` |
| CI/CD                 | GitHub Actions                                     |

---

## 2. Project context and motivation

### 2.1 The problem space

Paperless-ngx is the de facto open-source document management system
for self-hosters. Tens of thousands of installations exist worldwide,
running on home servers, small NAS appliances, and Kubernetes
clusters. The software is mature, feature-complete, and actively
maintained.

Paperless-ngx has, however, a structural weakness that is rarely
discussed openly: **it has no native scanner integration.** The
project assumes that documents arrive in the consume directory by
some external mechanism. That mechanism is left as an exercise to
the user.

The most common paths users actually take, in rough order of
prevalence:

1. **Network multifunction printer with SMB/SFTP target.** Scan to a
   network share that is also the Paperless consume folder. Works,
   but requires a multifunction device with this capability and a
   constantly running SMB target. Also: most of these scanners cap
   out at simplex or are slow on duplex.

2. **Email to Paperless.** Scan to email, Paperless polls the mailbox
   and ingests attachments. Works, but adds an email round-trip,
   requires a dedicated mailbox, and is slow.

3. **Manual scan on a Windows or macOS workstation, drag to a synced
   folder.** Works for occasional use, breaks the moment the
   workstation is off or asleep, and ties scanning to one machine.

4. **Mobile phone with Document Scanner apps.** Works for ad-hoc
   single pages, fails for batch scanning, multi-page documents, or
   anything requiring an ADF.

5. **Custom scripts on a Raspberry Pi connected to a USB scanner.**
   This is the path I took. It is the most flexible path, the most
   future-proof path, and the most painful path.

The fifth path is the one this project is about. The pain comes from
how fragmented the available knowledge is. There are individual blog
posts about SANE on a Pi. There are forum threads about scanbd
configuration for specific scanners. There are GitHub gists with
shell scripts that work for the author's setup. There is no single
project that takes a person from a fresh Raspberry Pi image to a
fully working, monitored, backed-up scanner pipeline.

### 2.2 Personal motivation

This project started because I have a Kodak ScanMate i1120 on my
desk in Maschen and a Paperless-ngx instance on my homelab server.
The i1120 is a sixteen-year-old desktop scanner with excellent
hardware — sturdy paper feeder, good optics, surprisingly fast for
its age — but its Linux support is maintained by exactly nobody. It
works under SANE through the `avision` backend, but the configuration
is non-obvious, the documentation is forty different posts in twelve
languages, and the SANE backend itself is marked "unmaintained" since
2020.

I spent most of February 2026 wiring this together. My notes filled
sixteen pages. Many of the dead ends I hit were ones other people had
hit before me, written about in scattered places, and then moved on
from without leaving a coherent trail. By March I had a working
setup. I realized that the most valuable thing I had built was not
the running stack — it was the body of decisions, dead ends, and
working configurations.

Hence this project. Other people should not have to spend a month on
this if they have similar hardware and similar needs.

### 2.3 Why now

Three independent timing factors made this the right project to start
in 2026.

**The Paperless-ngx ecosystem matured.** Until late 2024, Paperless
itself was still settling. By early 2026, the core has stabilized,
the workflow system is solid, the OCR pipeline is reliable, and the
configuration surface is well-documented. Building on top of it now
is building on a stable platform, not a moving target.

**The container ecosystem on Pi reached parity.** Docker on ARM64 is
no longer an exotic configuration. Multi-arch images are routine.
Compose v2 supports the patterns we need. Building a container-first
stack on a Pi in 2026 is genuinely as straightforward as on x86.

**Zensical replaced MkDocs Material.** As of November 2025, the
Material for MkDocs project entered maintenance mode and Zensical
launched as its successor. Starting fresh in 2026 means starting on
the platform of the next five years, not on one with a known
end-of-life.

### 2.4 Why open source, why MIT

The MIT license is deliberately chosen for maximum permissiveness.
The aim is for this project to be useful to anyone — homelabbers,
small businesses, schools, libraries. There are no commercial
extensions planned, no dual licensing, no copyleft requirements. The
project explicitly does not depend on Insiders, sponsorware, or
proprietary tooling.

Open source also serves a personal goal: I want this work to outlast
my interest in it. When I eventually move on to other projects (and
I will, that is the nature of side projects), other people should
be able to pick it up without negotiating licensing or ownership.

---

## 3. Vision and goals

### 3.1 Vision statement

A person walks into their home office, places a stack of paper into
their scanner, presses one button, and walks out. Thirty seconds later
the documents are filed, tagged, OCR'd, full-text-searchable, and
backed up off-site. They never touched a keyboard.

This is not aspirational. This is what the project delivers when
fully deployed. The vision statement is descriptive of the working
system, not a future goal.

### 3.2 Primary goals

The project succeeds if it achieves all of the following:

**Goal 1: One-button scanning works reliably.**
A trigger event (Zigbee button press, scanner button press, webhook)
results in a document in Paperless-ngx within 60 seconds, with
correct metadata, in the correct profile-defined format, in the
correct directory. The reliability target is 99% — out of 100 scan
attempts, at most one fails for reasons other than user error like
paper jams.

**Goal 2: Setup time is under 60 minutes.**
A user with the documented prerequisites (Pi, scanner, NAS, Docker
host) can go from cloning the repository to a successful first scan
in under one hour. This is a measurable goal — we will track it via
user feedback.

**Goal 3: The setup survives me losing interest.**
Six months after I stop active development, the project should still
be useful. This requires: container images with reproducible builds,
documentation that does not assume my mental model, no dependencies
on personal accounts or services, and explicit handover paths for
maintenance.

**Goal 4: The community contributes hardware compatibility.**
The hardware compatibility table grows from contributors, not just
from me. Within twelve months of the public launch, the table
contains at least fifteen scanner models verified by people other
than me.

### 3.3 Secondary goals

These are nice-to-haves that we pursue when they do not conflict
with the primary goals.

- **Educational value.** The documentation should teach the user
  about SANE, scanbd, NFS, and Paperless along the way. People who
  use this project should come out understanding their stack better.
- **Reference quality.** The codebase should be of a quality that I
  would be comfortable showing in a code review or a job interview.
  Test coverage, structured logging, reasonable abstractions.
- **SEO and discoverability.** When someone searches for "Kodak i1120
  Paperless Pi" or "Synology NFS Paperless-ngx setup", this project
  should be among the first results. Not as a marketing goal but as a
  service to the people looking.

### 3.4 Anti-goals

Things this project deliberately does not try to be:

- A general-purpose DMS — that is Paperless-ngx
- A SANE distribution — that is the upstream project
- A Home Assistant fork — we ship blueprints, not a fork
- A Synology package or DSM application — DSM is a black box NFS server
- A Kubernetes operator — Compose is the reference deployment
- A scanning service for unattended photo digitization — different
  use case, different optimization

---

## 4. Scope and non-goals

### 4.1 In scope

- USB-attached document scanners with SANE support
- Sheet-fed scanners with ADF (auto document feeder)
- Duplex and simplex scanning
- Multi-profile scanning with per-profile configuration
- Color, grayscale, and lineart modes
- PDF and JPEG output formats
- Triggering via HTTP webhook, Zigbee remote (via HA), scanner
  hardware buttons (via scanbd)
- Storage on Synology NAS (NFS, optionally iSCSI LUN)
- Three storage topologies: local FS + restic, NFS direct, iSCSI LUN
- Backup with restic, off-site replication via Hyper Backup
- Monitoring with Prometheus and Grafana
- Documentation in English and German
- Production hardening: security, threat model, disaster recovery
- Hardware compatibility list maintained by the community

### 4.2 Out of scope

- Flatbed scanners for photo digitization at high resolution (we
  support them mechanically but do not optimize the pipeline for
  them)
- Network scanners that already support SMB/SFTP/email — those
  do not need this bridge
- Multi-function printers used primarily for printing
- Scanners without SANE support (Brother network scanners on ARM,
  some legacy Canon models, certain Epson WiFi devices)
- High-volume scanning at industrial scale (more than 1000 pages
  per day) — Paperless itself becomes the bottleneck before we do
- Cloud document storage as a primary backend (S3, GDrive, OneDrive)
  — Synology NAS is the reference platform, others may work but are
  not tested
- Mobile phone scanning apps — Paperless has email ingestion for that
- Active-active high availability for Paperless-ngx — cold standby
  is the documented HA model
- Multi-tenant scenarios where multiple users share one bridge but
  see different documents

The boundary between "in scope" and "out of scope" is enforced by
declining feature requests that cross it. This is one of the
hardest disciplines for an open-source project. The repository is
organized so that adding cross-boundary functionality requires
significant work, which functions as a natural filter.

---

## 5. Target users

### 5.1 Primary persona — the Self-Hosting Engineer

**Who they are:** Software developer, sysadmin, IT professional, or
sufficiently advanced hobbyist. Has a homelab. Runs Paperless-ngx,
Home Assistant, possibly Nextcloud, possibly a Synology NAS. Knows
Docker, has used Compose. Comfortable on the Linux command line.

**What they want:** A working scanner ingestion pipeline. They are
allergic to manual processes. They want to scan and forget. They
also want to understand what is happening in their stack.

**What they tolerate:** Reading documentation. Editing YAML files.
Running scripts as root when justified. Compiling things if absolutely
necessary, though they prefer not to.

**What they will not tolerate:** Cloud dependencies for core
functionality. Phone-home telemetry without explicit consent.
Configuration that only works on one specific operating system or
hardware vendor. Closed-source binaries dropped into their stack.

**How this project serves them:** Container-first stack means no
host pollution. Compose-based deployment is familiar. Three storage
topologies means they can adapt to their actual setup. Detailed
documentation lets them understand and modify what they deploy.

### 5.2 Secondary persona — the Curious Power User

**Who they are:** Technically inclined, comfortable with tools, not
a software engineer. Maybe a teacher, a researcher, a small-business
owner, a freelance professional. Has heard of self-hosting,
respects it, dabbles in it.

**What they want:** A working setup that does what they need without
demanding deep technical investment. Will follow a tutorial. Will
not write code.

**How this project serves them:** Quickstart guide that gets them
from zero to working in under an hour. Bootstrap script that does
the right thing without requiring them to understand every step.
Sensible defaults. A community of people who have done this before
and can answer questions.

### 5.3 Tertiary persona — the Forking Developer

**Who they are:** Another developer building a related project.
Maybe they want a scanner ingestion path for a different DMS
(Mayan EDMS, Teedy, Open-Paperless). Maybe they want to extract the
SANE-in-Docker pattern for their own use. Maybe they want to use this
as a starting point for a commercial product.

**What they want:** Clear architecture. Modular code. Unambiguous
license. Documentation that explains design decisions, not just
implementation details.

**How this project serves them:** This concept document. The
architecture decision records implicit in the decision log. The
component-level READMEs. The MIT license that explicitly permits
forking and commercial use.

### 5.4 Who this project is not for

- People who want a fully managed solution. Use a hosted DMS or a
  network scanner with built-in SaaS integration.
- People who refuse to use Docker. Without containers, the
  architecture does not work. Forking to a host-installed variant is
  a substantial undertaking.
- People scanning at industrial volumes. The Pi is a bottleneck above
  ~2000 pages per day with full OCR.
- People with regulatory requirements that prohibit self-hosting
  document storage (some legal, healthcare, or financial use cases).

---

## 6. Core use cases

These are the use cases the system is optimized for. Each is
described as a short narrative and then validated against the system
design.

### 6.1 The single document scan

**Narrative:** A bill arrives in the mail. The user opens it, walks
to the scanner, places it face-down in the ADF, presses the Zigbee
button mapped to "private documents simplex", walks back to the
mailbox to check for more mail. By the time they return, the
document is in Paperless, OCR'd, tagged "private", and visible in
the inbox view.

**What the system does:** Zigbee button → Home Assistant automation →
HTTP POST to scan-bridge → scan-bridge dispatches to sane-runtime →
sane-runtime drives the scanner → scan-processor builds the PDF →
file lands in `/mnt/synology/consume/private/` → Paperless picks it
up, OCRs it, tags it "private" via subdirectory rule.

**Latency target:** 30 seconds from button press to PDF in Paperless.
Realistic given the i1120 scans at 20 ppm and a single page takes
~3 seconds plus 10 seconds for OCR.

### 6.2 The batch scan with profile selection

**Narrative:** The user has accumulated a stack of bank statements,
private letters, and a tax return. They scan the bank statements
first using the "business duplex" profile, then private letters with
"private duplex", then the tax return with "tax archive" (which uses
600 DPI and color for higher fidelity).

**What the system does:** For each batch, the user changes the
profile selector either via the LCD on the i1120 (if hardware buttons
are configured) or via the Zigbee remote button-mapping. The
scan-bridge daemon receives the profile parameter with each request
and dispatches accordingly. Each batch lands in its own subdirectory
and gets the corresponding tag in Paperless.

**Validation:** The profile system is the central abstraction. The
i1120 LCD has nine slots; we can map at least nine profiles. The
STYRBAR has eight distinguishable button events. Either is
sufficient for a typical workflow.

### 6.3 The unattended capture

**Narrative:** The user is on a phone call. A delivery comes with
several pages of documentation. They want to scan now without
interrupting the call. They place the documents in the ADF, press
the scanner's hardware "scan" button, and continue talking. The
scanner does its job; the bridge picks up the trigger; the document
is filed.

**What the system does:** scanbd inside sane-runtime polls the
scanner buttons. Button press is detected, calls a hook script
inside the container that POSTs to scan-bridge. From there, identical
to use case 6.1.

**Validation:** Hardware buttons work for users who do not want to
add Zigbee infrastructure. They are the lowest-friction trigger path
for "I am physically at the scanner anyway".

### 6.4 The remote scan

**Narrative:** The user is downstairs. A document is upstairs in the
office. They walk up, load the document, walk back down, and trigger
the scan from their phone via Home Assistant.

**What the system does:** HA app on phone → automation that POSTs
to scan-bridge → standard pipeline.

**Validation:** This use case validates that the trigger path is
fully decoupled from physical proximity. The same mechanism that
serves the in-the-room user serves the elsewhere-in-the-house user.

### 6.5 The recovery scenario

**Narrative:** The Docker host has a hard drive failure. The user
provisions a new host, runs the bootstrap, restores from restic, and
is back in business in 90 minutes. No documents lost, no metadata
corrupted, no manual reconstruction.

**What the system does:** Topology A (local FS + restic) means the
backup is explicit, well-tested, and verifiable. PostgreSQL dumps
are part of the snapshot. The restore runbook is documented and
periodically test-run.

**Validation:** This use case validates the production-grade claim.
A system that cannot recover is not production-grade regardless of
how clever its trigger paths are.

---

## 7. Architectural concept

A short summary; the full detail is in `ARCHITECTURE.md`.

### 7.1 Three layers

**Trigger layer** — the entry points. HTTP webhooks, Home Assistant
automations, scanbd hooks. All ultimately call the same REST
endpoint on the bridge.

**Processing layer** — the bridge daemon, the SANE runtime, the
processor. Three containers, each with one responsibility.

**Storage and consumer layer** — the consume directory on the
Synology, Paperless-ngx as the consumer, restic as the backup
mechanism, Prometheus/Grafana as the observer.

### 7.2 Container-first

The Pi host runs only Docker, an NFS mount, and udev rules. No SANE
on the host. No scanbd on the host. No language runtimes on the
host. This is enforced by the bootstrap script, which installs
exactly four things:

1. Docker Engine + Compose plugin
2. nfs-common + cifs-utils
3. The udev rules file
4. A systemd unit for the NFS mount

Everything else lives in containers. This makes the Pi disposable —
flash an SD card, run bootstrap, you are back online in 15 minutes.

### 7.3 Storage-flexible

Three storage topologies are supported, documented, and tested. The
user picks one based on their setup and priorities. None is
"correct" in absolute terms; each has a profile of trade-offs.

The compose stacks are organized so changing topology is a config
change, not a code change. The scan-bridge daemon does not know or
care which topology is in use; it writes to a path, and the
operator decides what is on the other end of that path.

### 7.4 Observable by default

Every component exports Prometheus metrics. Every component logs in
structured JSON. The bridge daemon includes a synthetic health check
mode that scans a test PDF every hour and verifies the round-trip.

This is non-optional. "It works on my machine" is not a state we
accept. Either the metrics show the system is working, or we know
something is wrong.

---

## 8. Technology decisions

This section captures the technology choices and the reasoning. Each
decision is summarized; the full discussion lives in the decision
log at the end of this document.

### 8.1 Daemon language: Go

**Chosen:** Go 1.22+
**Alternatives considered:** Node.js / TypeScript, Python, Rust
**Why Go won:** Single static binary, fast startup, small memory
footprint, trivial ARM64 cross-compilation, mature HTTP and JSON
libraries, excellent PDF library ecosystem (pdfcpu), low operational
complexity. The container weighs ~25 MB.
**Why not Node.js:** Container would be 200+ MB. Runtime dependencies
are a maintenance burden. ARM64 builds occasionally break. Excellent
fit for web UIs, poor fit for systems daemons.
**Why not Python:** Same container size issue. Distribution is more
fragile (virtualenvs, system packages, version conflicts). Excellent
for scripts and prototypes, weak for long-running services.
**Why not Rust:** Best technical fit. Rejected because the maintainer
(me) is more productive in Go, and the time-to-first-feature matters
more than the marginal performance improvement Rust offers.

### 8.2 Documentation engine: Zensical

**Chosen:** Zensical (pip-installable, Rust core, Python bindings)
**Alternatives considered:** Hugo, MkDocs Material, Docusaurus,
Sphinx
**Why Zensical won:** It is the explicit successor to MkDocs Material
by the same team. MIT licensed, multi-language native, fast builds,
mkdocs.yml compatible (so existing patterns transfer). Starting fresh
in 2026 means starting on the platform with the longest viable life.
**Why not MkDocs Material:** Project entered maintenance mode in
November 2025. End of feature life November 2026. Starting a new
multi-year project on a sunsetting platform makes no sense.
**Why not Hugo:** Theme ecosystem is fragmented for documentation
sites. No built-in multi-language model that matches Zensical's. No
strong default appearance for technical documentation.
**Why not Docusaurus:** Node.js stack. Adds toolchain complexity that
does not match the rest of the project's container-first ethos.
**Why not Sphinx:** Excellent for Python API docs. Heavy for general
technical writing. Steeper learning curve than the others.

### 8.3 Documentation hosting: GitHub Pages with custom domain

**Chosen:** GitHub Pages, custom domain `scan-bridge.strausmann.de`
**Alternatives considered:** GitLab Pages, self-hosted, Cloudflare Pages
**Why GitHub Pages won:** Repository is on GitHub. CI/CD is GitHub
Actions. Pages deployment is a single workflow step. No additional
service to maintain. Custom domain is free and well-supported.
**Why not self-hosted:** Documentation site needs to be available
even when my homelab is down. Self-hosting it would create a
circular dependency.

### 8.4 Container registry: GitHub Container Registry

**Chosen:** ghcr.io
**Alternatives considered:** Docker Hub, Quay, self-hosted Harbor
**Why GHCR won:** Native to GitHub, no separate auth, no rate limits
for public images, signed images via cosign supported, multi-arch
manifest works out of the box.
**Why not Docker Hub:** Rate limits on free tier are restrictive for
public projects that get popular. Pull-through caching is a hassle.

### 8.5 Backup tool: restic

**Chosen:** restic
**Alternatives considered:** Borg, Kopia, rsnapshot, plain rsync,
Synology Hyper Backup directly
**Why restic won:** Deduplication (incremental backups are tiny),
encryption (repository on the NAS is unreadable without the password),
well-tested restore path, integrity verification with `restic check
--read-data-subset`. Active maintenance, mature.
**Why not Borg:** Excellent tool. Restic edges it out for the use
case because of better cloud-storage compatibility (matters if a user
later wants to back up to S3) and slightly more flexible retention
policy syntax.
**Why not Hyper Backup directly:** Hyper Backup is the off-site
replication layer, not the local backup layer. Using it for both
loses the layered defense.

### 8.6 Storage default: Topology A (local FS + restic)

**Chosen:** Local filesystem on Docker host, with restic backup to NAS
**Alternatives:** NFS direct, iSCSI LUN
**Why this default:** Inotify works. Sub-second pickup for Paperless.
Backup is explicit and well-tested. Performance is best of the three.
**When to choose alternatives:** NFS direct for ultra-simple setups;
iSCSI LUN if you specifically want LUN snapshots as backup primitive.

### 8.7 CI/CD: GitHub Actions

**Chosen:** GitHub Actions
**Alternatives considered:** GitLab CI (mirror), Drone, self-hosted
Forgejo Actions
**Why GitHub Actions won:** Native to the chosen platform. Free for
public repos. Rich ecosystem of pre-built actions. Native multi-arch
container builds via QEMU.

### 8.8 Secrets: SOPS with age keys

**Chosen:** SOPS, age key recipients
**Alternatives considered:** Plain env files, HashiCorp Vault,
Bitwarden CLI integration, Doppler
**Why SOPS+age won:** Git-compatible (encrypted files in the repo
itself). No external service to maintain. age is a modern, simple
public-key cipher. SOPS handles the partial encryption (encrypt
values, not keys).
**Why not Vault:** Excellent tool. Overkill for a single-maintainer
project with no team. Adds significant operational complexity.

### 8.9 Profile config format: YAML

**Chosen:** YAML for profiles, TOML for daemon config
**Reasoning:** YAML is what most users in this space already know
(Compose files, Home Assistant, Ansible). TOML is what Go services
typically use for their own config. Splitting concerns avoids
confusion.

---

## 9. Component overview

For each component, this section gives the role, the technology, the
shipping format, and the upstream relationship. Detailed
specifications live in the per-component README files under
`components/`.

### 9.1 scan-bridge

**Role:** Core daemon. REST API, webhook receiver, profile dispatcher,
job tracker, metrics exporter.
**Technology:** Go 1.22+, standard library HTTP, BoltDB or SQLite for
job persistence, Prometheus client library.
**Shipping format:** OCI container, distroless base, statically
linked binary. Multi-arch (amd64, arm64).
**Image:** `ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:vX.Y.Z`
**Upstream relationship:** This is our code. No upstream.

### 9.2 sane-runtime

**Role:** SANE driver host. Wraps `scanimage`, `scanbd`, `sane-utils`
with a thin HTTP interface for the bridge to call.
**Technology:** Debian slim base + SANE packages + Go health check.
**Shipping format:** OCI container, multi-arch.
**Image:** `ghcr.io/strausmann/paperless-scan-bridge/sane-runtime:vX.Y.Z`
**Upstream relationship:** Wraps the SANE project. We do not modify
SANE itself; we package it for our use.

### 9.3 scan-processor

**Role:** Image-to-PDF pipeline. Deskew, blank-page detection, PDF
assembly, atomic NFS write.
**Technology:** Go, pdfcpu, optional gocv bindings for advanced image
processing.
**Shipping format:** OCI container, distroless base, multi-arch.
**Image:** `ghcr.io/strausmann/paperless-scan-bridge/scan-processor:vX.Y.Z`
**Upstream relationship:** Our code, using upstream Go libraries.

### 9.4 Paperless-ngx (adopted)

**Role:** Document management system, OCR engine, search index,
user interface.
**Shipping format:** Upstream image used directly.
**Image:** `ghcr.io/paperless-ngx/paperless-ngx:latest` (pinned in
production compose files)
**Upstream relationship:** Pure consumer. We do not fork or extend.

### 9.5 scanservjs (adopted, optional)

**Role:** Web UI for ad-hoc manual scanning.
**Shipping format:** Upstream image.
**Image:** `sbs20/scanservjs:latest` (pinned)
**Upstream relationship:** Adopted as-is. Our compose files
demonstrate how to run it alongside scan-bridge for users who want
both a button-driven and a web-driven path.

### 9.6 Supporting components

- **node-exporter** — Pi hardware metrics
- **watchtower** — automated container updates with explicit allowlist
- **(optional) cAdvisor** — container resource metrics
- **(optional) prometheus + grafana** — metrics scraping and dashboards

---

## 10. Data flow and lifecycle

### 10.1 Document lifecycle

A document moves through six stages, each with a defined location and
expected state.

1. **Physical paper** — in the user's hand, on the desk, in the ADF
2. **Raw scan output** — TIFF/JPEG batch in tmpfs inside the
   sane-runtime container
3. **Processed PDF** — in tmpfs inside the scan-processor container
4. **Consume directory entry** — atomically placed in
   `/mnt/synology/consume/<profile>/<timestamp>.pdf` (Topology B) or
   the local equivalent (Topology A, C)
5. **Paperless-managed document** — in `media/originals/` and
   `media/archive/`, with metadata in PostgreSQL
6. **Backup snapshot** — in the restic repository on the NAS, plus
   the off-site copy via Hyper Backup

The transition between stages 4 and 5 is the riskiest from a data
integrity perspective. We mitigate it with atomic writes
(`O_TMPFILE` + `linkat`) so Paperless never sees a partial file.

### 10.2 Trigger event lifecycle

A trigger event flows through the system with these waypoints:

1. **Trigger source emits an event** (button press, webhook, scanbd
   detection)
2. **Event reaches scan-bridge** as an HTTP POST with profile parameter
3. **scan-bridge validates** the request, looks up the profile, queues
   the job
4. **scan-bridge dispatches** to sane-runtime via Unix socket call
5. **sane-runtime executes** scanimage with profile parameters
6. **Raw output handed off** to scan-processor via shared volume
7. **scan-processor writes** the final PDF to the consume directory
8. **scan-bridge updates** the job status and emits a metric
9. **Job is observable** via `GET /jobs/<id>` for the trigger source
   to confirm

Failure at any step results in: error logged, job status set to
"failed", metric incremented, no retry automatically (the user
re-triggers if they want).

### 10.3 Backup lifecycle

Documents are backed up through three independent layers:

1. **Layer 1: restic snapshot** — nightly full snapshot of the consume
   directory, originals, archive, plus PostgreSQL dump. Lives on the
   Synology in a dedicated restic repository.
2. **Layer 2: Synology snapshots** — Btrfs snapshots of the volume
   that contains both the documents and the restic repository. Daily
   retention 7 days, weekly 4 weeks.
3. **Layer 3: Off-site replication** — Hyper Backup copies the restic
   repository to a remote location (second NAS, S3, encrypted USB
   disk). Weekly cadence.

Restoration scenarios are tested monthly via a CI job that pulls a
recent snapshot into an ephemeral VM and verifies Paperless starts
with the documents present.

---

## 11. Storage strategy

The full discussion is in `ARCHITECTURE.md`. This section gives the
strategic summary.

### 11.1 The Synology is the primary storage

All documents — originals, archives, backups — live on the Synology.
This is true regardless of which topology is chosen. The Synology
provides:

- Btrfs filesystem with snapshots
- Bit-rot detection via Btrfs checksums
- RAID redundancy at the disk level
- Hyper Backup for off-site replication
- Existing backup infrastructure that the user has already invested
  in

The bridge does not introduce a new storage location; it leverages
the existing one.

### 11.2 The Pi is stateless

The Pi holds no documents permanently. tmpfs is used for scratch
space during processing. The container working directories are
ephemeral. The Pi can be reflashed and rebuilt without data loss.

This is intentional. Pis fail. SD cards wear out. SSDs have shorter
lifespans than NAS drives. Treating the Pi as stateless eliminates
an entire category of operational concerns.

### 11.3 The Docker host is selectively stateful

In Topology A (local FS), the Docker host holds the live consume
directory and Paperless's working storage. The PostgreSQL database
lives here. This data is backed up nightly.

In Topology B (NFS direct), the Docker host holds only the
PostgreSQL database; the consume directory is on the NAS. Less
backup surface area.

In Topology C (iSCSI LUN), the LUN itself is on the NAS but the
mount is on the host. Snapshots happen at the LUN level on the NAS.

### 11.4 Encryption strategy

- **At rest on the NAS:** Optional. Synology supports volume
  encryption; users who need it enable it at the volume level. The
  bridge does not require it.
- **At rest in restic:** Mandatory. Restic encrypts every snapshot.
  The encryption key lives in a SOPS-encrypted file in the repo.
- **In transit between Pi and NAS:** NFSv4 with Kerberos optional.
  In a trusted LAN, plain NFSv4 is acceptable. The threat model
  documents the trade-off.
- **In transit between Docker host and NAS:** Same as above.
- **In transit between user devices and Paperless:** HTTPS via
  reverse proxy (out of scope for this project; the user's existing
  reverse proxy handles this).

---

## 12. Security concept

The full threat model is in `THREAT_MODEL.md`. This section gives the
strategic summary.

### 12.1 Trust model

- **Trusted:** The Pi, the Docker host, the Synology, the user's
  primary devices, the LAN segment they share.
- **Semi-trusted:** Home Assistant, n8n. They are LAN devices but
  are network-exposed and may be compromised through other means.
- **Untrusted:** The internet, anything outside the LAN, all
  third-party scanner manufacturers' assumed servers.

The bridge's webhook endpoint is reachable only from trusted and
semi-trusted devices via firewall rules. No internet-facing exposure
of the bridge itself.

### 12.2 Defense in depth

Five layers of defense:

1. **Network segmentation** — IoT VLAN for the Pi, separate from
   workstation VLAN. Firewall rules limit which devices can reach
   the bridge endpoint.
2. **Authentication** — webhook calls require a token; misconfigured
   tokens get rejected and logged.
3. **Authorization** — the bridge enforces profile-based
   authorization. Profiles can be marked as "admin only" if the
   threat model demands it.
4. **Encryption** — restic encrypts backups; SOPS encrypts secrets
   in the repository; HTTPS terminates at the user's reverse proxy.
5. **Auditing** — structured logs for every scan request, every job
   transition, every config reload. Logs ship to a syslog or Loki
   instance for long-term retention.

### 12.3 Supply chain security

Container images are pinned by digest in production compose files.
Watchtower has an explicit allowlist; it does not auto-update
arbitrary images. Renovate is configured to open PRs for image
updates, which a human reviews before merging.

The Go dependencies are vetted via `govulncheck` in CI. Any
vulnerability above CRITICAL severity blocks the merge.

The bootstrap script has a known SHA-256 hash, published in the
release notes. Users are encouraged to verify before running it.

### 12.4 What we do not protect against

Honest scope statement:

- **Physical attacker with access to the Pi.** Game over. The Pi
  has the NFS mount credentials.
- **Compromised Synology admin account.** Game over for the documents
  on that NAS.
- **Determined attacker on the LAN.** Substantial mitigation but not
  invincibility.
- **Quantum computer attacks on age encryption.** Not in scope; if
  this becomes practical, the entire repository's threat model is
  rewritten.

---

## 13. Operations concept

### 13.1 Deployment model

Three steps for the user:

1. **Bootstrap the Pi:** Run the install script. Installs Docker,
   mounts NFS, loads udev rules. Idempotent. Logs to a known
   location for troubleshooting.
2. **Configure the environment:** Edit `.env` and `profiles.yaml`.
   Both have well-documented examples.
3. **Bring up the stack:** `docker compose up -d`. Containers pull,
   start, register with the bridge. First scan validates the setup.

No Ansible required for the basic deployment. Optional Ansible
roles exist for users who want fleet management or who run multiple
bridges (e.g. one per office floor).

### 13.2 Update model

Three update paths:

- **Container updates** via Watchtower with allowlist. Bridge,
  sane-runtime, scan-processor get auto-updated. Paperless-ngx
  does not — it is on the manual-review list because version updates
  occasionally have breaking changes.
- **OS updates** via unattended-upgrades on Ubuntu Server. Security
  updates only.
- **Bridge updates** via the GitHub release process. Each release
  has a release note explaining what changed. Major version bumps
  document migration steps.

### 13.3 Monitoring model

Three layers:

- **Synthetic health check** — hourly test scan, alert if the
  round-trip fails.
- **Pipeline metrics** — Prometheus scraping bridge, processor,
  sane-runtime. Grafana dashboard with scan rate, error rate, queue
  depth, OCR latency.
- **Application metrics** — paperless-prometheus-exporter for
  Paperless-internal data; node-exporter for Pi hardware (CPU,
  memory, disk, USB throughput, temperature).

Alerting via Home Assistant notification chain (which the user
already has configured for other home automation alerts) or via a
chosen channel like Apprise, ntfy, or Gotify.

### 13.4 Backup and recovery model

Already detailed in section 10.3 and `DISASTER_RECOVERY.md`. The
key operational principle: **every backup is verified by a periodic
restore test.** A backup that has not been restored is a backup that
might not work.

### 13.5 Capacity planning

For a typical home setup:

- 1 GB / month of documents at 300 DPI grayscale
- 5 GB / month at 600 DPI color
- Five years' growth: ~60-300 GB depending on resolution
- restic deduplication brings backup size to ~1.2x the original
- Synology with a 4-bay NAS at 4 TB drives in SHR-1 has 12 TB
  usable. Comfortable for ten years.

For a small business setup: scale linearly, but consider a Pi 5 with
8 GB RAM and an external SSD via USB 3.0 to avoid bottlenecks.

---

## 14. Quality concept

### 14.1 What "production-grade" means here

I make a strong claim with that label. Here is what backs it up:

- **Tested code** — Go components have unit tests with coverage
  targets. Integration tests verify the full pipeline against a
  mocked SANE scanner.
- **Verified backups** — monthly automated restore test in CI.
- **Documented threat model** — explicit `THREAT_MODEL.md`.
- **Documented disaster recovery** — explicit `DISASTER_RECOVERY.md`
  with a procedure that has been executed at least once on real
  hardware.
- **Observability built in** — structured logs, Prometheus metrics,
  health checks. Not bolted on as an afterthought.
- **Security hardening as code** — Ansible roles, CrowdSec collections,
  unattended-upgrades configuration are all in the repo.
- **Reproducible builds** — container images pinned by digest;
  Renovate manages updates.
- **A real maintainer** — a human, with a real name, who runs the
  stack on their own scanner and uses it daily.

If any of these is missing, the "production-grade" label is wrong
and we drop it. We do not water down the meaning.

### 14.2 Test strategy

Three layers, each catching different defects:

- **Unit tests** — fast, focused, run on every commit.
- **Integration tests** — verify component interactions. Run on every
  PR. Slower (~minutes).
- **End-to-end tests** — full stack against mocked hardware. Run on
  every release tag. Slowest (~5-10 minutes).

Plus:

- **Linting** — shellcheck, golangci-lint, hadolint, yamllint,
  markdownlint. Pre-commit hooks enforce locally; CI enforces on the
  server.
- **Security scanning** — govulncheck, trivy on container images.
- **Documentation tests** — broken link checker on the docs site
  build.

### 14.3 Performance targets

- **First scan latency** (button press to PDF in Paperless) — under
  60 seconds for a single page, under 90 seconds for a 10-page batch.
- **Sustained throughput** — 200 pages per hour on a Pi 5 (limited
  by the i1120 mechanics, not the software).
- **Bridge daemon memory footprint** — under 64 MB resident at
  steady state.
- **Container startup time** — under 10 seconds from `docker run` to
  ready.

These targets are validated in the integration tests and tracked over
time as part of the CI metrics.

---

## 15. Documentation concept

### 15.1 Documentation as code

All documentation lives in the repository, in version control,
reviewed via PR. The site is generated from these sources at build
time. There is no documentation that lives only in someone's head or
in a private wiki.

### 15.2 Documentation languages

English is primary. German is secondary. Other languages are welcome
via PRs but maintained by their contributors.

The choice of two languages reflects two audiences:

- **English:** maximum global reach, GitHub norm, technical lingua
  franca
- **German:** my native language, my immediate community, the DACH
  region as a target audience for Paperless-ngx tutorials (where
  German content is comparatively scarce)

### 15.3 Documentation types

Following Daniele Procida's documentation system framework, we
maintain four types:

- **Tutorials** — learning-oriented. "Your first scan in fifteen
  minutes." Short, opinionated, gives the user a working setup.
- **How-to guides** — task-oriented. "How to add a new scan
  profile." Specific, focused, assumes the user knows the basics.
- **Reference** — information-oriented. API specs, configuration
  schemas, hardware compatibility table. Dry, complete, accurate.
- **Explanation** — understanding-oriented. This document, the
  architecture deep-dive, the threat model. Long-form, narrative,
  explains the "why".

Each piece of documentation is one of these four. Mixing types
reduces clarity.

### 15.4 Documentation maintenance

- New features must include documentation updates in the same PR
- Documentation changes that affect interfaces must include a
  CHANGELOG entry
- Broken links are CI failures
- Screenshots are versioned with the documentation; outdated
  screenshots are bugs
- Translations may lag the primary language; this is acceptable as
  long as the translation timestamp is visible in the page metadata

---

## 16. Community and contribution model

### 16.1 Stewardship

This is a single-maintainer project at launch. The maintainer is me,
Björn Strausmann. Decision-making is centralized for simplicity. As
the project grows, the contribution model can evolve.

The intent is not to remain a one-person project forever. The intent
is to grow contributors organically, to document decisions clearly
enough that someone else could pick up the project, and to ensure
that the licensing and architecture allow for that handover.

### 16.2 Contribution paths

In rough order of accessibility:

1. **Hardware compatibility reports** — anyone with a scanner can
   contribute. Lowest barrier to entry.
2. **Bug reports** — anyone who encounters a problem.
3. **Documentation improvements** — fix typos, add examples, clarify
   confusing sections.
4. **Translations** — German is secondary; other languages are open.
5. **Bug fixes** — code contributions for known issues.
6. **Feature additions** — code contributions for new capabilities.
   These should be discussed in an issue first.

### 16.3 Decision process

Small decisions (typo fixes, dependency updates, hardware compat
entries) are merged by the maintainer at their discretion.

Architectural decisions are discussed in an issue. The maintainer
makes the final call. Disagreement does not block — the discussion
is documented, the decision is made, and the issue is closed.

Breaking changes require:

1. An issue describing the change and its rationale
2. A migration guide drafted before the change is merged
3. A version bump that signals the breaking change clearly
4. A deprecation period if at all possible

### 16.4 Communication channels

- **GitHub Issues** — bugs, feature requests, hardware reports
- **GitHub Discussions** — open-ended questions, ideas, "show and
  tell" of community setups
- **PR review** — code-level feedback
- **Email to the maintainer** — security disclosures only (per
  SECURITY.md)

Deliberately not used:

- Discord or Slack — synchronous channels create exclusivity and
  fragment knowledge
- Twitter/X — too noisy, not a long-term archive
- Closed forums — defeats the purpose of an open project

---

## 17. Phased delivery plan

Already detailed in `ROADMAP.md`. The strategic summary:

- **Phase 0** (in progress): Foundation documents, repository
  structure, license, branding. Estimated 2 weeks.
- **Phase 1** (planned): Minimum viable stack with three containers,
  bootstrap script, basic compose, first working scan. Estimated 3-4
  weeks.
- **Phase 2** (planned): Trigger paths — Home Assistant blueprints,
  n8n workflows, scanbd integration, scanservjs adoption. Estimated
  3-4 weeks.
- **Phase 3** (planned): Production hardening — backup, monitoring,
  security, automated updates, disaster recovery. Estimated 5-6 weeks.
- **Phase 4** (planned): Maturity — hardware compatibility expansion,
  community contribution flow, lessons-learned retrospective.
  Estimated open-ended, multi-month.

Total estimated effort: 14-18 weeks of part-time work for the
maintainer, then ongoing community-driven evolution.

---

## 18. Risks and open questions

Honest list of things that could go wrong, and where I do not have
all the answers yet.

### 18.1 Technical risks

**The Kodak ScanMate i1120 LCD profile counter may not be exposed via
SANE.** I documented this in the earlier conversations. The
`avision` backend should expose it via the `--message` option, but
this is not verified for the i1120 specifically. Mitigation: the
Zigbee button trigger path works regardless of scanner button
support, so the project succeeds even if hardware-button-driven
profile selection does not.

**USB device permissions in containers are fragile.** Different host
distributions have different udev behaviors. The udev rule we ship
works on Ubuntu Server 24.04; it should work on Debian 12 and
Fedora; it has not been tested on every distribution. Mitigation:
the test matrix covers Ubuntu 22.04, Ubuntu 24.04, and Debian 12;
other distributions are best-effort.

**NFS performance can be unpredictable on some Synology models.**
Especially on older or low-RAM models, NFS with many small files
can stutter. Mitigation: Topology A (local FS) is the default
specifically because it isolates Paperless from NFS performance
quirks.

**SANE is unmaintained for some scanner backends.** The `avision`
backend that the i1120 uses is marked unmaintained. If a kernel
update breaks it, we have to fork it or work around it. Mitigation:
documented, expected, the project's value is not tied to one
backend.

### 18.2 Operational risks

**Single maintainer.** If I lose interest, get hit by a bus, or
become unable to maintain the project, it could go stale. Mitigation:
extensive documentation including this concept document, AGENTS.md
for AI tools, a clear license that allows others to fork.

**SD card failure on the Pi.** Pis with SD card boot fail eventually.
Mitigation: documentation recommends booting from SSD over USB 3.0,
and the cold-standby pattern means recovery is fast even if the SD
fails.

**Synology NAS failure or theft.** A single-NAS setup loses data to
a single failure. Mitigation: off-site backup is mandatory in the
production-grade pattern. Hyper Backup to a remote location, restic
to a remote location, or both.

**Container registry outage.** If GHCR is down, new deployments
cannot pull images. Mitigation: existing deployments continue to
work. Container images can be cached locally. For long-running
production setups, mirror to a private registry.

### 18.3 Strategic risks

**Zensical is new and may evolve in breaking ways.** Released
November 2025, still in active development. We bet on it but accept
that mid-project changes may require rework. Mitigation: the
documentation source is plain Markdown, not Zensical-specific. A
migration to another tool would be possible.

**Paperless-ngx changes its API.** Workflow triggers, consume
folder behavior, environment variables — all are subject to change.
Mitigation: pin Paperless-ngx version in production compose files.
Test against new versions before recommending them.

**Home Assistant changes its blueprint format.** Has happened
before, will happen again. Mitigation: blueprints are simple YAML;
when HA changes the format, we update the blueprints.

**Hardware vendor support disappears.** Kodak Alaris could discontinue
the i1120 line entirely (already discontinued, but parts could
disappear). Mitigation: the project supports any SANE-compatible
scanner, not just the i1120. Reference platform changes are easy.

### 18.4 Open questions

These are unresolved at the time of this writing. They will be
resolved as the project progresses, with answers documented in the
decision log.

- **Q1:** Should the bridge daemon support multiple scanners on one
  Pi? Currently the assumption is one scanner, one Pi. Multi-scanner
  setups are theoretically possible but add complexity to profile
  management.
- **Q2:** Should we ship a Helm chart for Kubernetes users? Compose
  is the reference deployment. Helm would expand reach but doubles
  the maintenance surface.
- **Q3:** Should we provide a hosted instance for evaluation? It
  would lower the barrier for curious users but contradicts the
  self-hosting ethos.
- **Q4:** What is the right disposition for OCR — should the bridge
  do it, or should Paperless? Currently Paperless. This is faster
  and simpler. The trade-off is that the consume directory contains
  un-OCR'd PDFs briefly.
- **Q5:** Should profile definitions be stored in a database or
  remain in YAML? YAML is simpler and works with git. A database
  would allow runtime modification via API. Currently YAML. This
  may revisit in Phase 4.

### 18.5 What I do not know yet

Honest gaps in my own understanding that affect the project:

- The exact behavior of `scanbd` with the i1120's profile counter is
  empirical. I have not yet wired this up; I am working from the
  SANE documentation and analogies to other scanners.
- The performance characteristics of restic on a Synology DS920+ at
  scale (~1 TB repository) are not well-documented. I will measure
  this in Phase 3.
- The migration path from MkDocs Material to Zensical for projects
  with `mike` versioning is documented but not yet practiced. We
  will hit this in Phase 1 documentation work.

---

## 19. Decision log

A chronological record of significant decisions, their context, and
their reasoning. New entries are appended; existing entries are not
edited (only superseded by later entries).

### ADR-001 — Container-first architecture

**Date:** 2026-04-25
**Status:** Accepted
**Context:** Initial discussion of how much to install on the Pi
host versus in containers. Existing tutorials install SANE, scanbd,
ImageMagick directly on the host. This makes the Pi non-disposable
and the configuration brittle.
**Decision:** Install only Docker, NFS mount, and udev rules on
the host. Everything else in containers.
**Consequences:** Larger initial complexity (three containers to
build). Lower long-term complexity (Pi is disposable). Better
separation of concerns. Higher reproducibility.

### ADR-002 — Go for the daemon, not Node.js

**Date:** 2026-04-26
**Status:** Accepted
**Context:** The maintainer is comfortable in both Go and TypeScript.
Node.js has more contributors in the open-source self-hosting
community.
**Decision:** Go for the daemon. Discussed in detail in section 8.1.
**Consequences:** Smaller container, faster startup, fewer
dependencies. Marginally fewer potential contributors who know Go
well, but Go is a small language and easy to learn. Net positive.

### ADR-003 — Zensical instead of MkDocs Material

**Date:** 2026-04-30
**Status:** Accepted
**Context:** Initial proposal was MkDocs Material. User pointed out
that MkDocs Material entered maintenance mode in November 2025 and
that the team is shipping Zensical as the successor.
**Decision:** Adopt Zensical from the start, despite its newer status.
**Consequences:** Some Zensical features may not be in feature parity
with MkDocs Material yet. We may hit edge cases. But: starting fresh
on the platform of the next 5+ years is better than starting on a
sunsetting platform and migrating later.

### ADR-004 — MIT license

**Date:** 2026-04-25
**Status:** Accepted
**Context:** License options: MIT, Apache 2.0, BSD-3, GPL, AGPL.
**Decision:** MIT.
**Consequences:** Maximum permissiveness. Anyone can use it for
anything, including commercial redistribution. We forgo any copyleft
protection. We accept this because the project's goal is utility,
not enforcement.

### ADR-005 — Synology NAS as primary storage

**Date:** 2026-04-29
**Status:** Accepted
**Context:** Where do documents live? Options: local on Docker host,
on the Synology, on cloud storage, on dedicated storage servers.
**Decision:** Synology NAS as primary. Local FS as cache layer in
Topology A.
**Consequences:** The user must have a Synology (or compatible NAS).
This narrows the audience but matches the homelab norm. Other NAS
vendors with NFS support work but are not tested.

### ADR-006 — Three storage topologies, document them all

**Date:** 2026-04-30
**Status:** Accepted
**Context:** Local FS gives inotify; NFS direct gives simplicity;
iSCSI LUN gives both inotify and snapshot-based backup but with
single-host constraint.
**Decision:** Support all three. Default recommendation is Topology
A. Document all three with their trade-offs.
**Consequences:** More documentation work. More compose file
variants. But genuinely serves users with different setups.

### ADR-007 — Scope strictly bounds external project boundaries

**Date:** 2026-04-30
**Status:** Accepted
**Context:** The temptation is to bundle related functionality.
"While we're at it, let's add a barcode generator." This bloats
projects and makes them harder to maintain.
**Decision:** Hard scope boundaries. We are not Paperless. We are
not SANE. We are not Home Assistant. We bridge them.
**Consequences:** We refuse some feature requests. Some users go
elsewhere. The project remains comprehensible.

### ADR-008 — No dependency on customer or employer context

**Date:** 2026-04-30
**Status:** Accepted
**Context:** The maintainer works at an MSP with various customer
projects. Conflating personal homelab work with employer-related
work creates legal and ethical risk.
**Decision:** This project is strictly homelab. No examples,
terminology, or scenarios from any commercial or customer context.
**Consequences:** Documentation tone is personal, not corporate.
Good for community; clear separation from any employer.

---

## 20. Glossary

Terms used throughout this document and the repository.

**ADF** — Automatic Document Feeder. The tray on a scanner that
holds multiple sheets and feeds them through automatically.

**Bridge** — Short for `paperless-scan-bridge`. The whole project,
or specifically the `scan-bridge` daemon depending on context.

**Compose** — Docker Compose v2. The orchestration tool used
throughout for multi-container deployments.

**Consume directory** — The directory Paperless-ngx watches for new
documents. New files placed here are ingested automatically.

**Container-first** — Architectural principle: all functional
software lives in containers; the host runs only Docker and minimal
infrastructure (mounts, udev, networking).

**DPI** — Dots Per Inch. Scanner resolution. Common values: 200,
300, 600.

**Duplex** — Scanning both sides of a sheet in one pass. Requires a
duplex-capable ADF.

**GHCR** — GitHub Container Registry. `ghcr.io`. Where this project
publishes its container images.

**Inotify** — Linux kernel API for filesystem event notifications.
Works on local filesystems; does not work on NFS.

**LUN** — Logical Unit Number. An iSCSI block-storage unit presented
by the Synology and mounted as a filesystem on a Docker host.

**OCR** — Optical Character Recognition. Converting scanned images
to searchable text.

**Profile** — A named configuration for a scan operation:
resolution, color mode, source, target directory, format. The user
selects a profile when triggering a scan.

**SANE** — Scanner Access Now Easy. The Linux scanner driver
framework. `sane-utils` provides `scanimage`. `scanbd` polls for
scanner button presses.

**SOPS** — Secrets OPerationS. Mozilla tool for encrypting secrets
in Git repositories using key files (age, GPG, AWS KMS).

**Topology** — One of three storage configurations: local FS with
restic backup (A), NFS direct (B), iSCSI LUN (C).

**Trigger** — An event that initiates a scan. Three trigger
sources: HTTP webhook, Zigbee remote via Home Assistant, scanner
hardware button via scanbd.

**udev** — Linux device manager. Used here to create stable USB
device paths for the scanner regardless of which USB port it is on.

**Zensical** — The static site generator used for the documentation
site. Successor to MkDocs Material.

---

*This concept document is alive. As the project evolves, this
document evolves with it. Significant decisions get added to the
decision log. Unresolved questions get resolved and moved out of
the open-questions list. Risks get mitigated and crossed off.*

*If you read this document and something is unclear, that is a
documentation bug. Please open an issue.*
