# scan-bridge

The core daemon of the `paperless-scan-bridge` suite. Exposes a REST
API for triggering scans, listing profiles, and observing job status,
plus a Prometheus metrics endpoint on a separate listener.

This is one of three custom container images. See
[`CONTAINER_SUITE.md`](../../CONTAINER_SUITE.md) sec. 4 for the full
specification, [`ARCHITECTURE.md`](../../ARCHITECTURE.md) for how the
component fits into the data flow, and the repo-root
[`AGENTS.md`](../../AGENTS.md) for the project-wide guardrails this
code is held to.

## Status

Phase 1.1. The HTTP surface is wired and the /health, /version,
/profiles, and /profiles/{name} endpoints are real; /scan, /jobs,
/jobs/{id}, /jobs/{id}/cancel, and /ready return a uniform `501
not_implemented` envelope until the dispatch and jobs subsystems
land.

Stubbed subsystems carry their TODOs in source:

- `internal/jobs` — state machine and BoltDB persistence (Phase 1.4)
- `internal/dispatch` — HTTP-over-Unix client to `sane-runtime`
  (Phase 1.4)
- `internal/healthcheck` — synthetic-scan worker (Phase 1.4)
- `internal/metrics` — job/dispatch/scan/processing histograms
  (Phase 1.4)

## Layout

```
components/scan-bridge/
├── Dockerfile                  # multi-stage, distroless static :nonroot
├── go.mod / go.sum
├── cmd/scan-bridge/main.go     # entry point, flag parsing, signal handling
└── internal/
    ├── api/                    # HTTP handlers, middleware, routes
    ├── config/                 # TOML + env + flag config loader
    ├── dispatch/               # sane-runtime client (stub)
    ├── healthcheck/            # liveness/readiness (stub)
    ├── jobs/                   # job state machine (stub)
    ├── metrics/                # Prometheus collectors
    └── profiles/               # YAML profile loader + defaults.yaml
```

## Build

Local toolchain:

```bash
cd components/scan-bridge
go build ./...
go test ./...
go vet ./...
```

The binary is fully static (`CGO_ENABLED=0`) and cross-compiles to
arm64 without extra setup:

```bash
GOOS=linux GOARCH=arm64 go build -o scan-bridge.arm64 ./cmd/scan-bridge
```

Container image (multi-arch via `docker buildx bake` once the bake
file lands; for now the equivalent direct command is):

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=$(git describe --tags --always --dirty) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:dev \
  components/scan-bridge
```

## Run

The container expects:

- `/etc/scan-bridge/config.toml` — main configuration. Mount your own
  or rely on the defaults baked in via env overrides.
- `/etc/scan-bridge/profiles.yaml` — profile definitions. The image
  ships `internal/profiles/defaults.yaml` here. Override by mounting
  your own.
- `/var/lib/scan-bridge` — declared `VOLUME` for the future BoltDB
  job store. Empty in Phase 1.1.
- `SCAN_BRIDGE_API_TOKEN_HASH` — SHA-256 hex digest of the bearer
  token, when running in token-auth mode (the default).

Local smoke run without Docker:

```bash
go run ./cmd/scan-bridge --version
go run ./cmd/scan-bridge --config /tmp/scan-bridge.toml
```

In another terminal:

```bash
curl -s http://localhost:8080/health
curl -s http://localhost:8080/version
curl -s http://localhost:8080/profiles
curl -s http://localhost:9090/metrics | head
```

## Configuration

Loaded in this order (lowest precedence first):

1. Compiled-in defaults (`config.Default()`).
2. TOML file at `--config` (default `/etc/scan-bridge/config.toml`).
3. Environment variables prefixed `SCAN_BRIDGE_`.
4. Flag overrides applied by the caller after `Load`.

Recognised env variables:

| Variable                       | Purpose                              |
| ------------------------------ | ------------------------------------ |
| `SCAN_BRIDGE_LISTEN`           | Public REST API listener address     |
| `SCAN_BRIDGE_METRICS_LISTEN`   | Prometheus listener address          |
| `SCAN_BRIDGE_AUTH_MODE`        | `token` or `ip_allowlist`            |
| `SCAN_BRIDGE_API_TOKEN_HASH`   | SHA-256 hex of bearer token          |
| `SCAN_BRIDGE_PROFILES_PATH`    | Path to profiles YAML                |
| `SCAN_BRIDGE_STATE_DIR`        | Directory for the future job store   |
| `SCAN_BRIDGE_SANE_SOCKET`      | Path to the sane-runtime Unix socket |
| `SCAN_BRIDGE_LOG_LEVEL`        | `debug`, `info`, `warn`, `error`     |
| `SCAN_BRIDGE_LOG_FORMAT`       | `json` or `text`                     |

## API surface

Mirror of `CONTAINER_SUITE.md` sec. 4.4. Implemented vs. stubbed:

| Endpoint            | Method | Status              |
| ------------------- | ------ | ------------------- |
| `/health`           | GET    | implemented         |
| `/version`          | GET    | implemented         |
| `/profiles`         | GET    | implemented         |
| `/profiles/{name}`  | GET    | implemented         |
| `/metrics`          | GET    | implemented (separate listener) |
| `/ready`            | GET    | 501 — needs dispatch |
| `/scan`             | POST   | 501 — needs dispatch |
| `/jobs`             | GET    | 501 — needs jobs store |
| `/jobs/{id}`        | GET    | 501 — needs jobs store |
| `/jobs/{id}/cancel` | POST   | 501 — needs jobs store |

## Testing

```bash
go test ./...        # unit tests
go vet ./...         # static analysis
```

Integration tests that bring up the full compose stack land under
`tests/integration/` once `sane-runtime` is implemented.

## See also

- [`CONTAINER_SUITE.md`](../../CONTAINER_SUITE.md) sec. 4 — full
  daemon specification
- [`ARCHITECTURE.md`](../../ARCHITECTURE.md) — system data flow
- [`THREAT_MODEL.md`](../../THREAT_MODEL.md) — security posture
