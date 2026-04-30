# Architecture

This document describes the technical architecture of
`paperless-scan-bridge` in enough depth that you can reason about the
system without reading the source code first.

## Design philosophy

Three principles drive every decision in this project.

**Container-first, host-thin.** The Pi host is treated as a thin
substrate. It runs Docker, mounts an NFS share from the Synology, and
loads udev rules for stable USB device paths. Everything else lives in
containers. This makes the Pi disposable — flash a fresh SD card, run
the bootstrap script, and the system is back within fifteen minutes.

**Single source of truth for storage.** Original documents and the OCR
archive live on the Synology NAS. The Pi acts as an ingestion node, not
a storage node. The Docker host that runs Paperless-ngx either reads
from the same Synology share, or holds a working copy backed up
nightly. Either way, the Synology is the canonical home of every
document.

**Trade explicit complexity over hidden magic.** When a choice has
real-world trade-offs — local FS versus NFS, scanbd versus webhooks,
restic versus Synology snapshots — the choice is documented, the
alternatives are documented, and the user picks. We do not bury
trade-offs in default configurations.

## High-level data flow

```
[ Trigger source ]                         [ Scanner hardware ]
   Zigbee button                              Kodak ScanMate i1120
   HA / n8n webhook                            ADF, USB
   Web UI / curl                                 |
        |                                         v
        v                                  /dev/bus/usb (host)
  POST /scan to scan-bridge                          |
        |                                            v
        v                              [ sane-runtime container ]
   scan-bridge daemon  -------exec----->   scanimage --batch
   (Go, REST, profiles)                         |
        |                                       v
        |                              raw TIFF/JPEG batch
        v                                       |
   [ scan-processor container ] <----------------+
   (Go, deskew, blank-page filter, PDF assembly)
        |
        v
  /mnt/synology/consume/<profile>/<timestamp>.pdf
        |
        v
  [ Paperless-ngx container ]
   (consumes via inotify or polling)
        |
        v
  PostgreSQL metadata + searchable PDF/A
```

Everything between the trigger and the final PDF is containerized. Only
the USB device node and the NFS mount cross the host-container
boundary.

## Component inventory

### Custom containers (we build, we publish on GHCR)

#### `scan-bridge`

The core daemon. Written in Go for a single static binary, fast startup,
small memory footprint, and easy ARM64 cross-compilation.

**Responsibilities:**

- Expose REST API: `POST /scan`, `GET /profiles`, `GET /jobs/:id`,
  `GET /health`, `GET /metrics`
- Accept Webhook calls from Home Assistant, n8n, or any HTTP client
- Load profile definitions from a YAML file mounted into the container
- Dispatch scan jobs to the `sane-runtime` container via a Unix socket
- Track job status in an in-memory queue; persist completed jobs to a
  small SQLite database for inspection
- Export Prometheus metrics: scan duration, queue depth, error rate,
  profile usage distribution
- Optional gRPC endpoint for Pi-to-Pi communication in HA setups

**Image:** `ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:vX.Y.Z`

**Container size goal:** under 25 MB (distroless base, static binary)

**Configuration:** TOML file at `/etc/scan-bridge/config.toml`,
profiles at `/etc/scan-bridge/profiles.yaml`

#### `sane-runtime`

The container that owns the scanner. Debian slim base because SANE
backends are mature on Debian and the additional ~50 MB is justified.

**Responsibilities:**

- Provide `scanimage`, `scanbd`, `sane-utils`, `imagemagick`, `qpdf`
- Detect the scanner via `scanimage -L` on startup
- Run `scanbd` daemon if hardware buttons are configured
- Expose a thin HTTP API on a Unix socket: `POST /scan` with profile
  parameters, returns image batch path
- Health-check endpoint that verifies scanner is responsive

**Image:** `ghcr.io/strausmann/paperless-scan-bridge/sane-runtime:vX.Y.Z`

