# Container Suite — Detailed Specification

> **Status:** Draft v1.0
> **Last updated:** 2026-04-30
> **Author:** Björn Strausmann
> **Audience:** Maintainers, contributors, forking developers, AI
> coding assistants. Anyone who needs to build, debug, or extend the
> container suite.

## Purpose of this document

This is the technical reference for the three custom container images
that make up `paperless-scan-bridge`. It is the source of truth for:

- What each container contains and what it does
- How they interact with each other and with the host
- How they are built, tagged, signed, and published
- How USB device handling works across the host-container boundary
- How the build pipeline works in CI and on a developer workstation
- How a contributor adds a feature, fixes a bug, or releases a new version

Anything implementation-specific that does not belong in
`ARCHITECTURE.md` (which is for the bigger picture) or `CONCEPT.md`
(which is for the strategic reasoning) lives here.

If you are about to write a Dockerfile, a Go module, a compose file,
or a CI workflow for this project, read this document first.

---

## Table of contents

1. [Suite overview](#1-suite-overview)
2. [Design principles for containers](#2-design-principles-for-containers)
3. [Image strategy](#3-image-strategy)
4. [Component: scan-bridge](#4-component-scan-bridge)
5. [Component: sane-runtime](#5-component-sane-runtime)
6. [Component: scan-processor](#6-component-scan-processor)
7. [Inter-container communication](#7-inter-container-communication)
8. [Volume and filesystem layout](#8-volume-and-filesystem-layout)
9. [USB device handling](#9-usb-device-handling)
10. [udev rules and host preparation](#10-udev-rules-and-host-preparation)
11. [Build pipeline](#11-build-pipeline)
12. [Multi-architecture builds](#12-multi-architecture-builds)
13. [Image signing and provenance](#13-image-signing-and-provenance)
14. [Local development workflow](#14-local-development-workflow)
15. [Testing strategy per container](#15-testing-strategy-per-container)
16. [Configuration and secrets](#16-configuration-and-secrets)
17. [Logging and observability](#17-logging-and-observability)
18. [Resource limits and tuning](#18-resource-limits-and-tuning)
19. [Update and rollback strategy](#19-update-and-rollback-strategy)
20. [Release process](#20-release-process)
21. [Security hardening per container](#21-security-hardening-per-container)
22. [Troubleshooting matrix](#22-troubleshooting-matrix)
23. [Future considerations](#23-future-considerations)

---

## 1. Suite overview

The container suite consists of three custom images plus several
adopted upstream images. The three custom images are:

| Image            | Role                                      | Base image            | Approx size |
| ---------------- | ----------------------------------------- | --------------------- | ----------- |
| `scan-bridge`    | REST daemon, profile dispatch, metrics    | `gcr.io/distroless/static-debian12` | ~20 MB |
| `sane-runtime`   | SANE drivers, scanbd, USB integration     | `debian:12-slim`      | ~180 MB |
| `scan-processor` | Image processing and PDF assembly         | `gcr.io/distroless/base-debian12` | ~45 MB |

Total footprint on a Pi: approximately 250 MB across the three.
Adopted upstream images (Paperless, scanservjs, watchtower,
node-exporter) add another ~700 MB; the total deployment fits
comfortably on a Pi 5 with the recommended 8 GB RAM and an SSD.

### 1.1 Why three containers, not one

A single mega-image was considered and rejected for these reasons:

**Independent update cadence.** SANE updates land on a different
schedule than our daemon. Bundling them forces a rebuild of the daemon
every time a SANE security patch lands.

**Separation of privilege.** The sane-runtime container needs USB
device access. The bridge daemon does not. The processor does not.
Splitting them lets each run with the minimum privilege it actually
needs.

**Easier debugging.** When something fails, knowing whether it failed
in dispatch, in the SANE call, or in PDF assembly narrows the
diagnostic surface dramatically.

**Easier scaling.** PDF processing is CPU-intensive. On a heavy day
you might run two processor containers behind a queue. Splitting
keeps that option open.

**Smaller blast radius.** A vulnerability in ImageMagick affects only
the processor. A vulnerability in scanbd affects only sane-runtime.

The downside of three containers is some inter-process communication
overhead and three sets of CI workflows. We accept that.

### 1.2 Naming and ownership

All custom images are published to GitHub Container Registry under
the namespace `ghcr.io/strausmann/paperless-scan-bridge/`. The full
image references are:

- `ghcr.io/strausmann/paperless-scan-bridge/scan-bridge`
- `ghcr.io/strausmann/paperless-scan-bridge/sane-runtime`
- `ghcr.io/strausmann/paperless-scan-bridge/scan-processor`

All images are MIT-licensed via the repository's LICENSE file.

---

## 2. Design principles for containers

These principles drive every decision about container construction.
They are not suggestions; they are requirements.

### 2.1 Distroless or slim where possible

Production-track images use:

- **distroless** for compiled-language services (scan-bridge,
  scan-processor) — no shell, no package manager, no curl, nothing
  for an attacker to leverage if they get RCE.
- **debian:12-slim** for the SANE runtime, because SANE has many
  binary dependencies and dynamic linking that distroless does not
  satisfy.

We do not use Alpine. The musl-libc toolchain has historical
incompatibilities with Go cgo dependencies (gocv, leptonica), and the
size savings versus distroless are negligible.

### 2.2 No multi-purpose containers

Each container does one job. The bridge does API and dispatch. The
runtime does SANE. The processor does PDF assembly. If a feature does
not fit cleanly into one of these roles, it belongs in a new
container, not bolted onto an existing one.

### 2.3 Multi-stage builds always

Every Dockerfile is multi-stage:

- Stage 1: build environment with toolchain (Go compiler, build
  dependencies)
- Stage 2: runtime environment with only the artifacts (binary,
  runtime config defaults)

This keeps production images free of compilers, source code, build
artifacts, and the security surface they bring.

### 2.4 Reproducible builds where feasible

Reproducibility is a goal but not an absolute requirement. We pin:

- Base image digests (not just tags)
- Go module versions via `go.sum`
- Debian package versions via apt pinning where it matters
- Build-time tools (Go version, buildx version) in CI

We do not yet target bit-identical reproducibility (which would
require careful timestamp handling and SOURCE_DATE_EPOCH propagation).
That is a Phase 4 goal.

### 2.5 No "latest" tags in production

Production compose files reference images by either:

- Semantic version tag: `scan-bridge:v1.2.3`
- Digest: `scan-bridge@sha256:abc123...`

Renovate manages updates by opening PRs. A human reviews and merges.
This catches accidental breaking changes and surfaces what is being
updated.

The `latest` tag exists but points to the most recent stable release.
It is acceptable for development but never in production.

### 2.6 Non-root containers

All three containers run as a non-root user inside. The user is
created in the image with a known UID/GID:

- `scan-bridge` runs as UID 10001
- `sane-runtime` runs as UID 10002 (member of `scanner` group GID 10003)
- `scan-processor` runs as UID 10004

The sane-runtime is the only one that needs special host-side
permissions, via the `scanner` group GID matching what udev grants
the USB device.

### 2.7 Read-only root filesystem

Production compose files set `read_only: true` on every custom
container. Writable state goes into explicit named volumes or tmpfs
mounts. This prevents an attacker from writing modified binaries
even if they get code execution.

### 2.8 Drop all capabilities, add only what is needed

Default capability set is dropped via `cap_drop: [ALL]`. Only the
sane-runtime needs anything back, and only `CAP_SYS_RAWIO` for USB
control endpoints under specific kernel versions. Most modern
kernels do not require it; we test both paths.

---

## 3. Image strategy

### 3.1 Tagging scheme

Every image carries multiple tags simultaneously:

| Tag pattern              | Example          | Purpose                                  |
| ------------------------ | ---------------- | ---------------------------------------- |
| `vMAJOR.MINOR.PATCH`     | `v1.2.3`         | Exact release pin                        |
| `vMAJOR.MINOR`           | `v1.2`           | Floating patch updates                   |
| `vMAJOR`                 | `v1`             | Floating minor updates within major      |
| `latest`                 | `latest`         | Most recent stable release               |
| `edge`                   | `edge`           | Latest commit on main, may be unstable   |
| `pr-NNN`                 | `pr-42`          | PR builds for testing                    |
| `sha-SHORT`              | `sha-a1b2c3d`    | Exact commit reference                   |

Production deployments pin to `vMAJOR.MINOR.PATCH` or to a digest.
Renovate updates produce PRs against the pinned versions.

### 3.2 Versioning policy

Strict Semantic Versioning 2.0.0 across all three containers.

- **MAJOR** bumps when there is a breaking API change, a breaking
  configuration change, or a removal of supported functionality
- **MINOR** bumps for additive features that maintain backward
  compatibility
- **PATCH** bumps for bug fixes and security updates

The three containers version **independently**. They are not lockstep.
However, the compose files we ship reference compatible combinations,
and the CHANGELOG entries call out compatibility constraints.

For example: `scan-bridge v2.0.0` may require `sane-runtime >= v1.5.0`
because the dispatch protocol changed.

### 3.3 Release cadence

- **Patch releases:** as needed for bugs and security updates,
  typically within 48 hours of a confirmed issue
- **Minor releases:** roughly monthly during active development,
  quarterly thereafter
- **Major releases:** when warranted by accumulated breaking changes,
  with a documented migration path

### 3.4 LTS and maintenance

The most recent two minor versions of each container receive bug
fixes and security updates. Older versions are unsupported but
remain on GHCR for users who explicitly pin them.

### 3.5 Deprecation policy

Features deprecate over at least one minor version. Deprecation
shows up as:

- A log line in the container at startup if the deprecated config is in use
- A note in the CHANGELOG
- A note in the release notes
- A warning header on the API response if the deprecated endpoint is hit

After a minor version cycle of warning, the deprecated feature is
removed in the next major version.

---

## 4. Component: scan-bridge

The core daemon. This section is the most detailed because most
project work happens here.

### 4.1 Responsibility

`scan-bridge` is the public face of the system. It:

- Exposes the HTTP REST API on port 8080
- Receives webhook calls from Home Assistant, n8n, scanbd hooks, or
  any HTTP client
- Looks up scan profiles by name
- Dispatches scan jobs to `sane-runtime` over a Unix socket
- Coordinates with `scan-processor` for PDF assembly
- Tracks job status in a small embedded database
- Exports Prometheus metrics on port 9090
- Logs structured JSON to stdout
- Provides a synthetic health-check mode for monitoring

### 4.2 Source layout

```
components/scan-bridge/
├── Dockerfile
├── go.mod
├── go.sum
├── cmd/
│   └── scan-bridge/
│       └── main.go              # entry point, flag parsing, signal handling
├── internal/
│   ├── api/
│   │   ├── handlers.go          # HTTP handlers, thin
│   │   ├── handlers_test.go
│   │   ├── middleware.go        # auth, logging, metrics
│   │   └── routes.go            # route table
│   ├── config/
│   │   ├── config.go            # TOML parsing, env override, defaults
│   │   └── config_test.go
│   ├── profiles/
│   │   ├── profiles.go          # YAML parsing, validation
│   │   ├── profiles_test.go
│   │   └── defaults.yaml        # shipped default profiles
│   ├── dispatch/
│   │   ├── dispatch.go          # talks to sane-runtime via Unix socket
│   │   ├── dispatch_test.go
│   │   └── client.go            # HTTP-over-Unix-socket client
│   ├── jobs/
│   │   ├── jobs.go              # job state machine, persistence
│   │   ├── jobs_test.go
│   │   └── store.go             # bbolt or SQLite store
│   ├── metrics/
│   │   └── metrics.go           # Prometheus collectors
│   └── healthcheck/
│       ├── synthetic.go         # synthetic-scan worker
│       └── liveness.go          # /health endpoint logic
├── api/
│   ├── openapi.yaml             # OpenAPI 3.1 spec
│   └── schema/
│       ├── profile.json         # JSON Schema for profile YAML
│       └── job.json             # JSON Schema for job records
├── README.md
└── tests/
    ├── integration/
    │   └── full_flow_test.go    # spins up real containers, end-to-end
    └── fixtures/
        └── sample-profiles.yaml
```

### 4.3 Dockerfile

The production Dockerfile is multi-stage. Build stage uses the
official Go image; runtime stage uses distroless static.

```dockerfile
# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.22
ARG ALPINE_VERSION=3.19

# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS build

WORKDIR /src

# Cache Go modules separately from source
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build \
        -trimpath \
        -ldflags "-s -w \
            -X main.version=${VERSION} \
            -X main.commit=${COMMIT} \
            -X main.buildDate=${BUILD_DATE}" \
        -o /out/scan-bridge \
        ./cmd/scan-bridge

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

USER nonroot:nonroot

COPY --from=build /out/scan-bridge /usr/local/bin/scan-bridge
COPY --from=build /src/internal/profiles/defaults.yaml /etc/scan-bridge/profiles.yaml

EXPOSE 8080 9090

VOLUME ["/var/lib/scan-bridge"]

ENTRYPOINT ["/usr/local/bin/scan-bridge"]
CMD ["--config", "/etc/scan-bridge/config.toml"]
```

Key choices:

- `--mount=type=cache` for both the Go module cache and the build
  cache, dramatically speeding up incremental builds
- Cross-compilation via `$TARGETOS` and `$TARGETARCH` so a single
  build job emits both amd64 and arm64
- `CGO_ENABLED=0` for a fully static binary, allowing distroless static
- `-trimpath` for reproducibility
- `-ldflags "-s -w"` to strip debug symbols (saves ~5 MB)
- Version, commit, and build date injected via ldflags so `--version`
  output is meaningful
- Distroless `:nonroot` variant, so the container does not run as root
  by default

### 4.4 HTTP API

The full OpenAPI 3.1 spec lives in `components/scan-bridge/api/openapi.yaml`.
Summary:

| Endpoint            | Method | Purpose                                            |
| ------------------- | ------ | -------------------------------------------------- |
| `/health`           | GET    | Liveness check, returns 200 OK if daemon is up     |
| `/ready`            | GET    | Readiness check, returns 200 OK only if SANE container is reachable and at least one profile is loaded |
| `/metrics`          | GET    | Prometheus metrics                                 |
| `/profiles`         | GET    | List configured profiles                           |
| `/profiles/{name}`  | GET    | Detail of one profile                              |
| `/scan`             | POST   | Trigger a scan job                                 |
| `/jobs`             | GET    | List recent jobs (paginated, default last 50)      |
| `/jobs/{id}`        | GET    | Detail of one job, including status and result     |
| `/jobs/{id}/cancel` | POST   | Cancel an in-progress job                          |
| `/version`          | GET    | Build version, commit, date                        |

The `/scan` endpoint accepts:

```json
{
  "profile": "private-duplex",
  "metadata": {
    "source": "zigbee:styrbar:button-up",
    "user_note": "December bills"
  }
}
```

And returns `202 Accepted`:

```json
{
  "job_id": "01HJ9P5K2N6QXZ8R5T3VWBC4FE",
  "status": "queued",
  "created_at": "2026-04-30T14:23:01Z",
  "links": {
    "self": "/jobs/01HJ9P5K2N6QXZ8R5T3VWBC4FE",
    "cancel": "/jobs/01HJ9P5K2N6QXZ8R5T3VWBC4FE/cancel"
  }
}
```

Job IDs are ULIDs (Universally Unique Lexicographically Sortable
Identifiers) so they sort by creation time when listed.

### 4.5 Authentication

Two auth modes are supported:

**Mode 1: Token-based (default).** A bearer token in the
`Authorization` header. The token is a 32-byte random string,
configured via the `SCAN_BRIDGE_API_TOKEN` environment variable. The
token is hashed (SHA-256) on the server side; the hash is stored in
the config, not the plaintext.

```
Authorization: Bearer <token>
```

**Mode 2: Unauthenticated, IP-allowlisted.** For trusted-LAN setups
where the user does not want to manage tokens. The bridge accepts
unauthenticated requests from a configured list of source CIDRs.
Default off.

```toml
# /etc/scan-bridge/config.toml
[auth]
mode = "ip_allowlist"
allowed_cidrs = ["192.168.1.0/24", "10.42.0.0/16"]
```

Both modes log every request with the source IP and the auth result
(token-valid / token-invalid / cidr-match / cidr-reject).

### 4.6 Profile model

Profiles are loaded from a YAML file, default
`/etc/scan-bridge/profiles.yaml`. Schema:

```yaml
profiles:
  - name: private-duplex             # required, string, unique
    description: "Private documents, duplex, color, 300 DPI"
    source: "ADF Duplex"             # SANE source name
    resolution: 300                  # DPI, integer 100-1200
    mode: "Color"                    # Color | Gray | Lineart
    format: "pdf"                    # pdf | jpeg | tiff
    target_subdir: "private/"        # appended to consume base
    deskew: true                     # post-processing flag
    remove_blank: true               # post-processing flag
    rotate_pages: true               # auto-rotate via tesseract OSD
    page_size: "A4"                  # A4 | Letter | A5 | auto
    timeout_seconds: 300             # job timeout
    metadata_template:
      paperless_tags: ["private"]
      paperless_correspondent: null
```

The schema is validated at load time. Invalid profiles cause the
daemon to refuse to start with a clear error pointing to the line
number.

### 4.7 Job state machine

A scan job moves through these states:

```
       queued
          |
          v
      dispatched ----+
          |          |
          v          v
       scanning    failed (cannot reach sane-runtime)
          |
          v
      processing ---+
          |         |
          v         v
       completed  failed (PDF assembly error)
          |
          v
       archived (after 7 days, kept for inspection)
```

State transitions are persisted to embedded BoltDB so a daemon
restart does not lose track of in-flight jobs. Jobs in `dispatched`
or `scanning` state at startup are marked `failed` with reason
"daemon restart during execution" — we do not attempt to resume.

### 4.8 Metrics

The Prometheus exposition includes:

| Metric                                  | Type      | Description                              |
| --------------------------------------- | --------- | ---------------------------------------- |
| `scan_bridge_build_info`                | gauge     | Version and commit as labels             |
| `scan_bridge_jobs_total`                | counter   | Total jobs by profile and outcome        |
| `scan_bridge_job_duration_seconds`      | histogram | End-to-end job duration                  |
| `scan_bridge_dispatch_duration_seconds` | histogram | Time from queue to sane-runtime hand-off |
| `scan_bridge_scan_duration_seconds`     | histogram | Time spent in sane-runtime               |
| `scan_bridge_processing_duration_seconds` | histogram | Time spent in scan-processor          |
| `scan_bridge_queue_depth`               | gauge     | Current queue depth                      |
| `scan_bridge_active_jobs`               | gauge     | Currently in flight                      |
| `scan_bridge_api_requests_total`        | counter   | API requests by endpoint and status      |
| `scan_bridge_api_request_duration_seconds` | histogram | API latency                          |
| `scan_bridge_synthetic_check_total`     | counter   | Synthetic health checks by outcome       |

All histograms use the standard Prometheus default buckets, plus an
additional bucket at 60 seconds for the long-running scan operations.

### 4.9 Logging

Structured JSON, one log line per event. Schema:

```json
{
  "time": "2026-04-30T14:23:01.234Z",
  "level": "info",
  "msg": "scan job dispatched",
  "trace_id": "01HJ9P5K...",
  "job_id": "01HJ9P5K2N6QXZ8R5T3VWBC4FE",
  "profile": "private-duplex",
  "source_ip": "192.168.1.42",
  "duration_ms": 12
}
```

Level vocabulary: `debug`, `info`, `warn`, `error`. No `fatal` or
`panic` — those exit the process and we want a structured log line
for every exit reason.

The `trace_id` field carries through the entire job's lifetime,
including across container boundaries. The bridge generates it; it
passes it to sane-runtime in the dispatch call; sane-runtime echoes
it back; scan-processor receives it via the job metadata; all logs
referencing the job include it.

### 4.10 Configuration loading order

In order of precedence, lowest first:

1. Compiled-in defaults
2. `/etc/scan-bridge/config.toml` (mounted into container)
3. Environment variables prefixed `SCAN_BRIDGE_`
4. Command-line flags

A typical production setup uses the TOML file for static config and
environment variables for secrets (the API token).

### 4.11 Graceful shutdown

On SIGTERM:

1. Stop accepting new requests on the API socket
2. Allow in-flight HTTP requests to complete (with 30s timeout)
3. Mark queued jobs as `cancelled_at_shutdown`
4. Allow currently dispatched jobs to complete
5. Flush metrics one last time
6. Close the database
7. Exit 0

On SIGINT (Ctrl-C in development): the same, but with a 5s timeout
instead of 30s.

If shutdown takes longer than 60 seconds, log an error and exit 1.

### 4.12 Resource expectations

Steady state:

- Memory: 30-60 MB resident
- CPU: 1-3% on a Pi 5 idle, brief spikes to 20-40% during dispatch
- Disk: ~50 KB for the database after 1000 jobs
- Network: dependent on scan rate; typically <1 Mbps

These are validated in CI with a load test against a mocked
sane-runtime that simulates 10 jobs per minute for an hour.

---

## 5. Component: sane-runtime

The container that owns the scanner.

### 5.1 Responsibility

`sane-runtime` is the only container that touches USB. It:

- Provides `scanimage`, `scanbd`, `sane-utils`, `sane-airscan`
- Exposes a thin HTTP API on a Unix socket for the bridge to call
- Optionally runs `scanbd` daemon if hardware buttons are configured
- Detects the scanner via `scanimage -L` on startup and on USB
  hotplug events
- Performs scans on demand and writes raw output to a shared volume
- Reports scanner status via the API and via Prometheus metrics

### 5.2 Source layout

```
components/sane-runtime/
├── Dockerfile
├── etc/
│   ├── sane.d/
│   │   ├── dll.conf             # enabled SANE backends
│   │   └── avision.conf         # Kodak i1120 specific
│   ├── scanbd/
│   │   ├── scanbd.conf          # main scanbd config
│   │   └── scanner.d/
│   │       └── kodak-i1120.conf # device-specific scanbd rules
│   └── scan-runtime.toml        # our HTTP wrapper config
├── usr/local/bin/
│   ├── scan-runtime              # Go HTTP wrapper
│   ├── scanbd-hook.sh            # called by scanbd on button press
│   └── healthcheck.sh            # used by Docker HEALTHCHECK
├── usr/local/lib/scan-runtime/
│   └── runtime/                  # Go source for the wrapper
│       ├── main.go
│       ├── scanner.go            # scanner detection
│       ├── server.go             # Unix-socket HTTP server
│       └── *_test.go
├── README.md
└── tests/
    ├── unit/
    └── integration/
        └── mock-scanner/         # virtual scanner for CI
```

### 5.3 Dockerfile

Two-stage build. The first stage compiles the Go HTTP wrapper. The
second stage installs SANE on Debian slim and copies the wrapper in.

```dockerfile
# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.22
ARG DEBIAN_VERSION=bookworm

# ---- Build stage for the Go wrapper ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

COPY usr/local/lib/scan-runtime/runtime/go.mod \
     usr/local/lib/scan-runtime/runtime/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY usr/local/lib/scan-runtime/runtime/ ./

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/scan-runtime \
        ./

# ---- Runtime stage ----
FROM debian:${DEBIAN_VERSION}-slim AS runtime

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        sane-utils \
        sane-airscan \
        scanbd \
        libsane-extras \
        usbutils \
        tini \
        curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user, member of scanner group
RUN groupadd -g 10003 scanner && \
    useradd -u 10002 -g 10003 -m -s /usr/sbin/nologin sanesvc

# Copy the SANE config and our wrapper
COPY etc/sane.d/ /etc/sane.d/
COPY etc/scanbd/ /etc/scanbd/
COPY etc/scan-runtime.toml /etc/scan-runtime.toml
COPY usr/local/bin/scanbd-hook.sh /usr/local/bin/scanbd-hook.sh
COPY usr/local/bin/healthcheck.sh /usr/local/bin/healthcheck.sh
COPY --from=build /out/scan-runtime /usr/local/bin/scan-runtime

RUN chmod +x /usr/local/bin/scanbd-hook.sh \
              /usr/local/bin/healthcheck.sh

USER sanesvc:scanner

VOLUME ["/var/run/scan-runtime", "/var/scans"]

EXPOSE 8081 9091

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/healthcheck.sh"]

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/scan-runtime"]
CMD ["--config", "/etc/scan-runtime.toml"]
```

Key choices:

- `tini` as PID 1 because `scanbd` may spawn child processes; tini
  reaps zombies properly
- The `scanner` group with explicit GID 10003 must match the GID
  granted by the host udev rule (covered later)
- `sane-airscan` included to support modern eSCL/AirScan-capable
  network scanners as an optional extension
- `usbutils` for `lsusb` debugging — slightly larger image, much
  faster troubleshooting
- HEALTHCHECK runs every 30 seconds; the script verifies that
  `scanimage -L` returns at least one device

### 5.4 Internal HTTP API

Listens on a Unix socket at `/var/run/scan-runtime/api.sock` shared
with the bridge container via a named volume.

| Endpoint           | Method | Purpose                                |
| ------------------ | ------ | -------------------------------------- |
| `/health`          | GET    | Liveness, returns 200 if daemon is up  |
| `/ready`           | GET    | Readiness, returns 200 if scanner detected |
| `/metrics`         | GET    | Prometheus metrics on TCP 9091         |
| `/scanners`        | GET    | List detected scanners                 |
| `/scanners/{id}`   | GET    | Capabilities of one scanner            |
| `/scan`            | POST   | Execute a scan with profile parameters |
| `/scan/{id}/cancel`| POST   | Cancel an in-progress scan             |
| `/buttons`         | GET    | Last button press state (if scanbd active) |

The `/scan` endpoint receives:

```json
{
  "job_id": "01HJ9P5K...",
  "trace_id": "01HJ9P5K...",
  "device": "avision:libusb:001:002",
  "source": "ADF Duplex",
  "resolution": 300,
  "mode": "Color",
  "format": "tiff",
  "output_dir": "/var/scans/01HJ9P5K.../"
}
```

Returns `202 Accepted`:

```json
{
  "job_id": "01HJ9P5K...",
  "status": "scanning",
  "scanner_busy": false
}
```

The wrapper invokes `scanimage` with the appropriate flags via
`os/exec.CommandContext`. Stdout streams to a temp file in the
output directory; stderr is captured for diagnostics. The wrapper
parses scanimage output (page count, errors) and posts status
updates back to the bridge via the dispatch callback URL.

### 5.5 Scanner detection

On startup and on `SIGUSR1` (sent by udev events from outside), the
wrapper runs `scanimage -L` and parses the output. Detected scanners
are cached in memory with their full capability information from
`scanimage -A -d <device>`.

If no scanners are found, the wrapper still starts but `/ready`
returns 503 until a scanner appears. This allows the container to
start in a known-good state even if the user has not yet plugged in
their scanner.

### 5.6 scanbd integration

scanbd polls the scanner buttons and triggers the configured hook
script when a button is pressed. The hook script is a thin wrapper
that POSTs to the bridge's `/scan` endpoint.

`/etc/scanbd/scanbd.conf` excerpt:

```
global {
    debug      = false
    debug-level = 2
    user       = sanesvc
    group      = scanner

    saned      = ""    # we don't use saned proxy mode
    saned_opt  = {}

    scriptdir  = /etc/scanbd/scripts
    timeout    = 500   # ms between polls

    environment {
        device = "SCANBD_DEVICE"
        action = "SCANBD_ACTION"
    }

    function function_knob {
        filter = "^message.*"
        desc   = "Profile counter from LCD"
        env    = "SCANBD_FUNCTION"
    }

    multiple_actions = true
}

include(scanner.d/kodak-i1120.conf)
```

`/etc/scanbd/scanner.d/kodak-i1120.conf`:

```
device kodak-i1120 {
    filter = "^avision.*"
    desc   = "Kodak ScanMate i1120"

    function function_knob {
        filter = "^message.*"
        env    = "SCANBD_FUNCTION"
    }

    action scan {
        filter = "^scan.*"
        numerical-trigger {
            from-value = 1
            to-value   = 0
        }
        desc   = "Scan button pressed"
        script = "scanbd-hook.sh"
    }
}
```

The hook script:

```bash
#!/bin/bash
set -euo pipefail

# Map LCD profile counter to a profile name
case "${SCANBD_FUNCTION:-1}" in
    1)  PROFILE="private-simplex"   ;;
    2)  PROFILE="private-duplex"    ;;
    3)  PROFILE="business-simplex"  ;;
    4)  PROFILE="business-duplex"   ;;
    5)  PROFILE="receipt"           ;;
    6)  PROFILE="photo"             ;;
    7)  PROFILE="document-archive"  ;;
    8)  PROFILE="legal-size"        ;;
    9)  PROFILE="ad-hoc"            ;;
    *)  PROFILE="private-simplex"   ;;
esac

curl --silent --fail \
     --max-time 10 \
     --header "Authorization: Bearer ${SCAN_BRIDGE_TOKEN}" \
     --header "Content-Type: application/json" \
     --data "{\"profile\": \"${PROFILE}\", \"metadata\": {\"source\": \"scanbd:hardware-button\"}}" \
     "http://scan-bridge:8080/scan"
```

The mapping between LCD counter and profile name is a configurable
artifact, not hardcoded. It lives in the same TOML file as the rest
of the runtime config.

### 5.7 Resource expectations

- Memory: 80-150 MB resident (debian-slim is heavier)
- CPU: 5-15% during a scan, near zero idle
- Disk: ~50 MB scratch space per scan in `/var/scans` (tmpfs)
- USB: bus-rate, dependent on scanner

---

## 6. Component: scan-processor

The pipeline worker.

> **Status (2026-08-13):** sections 6.1–6.7 below are this document's
> *original design sketch* and predate the actual implementation on
> several points that matter — most importantly the transport (6.1's
> "shared volume" + "callback URL" was never built; see the sec. 7.2
> correction below) and the processing toolchain (6.3's Leptonica
> CGO bindings + pdfcpu Go library were replaced by shelling out to
> `convert(1)`/`tesseract(1)`/`qpdf(1)`, per
> `components/scan-processor/internal/pipeline/exec_pipeline.go`).
> The authoritative, up-to-date description of what is actually built
> — API surface, configuration, pipeline stages, source layout — is
> [`components/scan-processor/README.md`](components/scan-processor/README.md);
> the design that replaced this section's transport/responsibility
> model is
> [`docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`](docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)
> sec. 4. The rest of this section is kept for historical/roadmap
> context (e.g. the blank-page threshold reasoning in 6.6 still
> matches the shipped default), not as a description of the running
> system.

### 6.1 Responsibility

`scan-processor` is the OCR/image-processing pipeline. It:

- Serves `POST /process` on a Unix-domain socket (`scan-bridge` dials
  it directly, HTTP over Unix socket — the same pattern `scan-bridge`
  already uses to dial `sane-runtime`, sec. 7.1) — **not** a shared
  volume it watches
- Receives a job's raw TIFF pages as `multipart/mixed` request-body
  parts, alongside a JSON control payload carrying the profile's
  processing flags
- Applies deskew (`convert -deskew`), blank-page removal
  (mean-brightness threshold), and rotation correction (`tesseract
  --psm 0` orientation detection + `convert -rotate`) — each
  independently profile-gated
- Runs OCR via `tesseract` (`deu+eng` default, off by default overall)
  when the profile enables it — producing a searchable PDF directly
  for `output_format=pdf`
- Converts to the profile's `output_format` and assembles pages per
  `assembly.page_grouping` (`qpdf`/`convert`)
- Returns the assembled document(s) as `multipart/mixed` response-body
  parts, in the **same** `POST /process` HTTP response — **not** a
  write to a consume directory, and **not** a callback to the bridge.
  `scan-processor` does not know Paperless-ngx, or any other
  destination, exists

### 6.2 Source layout

> This tree is the original design sketch (Leptonica CGO bindings,
> pdfcpu, a bridge-callback `jobs/client.go`) and does not match the
> built module. The real layout — `cmd/scan-processor/main.go`,
> `internal/procapi/` (HTTP handlers, routes, multipart (de)coding),
> `internal/pipeline/` (the `Pipeline` interface + the `convert(1)`/
> `tesseract(1)`/`qpdf(1)`-shelling `ExecPipeline`), no `go.sum` (no
> third-party dependencies) — is documented in
> [`components/scan-processor/README.md`](components/scan-processor/README.md#layout).

```
components/scan-processor/
├── Dockerfile
├── go.mod
├── go.sum
├── cmd/
│   └── scan-processor/
│       └── main.go
├── internal/
│   ├── pipeline/
│   │   ├── pipeline.go          # orchestrator
│   │   ├── deskew.go            # deskew logic
│   │   ├── blank.go             # blank page detection
│   │   ├── rotate.go            # rotation correction
│   │   ├── pdf.go               # PDF assembly via pdfcpu
│   │   └── *_test.go
│   ├── atomic/
│   │   ├── write.go             # atomic write via O_TMPFILE + linkat
│   │   └── write_test.go
│   ├── config/
│   │   └── config.go
│   ├── jobs/
│   │   └── client.go            # callback to scan-bridge
│   └── metrics/
│       └── metrics.go
├── README.md
└── tests/
    ├── fixtures/
    │   ├── sample-batch/        # known-good test batch
    │   ├── blank-pages/         # for blank detection tests
    │   └── skewed-pages/        # for deskew tests
    └── integration/
        └── full_pipeline_test.go
```

### 6.3 Dockerfile

Distroless base because we do not need a shell once compiled.

```dockerfile
# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.22

# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

# We use CGO for Leptonica bindings; needs cross-compilation toolchain
RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc-aarch64-linux-gnu \
        libc6-dev-arm64-cross \
    && rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ "$TARGETARCH" = "arm64" ] && [ "$BUILDPLATFORM" = "linux/amd64" ]; then \
        export CC=aarch64-linux-gnu-gcc; \
    fi && \
    CGO_ENABLED=1 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/scan-processor \
        ./cmd/scan-processor

# ---- Runtime stage ----
FROM gcr.io/distroless/base-debian12:nonroot

USER nonroot:nonroot

# Leptonica requires libleptonica and its dependencies
COPY --from=build /usr/lib/aarch64-linux-gnu/liblept*.so* /usr/lib/aarch64-linux-gnu/
COPY --from=build /usr/lib/aarch64-linux-gnu/libtiff*.so* /usr/lib/aarch64-linux-gnu/
COPY --from=build /usr/lib/aarch64-linux-gnu/libpng*.so* /usr/lib/aarch64-linux-gnu/
COPY --from=build /usr/lib/aarch64-linux-gnu/libjpeg*.so* /usr/lib/aarch64-linux-gnu/
COPY --from=build /out/scan-processor /usr/local/bin/scan-processor

VOLUME ["/var/scans", "/mnt/consume"]

ENTRYPOINT ["/usr/local/bin/scan-processor"]
CMD ["--config", "/etc/scan-processor/config.toml"]
```

### 6.4 Pipeline stages

The pipeline is configured per profile but follows the same general
flow:

1. **Input validation**: confirm raw images exist at the expected
   paths, are readable, and match expected formats (TIFF or JPEG)
2. **Deskew** (optional, profile-controlled): each image gets
   skew-corrected via Leptonica's `pixDeskew`
3. **Blank page detection** (optional): a page is "blank" if more
   than 99.5% of its pixels are within 5% of the page's median
   brightness. Configurable threshold.
4. **Rotation correction** (optional): Tesseract orientation script
   detection (OSD); if confidence is high and angle is non-zero,
   apply rotation
5. **PDF assembly**: pdfcpu writes a PDF with one image per page,
   JPEG-compressed at quality 85 by default
6. **Atomic write**: PDF is written to a tmp file in the same
   directory as the target, then `linkat`'d to the final name. This
   ensures Paperless never sees a partial file.
7. **Cleanup**: working directory is removed; metrics are emitted

Each stage logs its duration and outcome. Any stage failure aborts
the job and reports back to the bridge.

### 6.5 Atomic writes

This is subtle and important. The naive approach — write the PDF, then
move it — has a race condition where Paperless's inotify can fire on
the intermediate file. The correct approach uses `O_TMPFILE` which
creates an unnamed file linked only to its inode, then `linkat` to
give it a name once it is fully written.

```go
// internal/atomic/write.go

func WriteAtomic(dir, finalName string, data []byte) error {
    fd, err := unix.Open(dir, unix.O_TMPFILE|unix.O_RDWR, 0644)
    if err != nil {
        return fmt.Errorf("open tmpfile: %w", err)
    }
    defer unix.Close(fd)

    if _, err := unix.Write(fd, data); err != nil {
        return fmt.Errorf("write tmpfile: %w", err)
    }

    if err := unix.Fdatasync(fd); err != nil {
        return fmt.Errorf("fdatasync: %w", err)
    }

    procPath := fmt.Sprintf("/proc/self/fd/%d", fd)
    finalPath := filepath.Join(dir, finalName)

    if err := unix.Linkat(unix.AT_FDCWD, procPath,
                          unix.AT_FDCWD, finalPath,
                          unix.AT_SYMLINK_FOLLOW); err != nil {
        return fmt.Errorf("linkat: %w", err)
    }

    return nil
}
```

This pattern works on any modern Linux filesystem (ext4, btrfs, xfs).
It also works on NFSv4 with the right server support — Synology DSM
7+ supports it.

### 6.6 Blank page detection

The algorithm uses Leptonica's pixel-density tools:

```go
// internal/pipeline/blank.go

func IsBlank(img *leptonica.Pix, threshold float64) (bool, error) {
    gray, err := leptonica.PixConvertTo8(img, false)
    if err != nil {
        return false, err
    }
    defer gray.Destroy()

    histogram, err := gray.Histogram()
    if err != nil {
        return false, err
    }

    median := histogram.Median()
    nearMedian := histogram.PercentageWithin(median, 5)

    return nearMedian > threshold, nil
}
```

The threshold is per-profile. Default 0.995 (99.5% near-median pixels).
For receipts and forms with sparse content, lower to 0.985. For
photographs and dense scanned text, the algorithm rarely false-flags;
it is safe to leave on.

### 6.7 Resource expectations

- Memory: 80-200 MB during processing (image buffers)
- CPU: high during deskew and PDF assembly, near zero between jobs
- Disk: scratch space equal to roughly 2x the input batch size,
  located in tmpfs
- A 10-page color batch at 300 DPI processes in 5-15 seconds on a Pi 5

---

## 7. Inter-container communication

Three communication paths exist between the custom containers.

### 7.1 Bridge to sane-runtime: HTTP over Unix socket

The bridge dispatches scan requests to sane-runtime via HTTP over a
Unix socket. Both containers mount a shared named volume at
`/var/run/scan-runtime/`. The socket is `api.sock` in that volume.

Why Unix socket instead of TCP:

- No port management
- Implicit network isolation
- Slightly lower latency
- No accidental exposure if a network policy is misconfigured

The Go HTTP client uses a custom dialer:

```go
client := &http.Client{
    Transport: &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
            return net.Dial("unix", "/var/run/scan-runtime/api.sock")
        },
    },
    Timeout: 30 * time.Second,
}
```

The compose file:

```yaml
services:
  scan-bridge:
    volumes:
      - sane-socket:/var/run/scan-runtime

  sane-runtime:
    volumes:
      - sane-socket:/var/run/scan-runtime

volumes:
  sane-socket:
```

### 7.2 scan-bridge to scan-processor: HTTP over a second Unix socket

> **Status (2026-08-13):** this section originally described
> "sane-runtime to scan-processor: shared volume + callback". That
> model was never built. What ships today mirrors sec. 7.1 exactly,
> one level further down the pipeline — see
> `docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`
> sec. 4.2 for the design and
> [`components/scan-bridge/internal/procclient`](components/scan-bridge/internal/procclient/procclient.go)
> for the frozen wire contract both sides implement.

`sane-runtime` never talks to `scan-processor` — it only ever talks to
`scan-bridge` (sec. 7.1). `scan-bridge` is the orchestrator: once it
has written a completed scan's raw TIFF pages to its own `OutputDir`
(the same volume sec. 7.1's dispatch already writes pages to), it
re-reads them off disk and POSTs them to `scan-processor` over a
**second, separate** Unix socket — a new named volume/socket at
`/run/scan-processor/scan-processor.sock`, mounted only by
`scan-bridge` and `scan-processor` (never by `sane-runtime`).

The request is `multipart/mixed`: part 0 a JSON control payload
(OCR/deskew/blank/rotate flags, `page_grouping`, `output_format`,
`timeout_seconds`), parts 1..N the job's TIFF pages. `scan-processor`
replies `multipart/mixed` in the **same HTTP response**: part 0 a JSON
metadata block (`request_id`, one `documents[]` entry per assembled
document, `duration_ms`), parts 1..N the assembled document bytes.

There is no shared volume for image bytes between `scan-bridge` and
`scan-processor` (each side only ever sees the other's bytes as an
HTTP multipart part), and no callback endpoint — `scan-bridge` never
exposes anything like the `/internal/jobs/<id>/complete` URL this
section originally sketched, because `scan-processor` has nothing to
call back to: it answers within the same request it received.

### 7.3 Scan-bridge as the orchestrator

`scan-bridge` is still the single component every other container
talks to — `sane-runtime` and `scan-processor` never talk to each
other, and a caller never talks to either of them directly (unchanged
from the original design intent). What differs from this section's
original wording is *how* status reaches the bridge: there is no
push-style "job state update" stream from `sane-runtime` or
`scan-processor` today. Instead, `scan-bridge`'s single `POST /scan`
call blocks synchronously through dispatch → processing → every
configured destination's delivery, and the finished outcome (which
pages were captured, what `scan-processor` assembled, what each
destination reported) is returned inline as that call's `200 OK`
response body — there is no job-state store for a caller to poll
separately (`GET /jobs/:id` returns `501`; design doc sec. 7,
Option A).

---

## 8. Volume and filesystem layout

### 8.1 Named volumes

> **Status (2026-08-13):** the `scan-scratch` row below (`sane-runtime`
> and `processor` sharing one tmpfs) describes the shared-volume model
> sec. 7.2 corrects — it was never built, and there is no volume
> mounted by both `sane-runtime` and `scan-processor` today.
> `sane-runtime` and `scan-processor` each get their own socket volume
> shared only with `scan-bridge` (`sane-socket` and, new,
> `scan-processor-socket`); raw pages only ever live under
> `scan-bridge`'s own `OutputDir` bind mount. The deployed `compose.yaml`
> also uses bind mounts under `/docker/stacks/paperless-scan-bridge/`
> rather than the named volumes this table sketches — see `compose.yaml`
> for the actual, current volume layout.

| Volume name        | Mounted by              | Purpose                                |
| ------------------ | ----------------------- | -------------------------------------- |
| `sane-socket`      | bridge, sane-runtime    | Unix socket for dispatch               |
| `scan-scratch`     | sane-runtime, processor | **superseded** — tmpfs for raw scan output; never built, see status note above |
| `bridge-data`      | bridge                  | Job database (BoltDB)                  |
| `bridge-config`    | bridge                  | Read-only config files                 |
| `runtime-config`   | sane-runtime            | Read-only config files                 |
| `processor-config` | scan-processor          | Read-only config files                 |

The `scan-scratch` volume is a tmpfs:

```yaml
volumes:
  scan-scratch:
    driver: local
    driver_opts:
      type: tmpfs
      device: tmpfs
      o: size=512m,uid=10002,gid=10003
```

This keeps raw scan data in RAM, never touching the SD card. On a Pi
with 8 GB RAM, 512 MB tmpfs is comfortable; raise it for high-volume
setups.

> **Sizing note carried forward (2026-08-14, issue #47):** this
> section's `scan-scratch` example is superseded (see the status note
> above) and was never built, but its "512 MB is comfortable"
> figure is the deliberate reference point for the tmpfs `compose.yaml`
> *does* actually mount today: scan-processor's `/tmp` scratch
> directory (`internal/pipeline/exec_pipeline.go`'s
> `os.MkdirTemp`), sized `1g` there — headroom above
> `internal/procapi`'s `defaultMaxRequestBytes` (512 MiB, the
> `POST /process` request-body cap `http.MaxBytesReader` enforces).
> Both numbers derive from the same real page size: the repo's own
> `deploy/profiles/default.yaml` scans at 300 DPI/Color/A4, which is
> ≈25 MiB per page uncompressed TIFF, and `procclient` sends every
> page of a scan in one request — see `internal/procapi/api.go`'s
> `defaultMaxRequestBytes` doc comment for the full derivation. If
> either number changes, keep both — and this note — in sync.

### 8.2 Bind mounts

Two bind mounts cross from host into containers:

| Host path                          | Container path        | Mounted by              |
| ---------------------------------- | --------------------- | ----------------------- |
| `/dev/scanner-i1120`               | `/dev/scanner`        | sane-runtime            |
| `/mnt/synology/consume`            | `/mnt/consume`        | scan-processor, paperless |

The first is the udev-managed scanner symlink (covered in section 9).
The second is the NFS-mounted consume directory.

### 8.3 Configuration directory layout

On the Pi host:

```
/etc/paperless-scan-bridge/
├── compose/
│   ├── docker-compose.yml          # The active compose file
│   └── .env                        # Environment overrides
├── bridge/
│   ├── config.toml                 # Daemon configuration
│   └── profiles.yaml               # Profile definitions
├── runtime/
│   ├── scan-runtime.toml           # Runtime wrapper config
│   └── scanbd/                     # scanbd config tree
├── processor/
│   └── config.toml
└── secrets/
    ├── api-token.age               # SOPS-encrypted API token
    └── restic-repo.age             # SOPS-encrypted restic password
```

This is mounted into containers read-only. Compose files reference
these paths with `:ro` flags.

---

## 9. USB device handling

This section is the most technically subtle. Getting USB right is the
difference between "works once and breaks after the first reboot" and
"works for years."

### 9.1 The problem

The Linux kernel assigns USB device numbers dynamically. When a
scanner enumerates, it gets a path like `/dev/bus/usb/001/004`. After
a power cycle or a reconnect, the same scanner might be at
`/dev/bus/usb/001/005` or `/dev/bus/usb/002/003`.

If a container binds the original path, the binding is wrong after
the first reconnect. The container cannot find the scanner. The user
is confused.

### 9.2 The solution: udev rules with stable symlinks

We create a udev rule that recognizes the scanner by USB vendor and
product ID, and creates a stable symlink at `/dev/scanner-<model>`.
The container binds the symlink path, which always points to the
current device.

```
# /etc/udev/rules.d/99-paperless-scan-bridge.rules

# Kodak ScanMate i1120 (vendor 0x040a, product 0x6013)
SUBSYSTEM=="usb", ATTRS{idVendor}=="040a", ATTRS{idProduct}=="6013", \
    MODE="0664", GROUP="scanner", \
    SYMLINK+="scanner-i1120", \
    ENV{ID_PAPERLESS_SCANNER}="kodak-i1120"

# Future scanner entries follow the same pattern
```

Three things this rule does:

1. Sets ownership/permissions to allow the `scanner` group access
2. Creates the symlink `/dev/scanner-i1120`
3. Tags the device with an environment variable for any other tools
   that want to react to it (e.g. a hotplug event handler)

### 9.3 The scanner group and UID mapping

The container runs as a non-root user (`sanesvc`, UID 10002) member
of group `scanner` (GID 10003). The udev rule grants the host's
`scanner` group access to the device.

For the container's GID 10003 to map to the host's `scanner` group
GID, the host group must have GID 10003. The bootstrap script creates
it:

```bash
groupadd --system --gid 10003 scanner
```

This is the same GID inside and outside the container, so the
permissions match.

### 9.4 Compose configuration for the device

```yaml
services:
  sane-runtime:
    devices:
      - /dev/scanner-i1120:/dev/scanner
    group_add:
      - 10003  # scanner group GID
    cap_drop:
      - ALL
    cap_add:
      - SYS_RAWIO  # required for some USB control endpoints
    security_opt:
      - no-new-privileges:true
```

The `devices` directive grants the container access to the specific
device node. The `group_add` ensures the container process has the
scanner group in its supplementary groups, matching the udev rule's
group.

### 9.5 Hotplug handling

When a scanner is unplugged and replugged, two things happen:

1. The udev rule re-runs and recreates the symlink at the new device
   path
2. The kernel delivers a USB hotplug event

The container's symlink-following access continues to work because
the symlink target moves with the device. Sane-runtime's HTTP API
re-runs `scanimage -L` on receipt of `SIGUSR1` to refresh its cached
device list.

For the SIGUSR1 to arrive, we use a host-side udev rule that runs a
script which sends the signal to the container:

```
# Same udev file
SUBSYSTEM=="usb", ATTRS{idVendor}=="040a", ATTRS{idProduct}=="6013", \
    ACTION=="add", RUN+="/usr/local/bin/notify-scan-runtime-add"
SUBSYSTEM=="usb", ATTRS{idVendor}=="040a", ATTRS{idProduct}=="6013", \
    ACTION=="remove", RUN+="/usr/local/bin/notify-scan-runtime-remove"
```

The `notify-scan-runtime-*` scripts use `docker exec` to send the
signal:

```bash
#!/bin/bash
# /usr/local/bin/notify-scan-runtime-add
docker kill --signal=USR1 sane-runtime 2>/dev/null || true
```

The `|| true` keeps udev quiet when the container is not running
(during boot, before the compose stack is up).

### 9.6 Why not USB-over-IP

We considered USB-over-IP (`usbip`) for cases where the scanner is
not on the same Pi as the bridge. Rejected because:

- Adds operational complexity (two daemons, network setup)
- Does not solve any problem in our reference setup (scanner on
  same Pi as bridge)
- Creates a brittle network dependency

Users who have a specific need can run USB-over-IP themselves and
point the bridge at the resulting `/dev/scanner-*` symlink. We
document the option but do not bundle it.

---

## 10. udev rules and host preparation

### 10.1 The shipped udev rule file

`deploy/udev/99-paperless-scan-bridge.rules` ships with one rule per
supported scanner. As the hardware compatibility list grows, this
file grows.

Pattern:

```
# <Vendor> <Model> (vendor 0x<VID>, product 0x<PID>)
SUBSYSTEM=="usb", ATTRS{idVendor}=="<vid>", ATTRS{idProduct}=="<pid>", \
    MODE="0664", GROUP="scanner", \
    SYMLINK+="scanner-<model-slug>", \
    ENV{ID_PAPERLESS_SCANNER}="<vendor>-<model>"

SUBSYSTEM=="usb", ATTRS{idVendor}=="<vid>", ATTRS{idProduct}=="<pid>", \
    ACTION=="add", \
    RUN+="/usr/local/bin/notify-scan-runtime-add"
SUBSYSTEM=="usb", ATTRS{idVendor}=="<vid>", ATTRS{idProduct}=="<pid>", \
    ACTION=="remove", \
    RUN+="/usr/local/bin/notify-scan-runtime-remove"
```

### 10.2 Bootstrap script flow

The bootstrap script `deploy/bootstrap/install.sh` does these things
in order:

1. **Verify host OS** (Ubuntu Server 22.04+, Debian 12+, fail with
   clear error otherwise)
2. **Install packages**: `docker-ce`, `docker-compose-plugin`,
   `nfs-common`, `cifs-utils`
3. **Create the `scanner` group** with GID 10003
4. **Install udev rules** to `/etc/udev/rules.d/`
5. **Install hotplug helper scripts** to `/usr/local/bin/`
6. **Reload udev**: `udevadm control --reload && udevadm trigger`
7. **Configure NFS mount** in `/etc/fstab` based on user input
8. **Create the systemd unit** for the compose stack
9. **Pull the container images** from GHCR
10. **Generate a default config tree** in `/etc/paperless-scan-bridge/`
11. **Print next steps** including the URL for the configuration
    review

The script is idempotent; running it twice is safe. Each step checks
before doing.

### 10.3 What the script never does

Equally important. The script:

- Never modifies firewall rules without asking
- Never writes anything outside the documented paths
- Never installs anything not on the documented list
- Never pulls binaries from arbitrary sources (only Docker, only from
  GHCR + Docker Hub for adopted upstreams)
- Never sends telemetry anywhere

A clean uninstall is `deploy/bootstrap/uninstall.sh` which reverses
each step.

---

## 11. Build pipeline

### 11.1 Local builds

A developer can build all three images with one command:

```bash
docker buildx bake --load
```

The `docker-bake.hcl` file at the repository root drives this:

```hcl
group "default" {
    targets = ["scan-bridge", "sane-runtime", "scan-processor"]
}

variable "VERSION" {
    default = "dev"
}

variable "REGISTRY" {
    default = "ghcr.io/strausmann/paperless-scan-bridge"
}

target "_common" {
    args = {
        VERSION = "${VERSION}"
    }
    platforms = ["linux/amd64", "linux/arm64"]
}

target "scan-bridge" {
    inherits = ["_common"]
    context = "components/scan-bridge"
    tags = [
        "${REGISTRY}/scan-bridge:${VERSION}",
        "${REGISTRY}/scan-bridge:latest"
    ]
}

target "sane-runtime" {
    inherits = ["_common"]
    context = "components/sane-runtime"
    tags = [
        "${REGISTRY}/sane-runtime:${VERSION}",
        "${REGISTRY}/sane-runtime:latest"
    ]
}

target "scan-processor" {
    inherits = ["_common"]
    context = "components/scan-processor"
    tags = [
        "${REGISTRY}/scan-processor:${VERSION}",
        "${REGISTRY}/scan-processor:latest"
    ]
}
```

For development on amd64 wanting only amd64 binaries (faster), set
`platforms = ["linux/amd64"]` in `target "_common"`.

### 11.2 CI builds

GitHub Actions workflow `.github/workflows/build-images.yml`:

```yaml
name: Build container images

on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:
    paths:
      - 'components/**'
      - 'docker-bake.hcl'
      - '.github/workflows/build-images.yml'

permissions:
  contents: read
  packages: write
  id-token: write   # for cosign keyless signing
  attestations: write

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to GHCR
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        run: |
          if [[ "${{ github.ref }}" == refs/tags/v* ]]; then
            VERSION="${GITHUB_REF_NAME}"
          else
            VERSION="edge-${GITHUB_SHA::7}"
          fi
          echo "version=${VERSION}" >> "$GITHUB_OUTPUT"

      - name: Build and push images
        uses: docker/bake-action@v4
        with:
          push: ${{ github.event_name != 'pull_request' }}
          load: ${{ github.event_name == 'pull_request' }}
          set: |
            *.args.VERSION=${{ steps.meta.outputs.version }}
            *.args.COMMIT=${{ github.sha }}
            *.args.BUILD_DATE=${{ github.event.head_commit.timestamp }}

      - name: Sign images with cosign (keyless)
        if: github.event_name != 'pull_request'
        env:
          DIGEST_BRIDGE: ${{ steps.bake.outputs.metadata }}
        run: |
          # cosign sign each pushed image
          # ... details in section 13

      - name: Generate SBOM
        if: github.event_name != 'pull_request'
        uses: anchore/sbom-action@v0
        with:
          image: ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:${{ steps.meta.outputs.version }}
          format: spdx-json
          output-file: sbom-scan-bridge.spdx.json

      - name: Run Trivy security scan
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:${{ steps.meta.outputs.version }}
          severity: HIGH,CRITICAL
          exit-code: 1
```

### 11.3 Caching strategy

The bake action benefits enormously from BuildKit cache. We use:

- GitHub Actions cache for layers (`type=gha`)
- Registry cache for cross-job sharing (`type=registry,ref=...`)

A typical full build from a cold cache takes 8-12 minutes for both
architectures. With warm cache, it drops to 1-3 minutes for an
incremental change.

### 11.4 Build matrix

We do not separately test each container in CI; the bake builds them
all. We do separately test:

- Go unit tests per component
- Linting per component (golangci-lint, hadolint)
- Integration tests against the assembled stack
- Security scanning of finished images

These run as parallel jobs, not as a matrix, because they have
different dependencies.

---

## 12. Multi-architecture builds

### 12.1 Target architectures

We build for two architectures:

- `linux/amd64` — for users running Paperless on x86 hosts
- `linux/arm64` — for users running on Pi 4/5, Synology with ARM
  CPUs, AWS Graviton

We do not build for:

- `linux/arm/v7` — old Pi 3 era. Cross-compilation works but the
  resulting performance is poor enough that we recommend Pi 4+ instead
- `linux/386` — no realistic use case
- `linux/ppc64le`, `linux/s390x` — no demand
- Windows or macOS — these run upstream Paperless, not our containers

### 12.2 Cross-compilation strategy

For the pure-Go containers (scan-bridge), CGO_ENABLED=0 means
cross-compilation is trivial. The `$TARGETOS` and `$TARGETARCH`
build-args drive `go build`.

For scan-processor (which uses CGO via Leptonica), cross-compilation
requires a cross-toolchain inside the build stage. The Dockerfile
installs `gcc-aarch64-linux-gnu` when building arm64 from amd64.

For sane-runtime, the Debian package layer compiles natively in QEMU
emulation. This is slower (3-5x) but does not require cross-package
juggling.

### 12.3 Manifest list

The bake produces a manifest list that selects the correct
architecture at pull time. A user on a Pi 5 pulling
`ghcr.io/.../scan-bridge:v1.0.0` automatically gets the arm64 variant;
a user on x86 gets amd64.

---

## 13. Image signing and provenance

### 13.1 Cosign keyless signing

Every released image is signed with cosign in keyless mode using the
GitHub OIDC identity. Verification:

```bash
cosign verify ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:v1.0.0 \
    --certificate-identity-regexp '^https://github\.com/strausmann/paperless-scan-bridge/.*$' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

This proves the image was built by our GitHub Actions workflow on a
specific commit, with no secret material managed manually.

### 13.2 SBOM generation

Each image gets an SPDX-formatted SBOM published as an OCI artifact
attached to the image. Tools like `syft` and `grype` can then scan
the SBOM for vulnerabilities without re-pulling the image.

The SBOM is also attached to the GitHub release as a downloadable
asset.

### 13.3 SLSA provenance

We publish SLSA Level 3 provenance attestations. The attestation
includes:

- The exact commit that was built
- The build environment (GitHub Actions runner image, version)
- The build steps executed
- The resulting digest

Verification with `cosign verify-attestation`. This proves the image
was built from the claimed source code.

---

## 14. Local development workflow

### 14.1 Prerequisites

- Docker Desktop or Docker Engine with Buildx
- Go 1.22+
- Tilt
- Pre-commit
- (optional) golangci-lint

```bash
# macOS
brew install tilt-dev/tap/tilt go pre-commit

# Linux (Ubuntu)
curl -fsSL https://raw.githubusercontent.com/tilt-dev/tilt/master/scripts/install.sh | bash
sudo apt install golang pre-commit
```

### 14.2 Tiltfile

`Tiltfile` at the repository root drives local development:

```python
docker_compose("./deploy/compose/docker-compose.dev.yml")

dc_resource("scan-bridge", labels=["custom"])
dc_resource("sane-runtime", labels=["custom"])
dc_resource("scan-processor", labels=["custom"])
dc_resource("paperless-ngx", labels=["upstream"])

# Auto-rebuild scan-bridge on Go file changes
docker_build(
    "ghcr.io/strausmann/paperless-scan-bridge/scan-bridge",
    "./components/scan-bridge",
    dockerfile="./components/scan-bridge/Dockerfile",
    build_args={"VERSION": "dev"},
    only=[
        "go.mod",
        "go.sum",
        "cmd/",
        "internal/",
    ],
    live_update=[
        sync("./components/scan-bridge/cmd/", "/src/cmd/"),
        sync("./components/scan-bridge/internal/", "/src/internal/"),
        run(
            "cd /src && go build -o /usr/local/bin/scan-bridge ./cmd/scan-bridge",
            trigger=["./cmd/", "./internal/"],
        ),
        restart_container(),
    ],
)

# Same pattern for sane-runtime and scan-processor
docker_build(
    "ghcr.io/strausmann/paperless-scan-bridge/sane-runtime",
    "./components/sane-runtime",
    dockerfile="./components/sane-runtime/Dockerfile",
    build_args={"VERSION": "dev"},
)

docker_build(
    "ghcr.io/strausmann/paperless-scan-bridge/scan-processor",
    "./components/scan-processor",
    dockerfile="./components/scan-processor/Dockerfile",
    build_args={"VERSION": "dev"},
)
```

`tilt up` brings the whole stack up. The Tilt dashboard at
`http://localhost:10350` shows logs and rebuild status. Any save in
a Go file under `scan-bridge` triggers a sub-second rebuild and
container restart.

### 14.3 Mock scanner for development

Real scanners are not always at hand. We ship a mock scanner
container that emulates a SANE backend:

```yaml
# docker-compose.dev.yml
services:
  mock-scanner:
    image: ghcr.io/strausmann/paperless-scan-bridge/mock-scanner:latest
    volumes:
      - sane-socket:/var/run/scan-runtime

  sane-runtime:
    environment:
      SANE_DEVICE_OVERRIDE: "mock:dev"
    depends_on:
      - mock-scanner
```

The mock scanner serves canned PDFs from a fixtures directory in
response to scan requests. It supports all the profiles, returning
appropriate test images. This makes integration tests deterministic
and fast.

---

## 15. Testing strategy per container

### 15.1 scan-bridge

- **Unit tests** for every package under `internal/`
- **Coverage target**: 80% for new code
- **HTTP handler tests** using `httptest.NewServer` plus a mocked
  dispatch client
- **State machine tests** for the job lifecycle, exhaustively covering
  every transition
- **Integration tests** under `tests/integration/` that bring up the
  full stack with the mock scanner

Run locally:

```bash
cd components/scan-bridge
go test ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 15.2 sane-runtime

The Go HTTP wrapper has unit tests like scan-bridge.

The Bash hook script (`scanbd-hook.sh`) is tested with `bats`:

```bash
# tests/bats/scanbd-hook.bats
@test "maps profile counter 1 to private-simplex" {
    run env SCANBD_FUNCTION=1 ./scanbd-hook.sh --dry-run
    [ "$status" -eq 0 ]
    [[ "$output" =~ "private-simplex" ]]
}

@test "falls back to default profile for unknown counter" {
    run env SCANBD_FUNCTION=99 ./scanbd-hook.sh --dry-run
    [ "$status" -eq 0 ]
    [[ "$output" =~ "private-simplex" ]]
}

@test "dispatches to bridge URL with bearer token" {
    SCAN_BRIDGE_TOKEN=test-token \
    SCANBD_FUNCTION=1 \
    ./scanbd-hook.sh --dry-run --print-curl
    [[ "$output" =~ "Authorization: Bearer test-token" ]]
}
```

The full integration test brings up sane-runtime with a real (or
mocked) USB scanner attached and verifies end-to-end scan execution.

### 15.3 scan-processor

The processor has the most fixture-driven tests:

- **Deskew tests** with pre-skewed sample images of known angles
- **Blank page detection tests** with curated blank/non-blank pages
- **PDF assembly tests** verifying output structure with a PDF parser
- **Atomic write tests** that simulate concurrent reads during writes

The fixture directory `tests/fixtures/` is committed to the
repository:

```
tests/fixtures/
├── sample-batch/
│   ├── page-001.tiff
│   ├── page-002.tiff
│   └── page-003.tiff
├── blank-pages/
│   ├── truly-blank.tiff
│   ├── almost-blank-with-punch-holes.tiff
│   └── sparse-receipt.tiff
└── skewed-pages/
    ├── 5-degrees.tiff
    ├── 15-degrees.tiff
    └── 45-degrees.tiff
```

Fixtures are kept small; total fixture data is under 10 MB.

---

## 16. Configuration and secrets

### 16.1 Configuration files

Each container has a TOML or YAML config file. None of them contain
secrets. They are mounted read-only.

`bridge/config.toml`:

```toml
[server]
listen = ":8080"
metrics_listen = ":9090"

[auth]
mode = "token"  # token | ip_allowlist

[storage]
db_path = "/var/lib/scan-bridge/jobs.db"

[dispatch]
sane_socket = "/var/run/scan-runtime/api.sock"
processor_url = "http://scan-processor:8082"
timeout_seconds = 300

[profiles]
file = "/etc/scan-bridge/profiles.yaml"

[synthetic]
enabled = true
interval_seconds = 3600
test_profile = "synthetic-test"
```

### 16.2 Secrets

Two secrets matter:

- `SCAN_BRIDGE_API_TOKEN` — the bearer token for the API
- `RESTIC_PASSWORD` — the encryption password for backups

Both are managed via SOPS with age keys. The encrypted files live in
the repository:

```
secrets/
├── api-token.age
└── restic-repo.age
```

The compose stack receives them via environment variables, decrypted
at deploy time:

```yaml
services:
  scan-bridge:
    environment:
      SCAN_BRIDGE_API_TOKEN_FILE: /run/secrets/api-token
    secrets:
      - api-token

secrets:
  api-token:
    file: /etc/paperless-scan-bridge/secrets/api-token.decrypted
```

The decryption happens in a pre-deploy step driven by Make:

```makefile
secrets-decrypt:
	sops -d secrets/api-token.age > /etc/paperless-scan-bridge/secrets/api-token.decrypted
	chmod 600 /etc/paperless-scan-bridge/secrets/api-token.decrypted

deploy: secrets-decrypt
	docker compose -f deploy/compose/docker-compose.yml up -d
```

### 16.3 Environment variables

Documented in each component's README. Pattern:

| Variable                       | Default          | Purpose                  |
| ------------------------------ | ---------------- | ------------------------ |
| `SCAN_BRIDGE_LISTEN`           | `:8080`          | HTTP listen address      |
| `SCAN_BRIDGE_METRICS_LISTEN`   | `:9090`          | Metrics listen address   |
| `SCAN_BRIDGE_LOG_LEVEL`        | `info`           | Log level                |
| `SCAN_BRIDGE_API_TOKEN_FILE`   | (unset)          | Path to token file       |
| `SCAN_BRIDGE_DB_PATH`          | `/var/lib/...`   | Job database path        |

Secrets always come from `_FILE` variants pointing to mounted files.
We never accept secrets directly in environment variables, because
those are visible in `docker inspect` to anyone with Docker socket
access.

---

## 17. Logging and observability

### 17.1 Log format

All three containers log structured JSON to stdout. The Docker
logging driver captures it.

```json
{
  "time": "2026-04-30T14:23:01.234Z",
  "level": "info",
  "msg": "scan complete",
  "container": "scan-bridge",
  "version": "v1.2.3",
  "trace_id": "01HJ9P5K...",
  "job_id": "01HJ9P5K2N6QXZ",
  "profile": "private-duplex",
  "duration_ms": 28145,
  "page_count": 6
}
```

### 17.2 Log aggregation

We do not run a log aggregator inside the stack. Users plug into
their existing setup:

- Loki + Grafana — `loki-docker-driver` plugin sends container logs
  directly
- syslog — Docker's syslog driver
- elastic — `gelf` driver to a logstash forwarder

The compose file provides examples for each.

### 17.3 Trace correlation

The `trace_id` field carries through the entire job's lifetime. To
query all logs for a single scan job, filter on its trace_id across
all three containers.

We do not yet emit OpenTelemetry traces. That is a Phase 4
consideration; logs with trace_id give us 90% of the value at 10%
of the complexity.

### 17.4 Metrics scraping

Prometheus configuration to scrape all three containers:

```yaml
scrape_configs:
  - job_name: 'paperless-scan-bridge'
    scrape_interval: 30s
    static_configs:
      - targets:
          - 'scan-bridge:9090'
          - 'sane-runtime:9091'
          - 'scan-processor:9092'
        labels:
          stack: 'paperless-scan-bridge'
```

We ship a Grafana dashboard at `monitoring/grafana-dashboards/scan-bridge.json`
that visualizes the metrics. Panels include:

- Scan rate (jobs per minute) by profile
- Error rate by component
- Job latency p50, p95, p99
- Queue depth
- Pi temperature (from node_exporter)
- USB throughput (from node_exporter)

---

## 18. Resource limits and tuning

### 18.1 Default compose limits

```yaml
services:
  scan-bridge:
    deploy:
      resources:
        limits:
          memory: 128M
          cpus: '0.5'
        reservations:
          memory: 32M
          cpus: '0.1'

  sane-runtime:
    deploy:
      resources:
        limits:
          memory: 256M
          cpus: '1.0'
        reservations:
          memory: 64M
          cpus: '0.2'

  scan-processor:
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '2.0'
        reservations:
          memory: 128M
          cpus: '0.5'
```

These are conservative limits for a Pi 5 with 8 GB RAM. They prevent
runaway processes from starving the system.

### 18.2 Tuning for high-volume setups

If you scan more than 200 pages per day, consider:

- Bump `scan-processor` memory limit to 1 GB
- Run two `scan-processor` replicas behind a queue (Phase 4 feature)
- Move `scan-processor` to the bigger Docker host (it does not need
  USB access)

### 18.3 Tuning for low-resource setups

If you run on a Pi 4 with 2 GB RAM:

- Lower `scan-processor` memory limit to 256 MB
- Disable Tesseract orientation detection in profiles (memory-heavy)
- Use lower resolution profiles (200 DPI instead of 300)

---

## 19. Update and rollback strategy

### 19.1 Update flow

For Renovate-managed updates:

1. Renovate opens a PR bumping a container image version
2. CI runs the integration tests against the new version
3. If green, a maintainer reviews and merges
4. The merge triggers a deploy workflow that updates production

For users running their own deployments:

1. `git pull` the new compose file (if they track the repo) or
   manually edit their compose file
2. `docker compose pull && docker compose up -d`
3. Watchtower can automate this for the bridge components but is
   configured NOT to auto-update Paperless

### 19.2 Rollback flow

Container images on GHCR are immutable once pushed; old versions
remain available. To roll back:

1. Edit the compose file to reference the previous version tag
2. `docker compose pull && docker compose up -d`
3. Verify scans work; check metrics dashboard for anomalies

The job database is forward-compatible across minor versions but
not always across major versions. Major version rollbacks may
require restoring the database from backup.

### 19.3 Database migrations

The bridge uses BoltDB which has no migration framework. Schema
changes are handled in code:

- Each schema version has a magic byte at the start of a known key
- On startup, the bridge reads the magic byte and runs migrations
  if needed
- Migrations are forward-only; rolling back a major version requires
  restoring from a pre-migration backup

The CHANGELOG calls out database schema changes explicitly.

---

## 20. Release process

### 20.1 Cut a release

A maintainer cuts a release by:

1. Update `CHANGELOG.md` with the release notes
2. Open a "Release vX.Y.Z" PR
3. Run the tests, get reviews
4. Merge
5. Tag the merge commit: `git tag vX.Y.Z`
6. Push the tag: `git push origin vX.Y.Z`
7. CI builds the images, signs them, generates SBOM, creates the
   GitHub release with assets attached

### 20.2 Release notes format

Following Keep a Changelog 1.1:

```markdown
## [v1.2.3] - 2026-04-30

### Added
- New `business-archive` default profile for long-term business storage.

### Changed
- Default tesseract OSD threshold from 0.5 to 0.7 to reduce false
  rotations on form documents.

### Fixed
- Race condition in atomic write under heavy concurrent load (#142).
- Hangup on USB disconnect during a scan; now properly aborts the
  job (#138).

### Security
- Updated base image to debian:12.5-slim with latest CVE patches.

### Compatibility
- This release is compatible with sane-runtime >= v1.4.0 and
  scan-processor >= v1.3.0.
```

### 20.3 Post-release

After a release:

1. Verify the new images are pullable from GHCR
2. Verify the signatures with cosign
3. Update the documentation site to point to the new version
4. Announce on relevant channels (GitHub Discussions, Mastodon)

---

## 21. Security hardening per container

Summary; full threat model in `THREAT_MODEL.md`.

### 21.1 scan-bridge

- Distroless static base (no shell)
- Non-root user (UID 10001)
- Read-only root filesystem
- `cap_drop: [ALL]`
- `no-new-privileges: true`
- Network exposure: 8080 (API) and 9090 (metrics) only
- Authentication required for all non-health endpoints
- Rate limiting: 100 requests per minute per source IP

### 21.2 sane-runtime

- Debian slim base (we need the package manager during build, not
  runtime)
- Non-root user (UID 10002, member of scanner GID 10003)
- `cap_drop: [ALL]` then `cap_add: [SYS_RAWIO]` for USB
- `no-new-privileges: true`
- Network exposure: only Unix socket and Prometheus metrics
- USB device explicitly enumerated, not full `/dev/bus/usb`

### 21.3 scan-processor

- Distroless base with shared libraries for Leptonica
- Non-root user (UID 10004)
- Read-only root filesystem
- `cap_drop: [ALL]`
- `no-new-privileges: true`
- Network exposure: 8082 (internal callback only) and 9092 (metrics)
- Bind mount to consume directory is the only writable filesystem

---

## 22. Troubleshooting matrix

A quick-reference table for common failure modes.

| Symptom                                | Likely cause                                         | Diagnostic command                                   |
| -------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------- |
| `/health` returns 503                  | sane-runtime not reachable                           | `docker exec scan-bridge curl http://localhost:8080/ready` |
| Scanner not detected                   | udev rule missing or USB device path wrong           | `docker exec sane-runtime scanimage -L`              |
| Scanner detected but scans fail        | Wrong group GID mismatch                             | `docker exec sane-runtime id`                        |
| Jobs queued but never dispatched       | Unix socket permissions                              | `ls -la /var/run/scan-runtime/`                      |
| PDFs in consume but not consumed       | Inotify on NFS or polling not enabled                | Check Paperless `PAPERLESS_CONSUMER_POLLING`         |
| High memory usage on processor        | Tesseract OSD on large color images                  | `docker stats scan-processor`                        |
| Slow scans                             | Pi USB 2.0 instead of 3.0, or scanner mechanics      | `lsusb -t` to verify USB speed                       |
| Container restarts every 30 seconds    | HEALTHCHECK failing                                  | `docker inspect <container> | jq '.[0].State.Health'` |

The full troubleshooting guide lives at
`docs/operations/troubleshooting.md` with each symptom expanded into
diagnostic procedures and remediation steps.

---

## 23. Future considerations

Things we may add in Phase 4 or later. Each is a deliberate
non-goal for the current scope.

### 23.1 OpenTelemetry tracing

Span-based traces across the three containers, with the bridge as
the root span. Useful for diagnosing latency in production setups.
Adds operational complexity (collector, storage, UI). Defer until
demand is clear.

### 23.2 Multi-scanner support

One Pi, multiple scanners simultaneously. The architecture admits
this — the device list in sane-runtime is already a list — but
the profile system needs to grow a "device selector" field.
Estimated work: 2-3 weeks.

### 23.3 Direct Paperless API integration

Currently we drop files into the consume directory and let
Paperless ingest them. We could instead call the Paperless REST
API directly, which would allow us to set tags, correspondents, and
custom fields immediately. The trade-off is a tighter coupling to a
specific Paperless version.

### 23.4 Helm chart for Kubernetes

Compose is the reference deployment. A Helm chart would expand
reach to Kubernetes-based homelabs. Maintenance burden is real —
we would need to test on at least k3s and k8s, and Kubernetes Pod
Security Standards add complexity for the USB device handling.

### 23.5 Scan-processor as a queue worker

Currently scan-processor responds to a callback URL. A queue-based
model (Redis Streams, NATS JetStream) would allow horizontal
scaling and better backpressure. Worth considering when single-Pi
throughput becomes a bottleneck.

### 23.6 Bare-metal Go installation

For users who insist on no Docker, a bare-metal install path that
runs the daemons directly. Significant maintenance burden;
considered out of scope unless someone steps up to maintain it.

---

*This document is alive. As the implementation evolves, this
document evolves with it. Significant changes get added; obsolete
sections get removed (with a note in CHANGELOG); decisions get
recorded in CONCEPT.md's decision log.*

*If a contribution conflicts with what is documented here, the
correct response is one of: update the document first (and discuss
why the change is right), or revise the contribution to match.
Documentation drift is the failure mode we work hardest to avoid.*
