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
- **Hardware button** — scanbd polls the scanner's own buttons and calls
  a hook script. No extra infrastructure required.
- **Zigbee remote** — a STYRBAR button mapped through Home Assistant, one
  button event per scanning profile.
- **HTTP webhook** — a plain `POST /scan` from any system on the
  network: a phone shortcut, a script, another service.
</div>

## Nothing on the host but Docker

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

!!! warning "Project status: early Phase 1 — nothing scans yet"

    This is a home-lab project under active development. Phase 0
    (repository, documentation, this site) is done except the launch
    blog post. Phase 1 has started: the `scan-bridge` daemon serves
    `/health`, `/version`, `/profiles` and `/profiles/{name}` today,
    while `/ready`, `/scan` and the `/jobs` endpoints return
    `501 Not Implemented`. The `sane-runtime` and `scan-processor`
    containers, the compose stacks, and the bootstrap script are not
    written yet, so there is no working scan path.

<div class="mdx-status" markdown="1">
| Phase | Scope | Status |
| ----- | ----- | ------ |
| **0** | Repository, MIT license, docs site, hardware table | complete* |
| **1** | `scan-bridge` HTTP surface merged; `sane-runtime` and `scan-processor` not yet built | in progress |
| **2** | Hardware buttons, Zigbee blueprints, n8n workflow exports | not started |
| **3** | restic backup, Prometheus/Grafana, security hardening | not started |
| **4** | Ecosystem maturity — community-driven | not started |
</div>

The full [roadmap](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md)
tracks what exists and what does not — this is not a marketing summary
of it, it's the same status stated everywhere else in the repository.

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
