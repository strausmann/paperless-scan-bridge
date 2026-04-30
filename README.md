# paperless-scan-bridge

> Turn any SANE-compatible USB scanner into a hands-free,
> Zigbee-triggered ingestion pipeline for Paperless-ngx —
> with a Synology NAS as the document hub.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/ci.yml)
[![Docs](https://github.com/strausmann/paperless-scan-bridge/actions/workflows/docs.yml/badge.svg)](https://scan-bridge.strausmann.de)
[![Tested on Pi 4 / Pi 5](https://img.shields.io/badge/Tested%20on-Pi%204%20%7C%20Pi%205-success)](docs/HARDWARE_COMPATIBILITY.md)
[![Container images on GHCR](https://img.shields.io/badge/images-ghcr.io-2188FF)](https://github.com/strausmann?tab=packages&repo_name=paperless-scan-bridge)

## What this does

Place a document in the scanner. Press a button. Find the document searchable
in your Paperless-ngx instance thirty seconds later — without ever touching
a desktop computer.

`paperless-scan-bridge` is a container-first stack that connects a USB
document scanner attached to a Raspberry Pi to a Paperless-ngx instance
running anywhere on your network. Multiple scanning profiles — duplex/simplex,
color/grayscale, private/business — are selectable either through the
scanner's hardware buttons or through a Zigbee remote routed via Home
Assistant or n8n.

Documents land on a Synology NAS, which means the existing snapshot and
backup strategy of the NAS applies to everything the system produces.
The Pi itself stays disposable: a fresh image plus one bootstrap script
brings the bridge back online in under fifteen minutes.

## Architecture at a glance

Three components run as containers on the Pi. The host carries Docker, an
NFS mount, and udev rules for stable USB access — nothing else.

| Component | Language | Purpose |
| --------- | -------- | ------- |
| `scan-bridge` | Go | REST and webhook API, profile dispatching, health and metrics |
| `sane-runtime` | Bash | SANE backend container providing `scanimage` and optional `scanbd` |
| `scan-processor` | Go | PDF assembly, deskew, blank-page detection, atomic NFS writes |

Triggers reach the Pi over three paths: a hardware button on the scanner,
a Zigbee remote via Home Assistant, or any HTTP webhook (n8n, custom
scripts, browser bookmark). All three end up calling the same REST endpoint
on `scan-bridge`.

Storage runs in three documented topologies: local FS on the Docker host
with restic backup to the NAS (recommended default), NFS direct from the
NAS, or an iSCSI LUN. Each has its trade-offs around inotify support,
backup simplicity, and HA compatibility — the
[storage topology document](docs/STORAGE_TOPOLOGIES.md) walks through all
three.

## Quickstart

Prerequisites: A Raspberry Pi 4 or 5 with Ubuntu Server 24.04 (arm64),
a Synology NAS with NFS enabled, a Docker host running Paperless-ngx,
and SSH access to all three.

```bash
git clone https://github.com/strausmann/paperless-scan-bridge.git
cd paperless-scan-bridge
cp deploy/bootstrap/.env.example deploy/bootstrap/.env
$EDITOR deploy/bootstrap/.env
ssh pi@your-scanner-pi 'sudo bash -s' < deploy/bootstrap/install.sh
```

That installs Docker, configures the NFS mount, deploys the udev rules
for USB stability, pulls the three container images from GHCR, and brings
up the stack with sane defaults. Plug in the scanner, import the Home
Assistant blueprint, and the bridge is operational.

For step-by-step instructions including the Synology share configuration,
the scanner verification procedure, and the trigger-source setup, see
the [getting-started guide](docs/getting-started/index.md).

## Documentation

The full documentation lives at **[scan-bridge.strausmann.de](https://scan-bridge.strausmann.de)**
and is built with [Zensical](https://zensical.org) from the `docs/`
directory. Available in English and German.

Key entry points:

- [Getting started](docs/getting-started/index.md) — first deployment, end to end
- [Architecture](docs/ARCHITECTURE.md) — component breakdown and data flows
- [Storage topologies](docs/STORAGE_TOPOLOGIES.md) — NFS, local FS, iSCSI compared
- [Hardware compatibility](docs/HARDWARE_COMPATIBILITY.md) — tested scanner models
- [Disaster recovery](docs/DISASTER_RECOVERY.md) — backup, restore, runbooks
- [Threat model](docs/THREAT_MODEL.md) — security analysis and mitigations
- [Troubleshooting](docs/TROUBLESHOOTING.md) — common issues and resolutions

## What is in this repository

| Path | Purpose |
| ---- | ------- |
| `components/scan-bridge/` | Go source for the core daemon |
| `components/sane-runtime/` | Container definition for the SANE runtime |
| `components/scan-processor/` | Go source for the PDF pipeline |
| `deploy/compose/` | Docker Compose stacks for Pi and Paperless host |
| `deploy/bootstrap/` | One-shot installer for a fresh Pi |
| `deploy/udev/` | USB stability rules |
| `deploy/ansible/` | Optional Ansible layer for fleet deployments |
| `homeassistant/` | Importable blueprints for STYRBAR, SYMFONISK, RODRET |
| `n8n/` | Workflow exports as JSON |
| `backup/` | Restic wrappers and restore runbook |
| `monitoring/` | Prometheus exporters and Grafana dashboards |
| `security/` | Hardening profiles and threat-model artifacts |
| `docs/` | Zensical source for the documentation site |

## Why I built this

I have a Kodak ScanMate i1120 on my desk and a Paperless-ngx instance
on my homelab server in Maschen. Connecting the two without keeping a
Windows PC running turned into a multi-week deep dive into SANE backends,
scanbd quirks, NFS permission models, and Zigbee button automation.
This repository is the documented version of that journey, packaged so
the next person does not have to repeat it.

There are dozens of fragmentary tutorials for parts of this stack. There
is no single repository that walks you from a fresh Pi image to a
production-grade scan pipeline with backup, monitoring, and security
hardening — until now.

## Status

Production-grade target. Active development. See the
[roadmap](ROADMAP.md) for the phase plan.

| Phase | Scope | Status |
| ----- | ----- | ------ |
| 0 | Repo skeleton, foundational documents | In progress |
| 1 | Minimum viable stack on Pi 5 with i1120 | Planned |
| 2 | Trigger paths and UI | Planned |
| 3 | Production hardening, monitoring, backup | Planned |
| 4 | Maturity, ecosystem, community contributions | Planned |

## Contributing

Pull requests are welcome — especially hardware compatibility reports,
trigger-path examples, and bug fixes. See [CONTRIBUTING.md](CONTRIBUTING.md)
for the workflow, code style, and how to run the test suite locally.

For security issues, please follow the responsible disclosure procedure
in [SECURITY.md](SECURITY.md) instead of opening a public issue.

## License

MIT — see [LICENSE](LICENSE).

### Trademarks

Kodak® and ScanMate® are trademarks of Kodak Alaris Inc.
Synology® is a trademark of Synology Inc.
IKEA®, TRÅDFRI®, STYRBAR®, and SYMFONISK® are trademarks of Inter IKEA Systems B.V.
Raspberry Pi® is a trademark of Raspberry Pi Ltd.
Paperless-ngx® is a community-maintained project.
This repository is not affiliated with, endorsed by, or sponsored by any
of these companies. Product names are used solely for identification
purposes.
