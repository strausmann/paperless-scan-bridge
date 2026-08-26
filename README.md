# paperless-scan-bridge

> Turn any SANE-compatible USB scanner into a hands-free,
> Zigbee-triggered ingestion pipeline for Paperless-ngx —
> with a Synology NAS as the document hub.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/ci.yml)
[![Docs](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/docs.yml/badge.svg)](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/docs.yml)
[![Container Images](https://img.shields.io/badge/Containers-GHCR-blue)](https://github.com/strausmann?tab=packages&repo_name=paperless-scan-bridge)

Place a document. Press a button. Find it searchable in Paperless-ngx
thirty seconds later.

`paperless-scan-bridge` is a container-first stack that connects a USB
document scanner attached to a Raspberry Pi to a Paperless-ngx instance
running anywhere on your network. Multiple scanning profiles —
duplex/simplex, color/grayscale, by category — are selectable either
through the scanner's hardware buttons, a Zigbee remote routed via Home
Assistant or n8n, or a web UI.

Documents land on a Synology NAS. Your existing backup, snapshot, and
off-site replication strategy applies to everything the system produces.

## Why this exists

There are dozens of fragmentary tutorials for parts of this stack — SANE
on a Pi, Paperless-ngx with NFS, scanner buttons via scanbd, Zigbee
automation in Home Assistant. There is no single repository that walks
you from a fresh Pi image to a fully production-grade scan pipeline with
backup, monitoring, and security hardening.

This repository fills that gap. It is also a living record of my own
journey turning a Kodak ScanMate i1120 — a sixteen-year-old desk scanner
without modern Linux drivers — into a hands-free part of my homelab.

## What this stack provides

- A `scan-bridge` daemon (Go) exposing a REST API for scan jobs, profile
  management, and Prometheus metrics
- A `sane-runtime` container with SANE drivers, scanbd for hardware
  buttons, and udev integration for stable USB device paths
- A `scan-processor` container that takes raw scans, deskews them,
  filters blank pages, builds PDFs, and writes them atomically to the
  consumption directory
- Production-ready Docker Compose stacks for Paperless-ngx with the
  storage topology of your choice
- Home Assistant blueprints and n8n workflow exports for Zigbee-triggered
  scanning with multi-profile support
- restic-based backup with PostgreSQL dumps, retention policies, and a
  tested restore runbook
- Prometheus exporters, Grafana dashboards, and synthetic health checks
- Security hardening profiles, threat model, and CrowdSec collections
- Full documentation site at
  [strausmann.github.io/paperless-scan-bridge](https://strausmann.github.io/paperless-scan-bridge/)

## Quickstart

Prerequisites: A Raspberry Pi 4 or 5 running Ubuntu Server 24.04 (arm64),
a Synology NAS with NFS enabled, and a Docker host for Paperless-ngx.

The Pi only needs Docker, an NFS mount, and USB permissions. Everything
else runs in containers — no SANE installation on the host, no scanbd on
the host, no language runtimes on the host.

```bash
git clone https://github.com/strausmann/paperless-scan-bridge.git
cd paperless-scan-bridge

# Bootstrap the Pi (Docker, NFS mount, udev rules, container pull).
# Download and read the script before running it — it edits /etc/fstab
# and /etc/udev/rules.d/ as root.
ssh pi@your-pi-host
curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
less install.sh
sudo bash install.sh

# Configure your environment
cp deploy/compose/.env.example deploy/compose/.env
$EDITOR deploy/compose/.env

# Bring up the bridge
docker compose -f deploy/compose/scan-bridge.yml up -d
```

For step-by-step instructions including Synology share configuration,
hardware verification, Home Assistant blueprint import, and storage
topology choice, see the
[Quickstart guide](https://strausmann.github.io/paperless-scan-bridge/getting-started/quickstart/).

## Architecture at a glance

Three custom container images plus adopted upstream images. The Pi runs
only Docker; everything functional lives in containers.

| Component         | Role                                                 | Language       |
| ----------------- | ---------------------------------------------------- | -------------- |
| `scan-bridge`     | Core daemon, REST API, profile dispatch, metrics     | Go             |
| `sane-runtime`    | SANE drivers, scanbd, USB integration                | Bash + Go      |
| `scan-processor`  | Image processing, PDF assembly, NFS write            | Go             |
| Paperless-ngx     | DMS with OCR, indexing, UI                           | (upstream)     |
| scanservjs        | Optional manual scanning web UI                      | (upstream)     |

Three storage topologies are supported and documented:

1. **Local FS on Docker host** with restic backup to NAS — fastest, recommended default
2. **NFS direct from Synology** — simplest backup, polling instead of inotify
3. **iSCSI LUN from Synology** — block-level backup via LUN snapshots, single-host limitation

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full architectural
discussion and trade-offs.

## Repository layout

| Path             | Purpose                                                       |
| ---------------- | ------------------------------------------------------------- |
| `components/`    | Source code for the three custom container images             |
| `deploy/`        | Compose stacks, bootstrap script, optional Ansible layer      |
| `homeassistant/` | Importable blueprints for STYRBAR, SYMFONISK Gen 2, RODRET    |
| `n8n/`           | Workflow exports for the alternative trigger path             |
| `backup/`        | restic wrappers, PostgreSQL dump scripts, restore runbooks    |
| `monitoring/`    | Prometheus exporters, Grafana dashboards, synthetic checks    |
| `security/`      | Hardening profiles, CrowdSec collections                      |
| `ha/`            | Cold-standby setup, failover runbook, witness Pi pattern      |
| `docs/`          | Zensical source for [strausmann.github.io/paperless-scan-bridge](https://strausmann.github.io/paperless-scan-bridge/) |
| `tests/`         | Bats tests, integration tests, CI configuration               |

## Roadmap

The project follows a four-phase plan:

- **Phase 0** — Repository foundation, documentation skeleton, MIT license
- **Phase 1** — Minimum viable stack: three containers, bootstrap, REST API
- **Phase 2** — Trigger paths: Home Assistant blueprints, n8n workflows, hardware buttons
- **Phase 3** — Production hardening: backup, monitoring, security, CI/CD
- **Phase 4** — Maturity: hardware compatibility expansion, community PRs, lessons-learned

See [ROADMAP.md](ROADMAP.md) for current status and tracked issues.

## Documentation

Full documentation lives at
[strausmann.github.io/paperless-scan-bridge](https://strausmann.github.io/paperless-scan-bridge/) and is
built from `docs/` using [Zensical](https://zensical.org).

Key entry points:

- [Quickstart guide](https://strausmann.github.io/paperless-scan-bridge/getting-started/quickstart/)
- [Architecture deep-dive](ARCHITECTURE.md)
- [Hardware compatibility list](HARDWARE_COMPATIBILITY.md)
- [Storage topology comparison](https://strausmann.github.io/paperless-scan-bridge/architecture/storage-topologies/)
- [Backup and disaster recovery](DISASTER_RECOVERY.md)
- [Threat model](THREAT_MODEL.md)
- [Troubleshooting guide](https://strausmann.github.io/paperless-scan-bridge/operations/troubleshooting/)

The site is English-first. A German version is served under `/de/` and
is being filled in gradually — Zensical has no native multi-language
support yet, so each language is a separate build (issue #13).

## Contributing

Pull requests are welcome, especially for hardware compatibility reports.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow,
code style, and test suite instructions.

For security issues, please follow the responsible disclosure procedure
in [SECURITY.md](SECURITY.md) instead of opening a public issue.

## License

MIT — see [LICENSE](LICENSE).

### Trademarks

Kodak® and ScanMate® are trademarks of Kodak Alaris Inc. Synology® is a
trademark of Synology Inc. IKEA®, TRÅDFRI®, STYRBAR®, and SYMFONISK® are
trademarks of Inter IKEA Systems B.V. Raspberry Pi® is a trademark of
Raspberry Pi Ltd. Paperless-ngx is a community-maintained fork of
Paperless-ng. This project is not affiliated with, endorsed by, or
sponsored by any of these companies. Product names are used solely for
identification purposes.
