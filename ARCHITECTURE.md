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

> **Status (2026-08-13):** this diagram reflects what is actually
> built today. Earlier drafts of this document described
> `scan-processor` reading from a shared volume and writing its
> output to a Synology NFS consume directory that Paperless-ngx polls
> or inotify-watches — that model was never implemented. The real
> pipeline is request/response end-to-end: `scan-bridge` is the only
> component that talks to `sane-runtime`, `scan-processor`, and
> Paperless-ngx; neither of the other two ever talks to the other
> directly, and none of them write to a directory the next stage
> watches. See
> [`docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`](docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)
> for the design that replaced the shared-volume/inotify/callback
> sketch, and
> [`components/scan-processor/README.md`](components/scan-processor/README.md)
> for `scan-processor`'s authoritative contract.

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
   scan-bridge daemon  ----HTTP/Unix-socket-->  scanimage --batch
   (Go, REST, profiles)                         |
        ^                                       v
        |                              multipart: TIFF pages back
        +---------------------------------------+
        |
        v  scan-bridge writes the pages to its own OutputDir, then
        |  re-reads them and POSTs them (HTTP over a SECOND, separate
        |  Unix socket) to:
        v
   [ scan-processor container ]
   (Go; shells out to convert(1)/tesseract(1)/qpdf(1) for deskew,
    blank-page removal, rotation, OCR deu+eng, format conversion,
    multi-page assembly)
        |
        v
   assembled document(s) returned to scan-bridge in the SAME HTTP
   response (multipart/mixed) -- no shared volume, no callback URL
        |
        v
   scan-bridge resolves the profile's configured destinations
   (ADR 0016) and delivers each document to every one of them. The
   only destination built today:
        |
        v
  POST /api/documents/post_document/ on Paperless-ngx
  (multipart/form-data, Token auth, direct API call -- not a
   consume-directory write)
        |
        v
  200 {"task_id": "..."} -- Paperless-ngx accepted the upload for
  its own asynchronous Celery consumption task
        |
        v
  PostgreSQL metadata + searchable PDF/A
```

Everything between the trigger and the finished Paperless-ngx document
is containerized. Only the USB device node crosses the host-container
boundary; the NFS mount from the "Storage topologies" section below is
relevant to a possible future NFS/SMB destination module (not built —
see `docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`
sec. 1/12), not to the pipeline this diagram shows.

`scan-bridge`'s `POST /scan` call is synchronous end-to-end today: it
blocks through scan → processing → every destination's upload
*submission* (not upload completion) and returns the finished result
inline as `200 OK`. `GET /jobs/:id` below is not yet backed by an
implementation and currently always returns `501` — there is no async
job queue to poll (design doc sec. 7, Option A).

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
- Dispatch scan jobs to the `sane-runtime` container via a Unix
  socket, then the resulting pages to the `scan-processor` container
  over a second Unix socket, then each assembled document to the
  profile's configured destinations (ADR 0016; Paperless-ngx is the
  only destination built today) — all within the one synchronous
  `POST /scan` call
- Track job status in an in-memory queue; persist completed jobs to a
  small SQLite database for inspection — **not implemented yet**:
  `GET /jobs/:id` and the rest of `/jobs*` currently return `501`;
  `POST /scan` is fully synchronous instead (design doc sec. 7,
  Option A)
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

The OCR/image-processing pipeline worker. Takes a job's raw TIFF pages
from `scan-bridge` over a Unix socket and hands back the assembled,
processed document(s) the same way — it never touches a shared volume
and never calls back into `scan-bridge` or anywhere else.

**Responsibilities:**

- Serve `POST /process` (`multipart/mixed` request: JSON control
  payload + raw TIFF pages; `multipart/mixed` response: JSON metadata
  + assembled document(s)) and `GET /health` on a Unix-domain socket
  (`/run/scan-processor/scan-processor.sock` by default) —
  single-flight, a second concurrent request gets `409` immediately
- Deskew (`convert -deskew`), blank-page removal (mean-brightness
  threshold), and rotation correction (`tesseract --psm 0` + `convert
  -rotate`) — each independently profile-gated
- OCR via `tesseract` (`deu+eng` by default when enabled; **off by
  default**, matching this document's long-standing "Paperless does
  this better on the bigger Docker host" rationale) — produces a
  searchable PDF directly when `output_format=pdf` (Tesseract's own
  PDF output mode), no separate assemble-then-OCR step
- Format conversion and multi-page assembly (`qpdf` for PDF, `convert`
  for a multi-page TIFF) per the profile's `assembly.page_grouping`:
  `combined` merges a job's surviving pages into one document,
  `per_page` emits one document per surviving source page
- Return the assembled document(s), page counts, and any warnings to
  `scan-bridge` in the `POST /process` HTTP response itself — there is
  no consume directory, no atomic-rename dance, and no NFS write on
  this component's part. Where the resulting document(s) end up
  (Paperless-ngx today; NFS/SMB/fileee are designed as registry slots
  but not built) is entirely `scan-bridge`'s decision, made *after*
  this response, per the profile's `destinations` list (ADR 0016) —
  `scan-processor` does not know Paperless, or any other destination,
  exists

**Image:** `ghcr.io/strausmann/paperless-scan-bridge/scan-processor:vX.Y.Z`

**Why a separate container:** Tesseract, its language data
(`deu`+`eng`), and the ImageMagick/`qpdf` toolchain are a materially
different dependency surface and update cadence than `scan-bridge`'s
REST/dispatch code (ADR 0003) — keeping them in their own container
keeps `scan-bridge` small (its own `under 25 MB` goal above) and lets
the two update independently.

See [`components/scan-processor/README.md`](components/scan-processor/README.md)
for the authoritative API contract, configuration, and pipeline-stage
list, and
[`docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`](docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)
sec. 4 for the design that replaced this document's original
shared-volume/inotify/callback sketch.

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