**Privileges:** Requires `--device=/dev/bus/usb` plus the appropriate
udev rule for stable device paths. Does **not** require `--privileged`
mode; specific device cgroup permissions are sufficient.

**Why a separate container:** Updates to the SANE stack happen on a
different cadence than updates to the daemon. Separating them prevents
a SANE update from forcing a daemon redeploy.

#### `scan-processor`

The pipeline worker. Takes raw scan output and produces consumable PDFs.

**Responsibilities:**

- Read raw image batch from a shared volume
- Apply deskew via Leptonica bindings or imagemagick fallback
- Detect and remove blank pages using pixel-density thresholds
- Optional rotation correction
- Merge images into a single PDF with pdfcpu
- Optional local OCR pass with tesseract (off by default — Paperless
  does this better on the bigger Docker host)
- Write output atomically to `/mnt/synology/consume/<profile>/` using
  `O_TMPFILE` + `linkat` so Paperless never sees a partially written
  file
- Clean up the working directory

**Image:** `ghcr.io/strausmann/paperless-scan-bridge/scan-processor:vX.Y.Z`

**Why a separate container:** PDF processing is CPU-intensive and can
be scaled independently. On a busy day with 50+ scans, you might run
two processor containers behind a queue.

### Adopted upstream containers

| Image                              | Role                                  | Notes                                   |
| ---------------------------------- | ------------------------------------- | --------------------------------------- |
| `ghcr.io/paperless-ngx/paperless-ngx` | DMS, OCR, indexing, web UI         | Reference target for the consumer       |
| `sbs20/scanservjs`                 | Optional manual scan web UI           | Useful for ad-hoc scans, parallel to bridge |
| `containrrr/watchtower`            | Container update automation           | With strict allowlist, never auto-update Paperless |
| `prom/node-exporter`               | Pi hardware metrics                   | CPU, memory, USB throughput             |
| `prom/prometheus` (optional)       | Metric scraping                       | Often runs on the Docker host instead   |
| `grafana/grafana` (optional)       | Dashboards                            | Often runs on the Docker host instead   |

## Storage topologies

The relationship between Pi, Docker host, and Synology NAS is the most
consequential architectural decision. We support three topologies and
document the trade-offs explicitly.

### Topology A: Local filesystem on Docker host with restic backup

```
Pi → NFS write → /mnt/synology/staging/   (Synology)
                          |
                       sync via inotify-watch container
                          v
                Docker host: /var/lib/paperless/consume   (local FS)
                          |
                       inotify pickup
                          v
                Paperless-ngx
                          |
                          +-- nightly restic to Synology /backup/restic/
                          |
                          +-- weekly restic check --read-data-subset=10%
```

**Pros:** Inotify works, sub-second pickup, fastest performance for
Paperless reads. Backup is a separate, well-defined process with a
dedicated tool.

**Cons:** Two separate storage tiers (live + backup). Restore requires
restic, not just file copy.

**When to choose:** This is the recommended default. Best balance of
performance, backup integrity, and operational clarity.

### Topology B: NFS direct from Synology

```
Pi → NFS write → /mnt/synology/consume/   (Synology)
                          |
                          v
                Docker host: NFS mount → Paperless container
                          |
                       polling (inotify does not work on NFS)
                          v
                Paperless-ngx
                          |
                          +-- relies on Synology Btrfs snapshots
                          +-- Hyper Backup to off-site
```

**Pros:** Single storage tier. Backup is implicit via snapshots and
existing Synology infrastructure. Simpler mental model.

**Cons:** `inotify` does not work over NFS, so Paperless must poll
(`PAPERLESS_CONSUMER_POLLING=10`). 10-second latency at the scanner.
NFS lock contention possible under heavy load. Btrfs snapshots on a
live consume directory can race with writes; not always crash-consistent.

**When to choose:** Smaller setups where simplicity beats latency.
Single-user homes with low scan volume.

### Topology C: iSCSI LUN from Synology

