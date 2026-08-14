---
template: home.html
title: "paperless-scan-bridge — a hands-free scanner pipeline for Paperless-ngx"
hide:
  - navigation
  - toc
---

## Why this exists

Paperless-ngx has no native scanner integration. There are dozens of
fragmentary tutorials for parts of this stack — SANE on a Pi,
Paperless-ngx with NFS, scanner buttons via scanbd, Zigbee automation in
Home Assistant. There is no single repository that walks you from a
fresh Pi image to a fully production-grade scan pipeline with backup,
monitoring, and security hardening.

This repository fills that gap. It is also a living record of turning a
**Kodak ScanMate i1120** — a sixteen-year-old desk scanner without modern
Linux drivers — into a hands-free part of a homelab. If it can be made
to work, most SANE-supported ADF scanners can too.

## Three containers. No host installs.

The Pi's job is Docker, an NFS mount, and udev rules — nothing else.
Every real piece of work happens inside one of these three images, which
hand off to your existing Paperless-ngx instance.

<div class="mdx-grid" markdown="1">
- **`scan-bridge`** *(Go)* — REST API, profile dispatch, Prometheus
  metrics. Receives the trigger — hardware button, Zigbee, or webhook —
  and starts the job.
- **`sane-runtime`** *(Bash + Go)* — SANE drivers and udev integration for
  stable USB device paths. Drives the physical scanner.
- **`scan-processor`** *(Go)* — deskews, filters blank pages, assembles
  the PDF, writes it atomically to the consume directory over NFS.
- **`paperless-ngx`** *(upstream)* — picks the PDF up from its consume
  folder, OCRs it, tags it by profile. Runs wherever you already run it.
</div>

## Three ways to say "scan this"

The trigger path is fully decoupled from physical proximity. The same
mechanism that serves someone standing at the scanner serves someone two
floors away on their phone.

<div class="mdx-grid" markdown="1">
- **Hardware button** *(planned)* — scanbd polling the scanner's own
  buttons. `sane-runtime`'s own README documents scanbd as intentionally
  out of scope for that module so far; this path is designed, not built.
- **Zigbee remote** *(planned)* — a STYRBAR button mapped through Home
  Assistant, one button event per scanning profile.
  `homeassistant/blueprints/` is scaffolding only, no blueprint yet.
- **HTTP webhook** — a real, bearer-token-protected `POST /scan` on the
  `scan-bridge` daemon today: dispatches to `sane-runtime`, then
  `scan-processor`, then delivery — from a phone shortcut, a script, or
  any other system on the network.
</div>

## Nothing on the host but Docker

!!! warning "Not yet runnable"

    The bootstrap script (`deploy/bootstrap/install.sh`) and the compose
    stacks under `deploy/compose/` shown below are Phase 1 deliverables
    and are not in the repository yet — both directories currently hold
    only a `.gitkeep`. This is the intended flow, kept here so the shape
    of the setup is reviewable before the code lands, the same framing
    the real [Quickstart guide](getting-started/quickstart.md) uses.
    The Phase 1.2 core (`scan-bridge` + `sane-runtime` + `scan-processor`)
    does already exist and is wired together by the repository-root
    `compose.yaml` — see "Where it actually stands" below for what that
    covers and what it does not yet.

The bootstrap script edits `/etc/fstab` and udev rules as root — download
and read it before you run it.

```bash
# on the Pi
curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
less install.sh
sudo bash install.sh

# configure and start
cp deploy/compose/.env.example deploy/compose/.env
docker compose -f deploy/compose/scan-bridge.yml up -d
```

No SANE, no scanbd, no language runtime installed on the host. Documents
land on your own Synology NAS, so your existing backup and snapshot
policy already covers them. MIT licensed, no cloud dependency, no
telemetry.

## Where it actually stands

!!! warning "Project status: Phase 1 core is real, deployment tooling is not"

    This is a home-lab project under active development. Phase 0
    (repository, documentation, this site) is done except the launch
    blog post. The Phase 1.2 pipeline core is further along than a
    quick glance at the roadmap suggests: `scan-bridge` serves
    `/health`, `/version`, `/ready`, `/profiles`, and `/profiles/{name}`
    today, and `POST /scan` is a real, bearer-protected handler that
    dispatches through `sane-runtime` and `scan-processor` to delivery
    — only the `/jobs*` endpoints still return `501 Not Implemented`.
    Both `sane-runtime` and `scan-processor` have real Go
    implementations and Dockerfiles, and the repository-root
    `compose.yaml` wires all three services together for a hardware
    smoke test on hhplex01 that is prepared but, as of this writing,
    not yet run. What is genuinely missing: the bootstrap script, the
    published `deploy/compose/` stack, scanbd (hardware-button
    triggering — documented as out of scope for `sane-runtime` so far),
    the Home Assistant blueprint, and the async job store. If this ever
    drifts from [ROADMAP.md](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md),
    treat the code as authoritative — the roadmap is a plan, this page
    and the table below are checked against what is actually in the
    repository.

<div class="mdx-status" markdown="1">
| Phase | Scope | Status |
| ----- | ----- | ------ |
| **0** | Repository, MIT license, docs site, hardware table | complete* |
| **1** | `scan-bridge` HTTP surface + `POST /scan` real; `sane-runtime` and `scan-processor` implemented and wired via `compose.yaml`; hardware smoke test prepared, not yet run; bootstrap script and published compose stack not written; job store (`/jobs*`) still `501` | in progress |
| **2** | Hardware buttons (scanbd), Zigbee blueprints, n8n workflow exports | not started |
| **3** | restic backup, Prometheus/Grafana, security hardening | not started |
| **4** | Ecosystem maturity — community-driven | not started |
</div>

Checked directly against the repository at the time of writing — the
[roadmap](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md)
is the plan, not always the up-to-the-commit status; where they
disagree, what's actually in the repository wins here.

## Where to go next

- [Getting started](getting-started/index.md) — prerequisites and the
  first scan
- [Architecture](architecture/index.md) — components, data flow, and the
  three storage topologies
- [Hardware](hardware/index.md) — what works, what does not, and how to
  report your own device
- [Operations](operations/index.md) — troubleshooting and day-two
  concerns

## License and trademarks

MIT. Kodak® and ScanMate® are trademarks of Kodak Alaris Inc. Synology®
is a trademark of Synology Inc. IKEA®, TRÅDFRI®, STYRBAR®, and
SYMFONISK® are trademarks of Inter IKEA Systems B.V. Raspberry Pi® is a
trademark of Raspberry Pi Ltd. Paperless-ngx is a community-maintained
fork of Paperless-ng. This project is not affiliated with, endorsed by,
or sponsored by any of these companies. Product names are used solely
for identification purposes.
