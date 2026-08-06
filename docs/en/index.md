# paperless-scan-bridge

Place a document. Press a button. Find it searchable in Paperless-ngx
thirty seconds later.

`paperless-scan-bridge` is a container-first stack that connects a USB
document scanner attached to a Raspberry Pi to a Paperless-ngx instance
running anywhere on your network. Documents land on a Synology NAS, so
your existing backup, snapshot, and off-site replication strategy
applies to everything the system produces.

!!! warning "Project status: early Phase 1 — nothing scans yet"

    This is a home-lab project under active development. Phase 0
    (repository, documentation, this site) is done. Phase 1 has started:
    the `scan-bridge` daemon serves `/health`, `/version`, `/profiles` and
    `/profiles/{name}` today, while `/ready`, `/scan` and the `/jobs`
    endpoints return `501 Not Implemented`. The `sane-runtime` and
    `scan-processor` containers, the compose stacks and the bootstrap
    script are not written yet, so there is no working scan path. The
    [roadmap](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md)
    tracks what exists and what does not.

## Why this exists

There are dozens of fragmentary tutorials for parts of this stack — SANE
on a Pi, Paperless-ngx with NFS, scanner buttons via scanbd, Zigbee
automation in Home Assistant. There is no single repository that walks
you from a fresh Pi image to a fully production-grade scan pipeline with
backup, monitoring, and security hardening.

This repository fills that gap. It is also a living record of turning a
Kodak ScanMate i1120 — a sixteen-year-old desk scanner without modern
Linux drivers — into a hands-free part of a homelab.

## What the stack provides

- A `scan-bridge` daemon (Go) exposing a REST API for scan jobs, profile
  management, and Prometheus metrics
- A `sane-runtime` container with SANE drivers and udev integration for
  stable USB device paths
- A `scan-processor` container that takes raw scans, deskews them,
  filters blank pages, builds PDFs, and writes them atomically to the
  consumption directory
- Docker Compose stacks for Paperless-ngx with the storage topology of
  your choice
- Home Assistant blueprints and n8n workflow exports for Zigbee-triggered
  scanning
- restic-based backup with PostgreSQL dumps, retention policies, and a
  tested restore runbook
- Prometheus exporters, Grafana dashboards, and synthetic health checks

## The one non-negotiable rule

**Container-first, host-thin.** The only acceptable modifications to the
Pi are: install Docker and the compose plugin, mount the Synology NFS
share via `/etc/fstab`, and install udev rules under
`/etc/udev/rules.d/`. No SANE on the host. No scanbd on the host. No
Python or Go toolchains on the host.

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