```
Pi → NFS write → /mnt/synology/staging/   (Synology)
                          |
                          v
                Docker host: iSCSI LUN mounted as ext4 → Paperless
                          |
                       inotify pickup
                          v
                Paperless-ngx
                          |
                          +-- LUN snapshots on Synology
                          +-- Hyper Backup of LUN snapshots
```

**Pros:** Inotify works (block device, local filesystem from the
host's perspective). LUN snapshots are crash-consistent at the block
level. Single storage location for backup purposes.

**Cons:** A LUN belongs to exactly one host. Cannot run active-active
on two Docker nodes. iSCSI on 1 GbE is the bottleneck for very large
scans. Requires more setup than NFS.

**When to choose:** Want inotify + Synology-native backup; willing to
accept single-host limitation.

The `deploy/compose/` directory contains compose files for all three
topologies. The user picks one and copies it to `docker-compose.yml`.

## The Pi itself

The Pi runs Ubuntu Server 24.04 LTS for arm64. Reference hardware is a
Raspberry Pi 5 with 8 GB RAM and an SSD over USB 3.0. A Pi 4 with 4 GB
also works, with slightly slower scan throughput.

**Host installations:**

1. Docker Engine and the Compose plugin
2. `cifs-utils` and `nfs-common` (NFS mount)
3. The udev rules file at `/etc/udev/rules.d/99-paperless-scan-bridge.rules`
4. `systemd` units for the NFS mount and a watchdog timer

**That is it.** No SANE on the host. No scanbd on the host. No Python
runtimes, no Node.js, no Go toolchain. The bootstrap script installs
exactly these four things.

The udev rule is necessary because USB device paths (`/dev/bus/usb/001/004`)
shift after every disconnect. The rule writes a stable symlink like
`/dev/scanner-i1120` based on USB vendor/product ID. The container
binds the symlink, not the moving device path.

## Trigger paths

Three trigger sources are supported. The user enables some or all of them.

### HTTP webhook (the primary path)

`POST /scan` with a JSON body specifying the profile. This is the
canonical interface — every other trigger source ultimately calls this
endpoint.

```http
POST /scan HTTP/1.1
Content-Type: application/json

{ "profile": "private-duplex" }
```

Response:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{ "job_id": "01HJ9P5K2N6QXZ", "status": "queued" }
```

### Home Assistant blueprint

Importable HA blueprint that listens for Zigbee button events and POSTs
to the scan-bridge webhook. The blueprint is parameterized — you select
your button entity and map button events to profiles in the HA UI.

Blueprints ship for IKEA STYRBAR, IKEA SYMFONISK Sound Remote Gen 2,
and IKEA RODRET. Each blueprint covers all the button events the device
supports — short press, long press, multi-tap.

### Hardware scanner buttons

The `sane-runtime` container runs `scanbd` which polls the scanner's
hardware buttons. When a button is pressed (or the profile-counter LCD
on the i1120 changes), scanbd executes a hook script inside the
container that POSTs to the scan-bridge webhook.

This path is optional. It only works for scanners that expose buttons
via SANE.

## Profile system

Profiles are the abstraction that ties everything together. A profile
defines:

- Source: `ADF Front`, `ADF Duplex`, flatbed
- Resolution in DPI
- Color mode: `Color`, `Gray`, `Lineart`
- Output format: PDF, JPEG batch, single TIFF
- Target directory under `/mnt/synology/consume/`
- Optional pre-processing flags (deskew, blank page removal)
- Optional Paperless tags via subdirectory placement

Profiles are defined in `profiles.yaml`:

```yaml
profiles:
  - name: private-duplex
    source: ADF Duplex
    resolution: 300
    mode: Color
    format: pdf
    target: private/
    deskew: true
    remove_blank: true

  - name: business-simplex
    source: ADF Front
    resolution: 200
    mode: Gray
    format: pdf
    target: business/
    deskew: true
    remove_blank: false

  - name: receipt
    source: ADF Front
    resolution: 300
    mode: Color
    format: pdf
    target: receipts/
    deskew: true
    remove_blank: false
```

The `target` is a subdirectory under the consume directory. Combined
with `PAPERLESS_CONSUMER_RECURSIVE=true` and
`PAPERLESS_CONSUMER_SUBDIRS_AS_TAGS=true`, the directory name becomes a
Paperless tag automatically.

## Backup architecture

Documents are the asset. Everything else is rebuildable. The backup
strategy reflects this priority.

**What is backed up:**

- Original documents (in topology A, the local FS; in B and C, the
  Synology share)
- PostgreSQL dump from Paperless (the metadata, tags, document types)
- Paperless `media/originals` and `media/archive` directories
- Configuration files: docker-compose, profiles.yaml, environment files

**What is not backed up:**

- Container images (rebuilt from GHCR)
- Operating system on the Pi (rebuilt with bootstrap script)
- Redis state (rebuilt on next start)
- Search index (rebuilt by Paperless)

**Tool:** restic. Reasons: deduplication (incremental backups are
small), encryption (the repository on the NAS is unreadable without
the password), well-tested restore path, integrity verification with
`restic check`.

**Schedule:**

- Hourly: PostgreSQL dump to a local file (via `pg_dump`)
- Nightly: full restic snapshot of the consume tree, originals,
  archive, and the latest PG dump
- Weekly: `restic forget --prune` with retention policy
  (`--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --keep-yearly 5`)
- Weekly: `restic check --read-data-subset=10%` to verify the
  repository is consistent

**Off-site:** The Synology runs Hyper Backup of the restic repository
to an external location (second NAS, S3-compatible bucket, encrypted
external HDD).

The complete restore procedure is documented in
[DISASTER_RECOVERY.md](DISASTER_RECOVERY.md).

## High-availability stance

Real high availability for Paperless-ngx is hard — the database is
shared state, and active-active configurations require careful
coordination. We take a pragmatic position.

**For the Pi:** Cold standby. A second Pi with the bootstrap script in
a drawer. If the active Pi fails, swap, run bootstrap, restart
containers. Recovery time: 15-30 minutes.

**For the Docker host (Paperless):** Cold standby. A second Docker host
with the same compose stack, restic restore as the recovery procedure.
Recovery time: 60-90 minutes including restic restore.

**For the Synology:** This is where actual HA might be justified.
Synology HA Cluster with two NAS devices is the supported path, but
Enterprise pricing applies. For most home setups, off-site backup is
the right answer.

The runbooks in `ha/` document each scenario step by step.

## Threat model summary

Full threat model in [THREAT_MODEL.md](THREAT_MODEL.md). The
short version:

- Documents may contain sensitive personal data; treat the entire
  pipeline as handling private information
- The bridge daemon is on the LAN; it has no direct internet exposure
- Webhook endpoints accept calls only from configured source IPs (HA,
  n8n) plus localhost
- Container images are pinned by digest in production compose files;
  Watchtower has an explicit allowlist
- Secrets are managed via SOPS with age keys; no secrets in git
- The Synology share allows access only from the Pi's and Docker
  host's IP addresses
- restic repository encryption protects backups against NAS compromise

## What this architecture optimizes for

A summary of the priorities, in order:

1. **Reliability** — when you press the button, the scan happens
2. **Recoverability** — when something fails, you can rebuild quickly
3. **Auditability** — every component is small, focused, readable
4. **Operability** — Prometheus metrics, structured logs, Grafana dashboards
5. **Security** — defense in depth, no shortcuts on credentials
6. **Performance** — fast enough that the user does not wait
7. **Flexibility** — three storage topologies, three trigger paths

## What this architecture does not optimize for

Equally important. We do not pretend to be optimal at:

- Multi-user collaboration on the same document inbox (Paperless does
  this; we do not add a layer)
- Cloud-native horizontal scaling (this is a homelab project)
- Cross-region replication (out of scope for self-hosting)
- Sub-second scan latency at any cost (the scanner mechanics dominate
  end-to-end time anyway)

If you need any of these, this stack is the wrong tool.
