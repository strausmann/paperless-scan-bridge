# sane-runtime

The scanner-facing daemon of the `paperless-scan-bridge` suite. Owns
the physical SANE-compatible scanner and exposes a small HTTP API —
`POST /scan`, `GET /health`, `GET /ready` — over a Unix-domain socket
so `scan-bridge` never needs direct hardware access (see ADR 0008 —
`sane-runtime` owns the scanner via device passthrough + udev, never
`--privileged`).

This is one of three custom container images. See
[`CONTAINER_SUITE.md`](../../CONTAINER_SUITE.md) sec. 4/9,
[`ARCHITECTURE.md`](../../ARCHITECTURE.md) for how the component fits
into the data flow, and the repo-root [`AGENTS.md`](../../AGENTS.md)
for the project-wide guardrails this code is held to.

## Status

Phase 1.2. `POST /scan`, `GET /health`, and `GET /ready` are real. The
scanning backend (`internal/scanner.ExecScanner`) shells out to
`scanimage(1)`; every test in this module runs against a fixture
shell script or an in-memory fake, so `go test ./...` needs no scanner
hardware and no `scanimage` binary on the host running the tests.

Layout diverges from `CONTAINER_SUITE.md` sec. 5.2's flat sketch —
this module follows the idiomatic Go layout `scan-bridge` already
uses (`cmd/<binary>/main.go` + `internal/<package>/`) instead. The
sec. 5.2 sketch should be reconciled with this layout as a doc
follow-up (Task 19).

## Layout

```
components/sane-runtime/
├── go.mod / go.sum
├── cmd/sane-runtime/main.go        # flags/env, unix listener, signal handling, graceful shutdown
└── internal/
    ├── scanapi/                    # HTTP handlers, routes, multipart response encoding
    └── scanner/                    # Scanner interface, argv building, ExecScanner, device parsing
```

## Build

```bash
cd components/sane-runtime
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

This module is part of the repo-root `go.work` workspace
(`go work use ./components/sane-runtime`), so `go build`/`go test`
from the repo root also pick it up. The `go.work` file itself is a
small, deliberate addition beyond the sketch in `CONTAINER_SUITE.md`
sec. 5.2 — `AGENTS.md` already calls for `go.work` tying the
components together, this just makes it exist on disk.

## Run

Local smoke run without Docker or hardware — point `--socket` at a
throwaway path and hit it with `curl --unix-socket`:

```bash
go run ./cmd/sane-runtime --socket /tmp/sane.sock
```

In another terminal:

```bash
curl --unix-socket /tmp/sane.sock http://localhost/health
curl --unix-socket /tmp/sane.sock http://localhost/ready
curl --unix-socket /tmp/sane.sock -X POST http://localhost/scan \
  -H 'Content-Type: application/json' \
  -d '{"request_id":"demo","source":"ADF Duplex","resolution":300,"mode":"Color","format":"tiff"}' \
  -o /tmp/scan-response.multipart
```

`/ready` and `POST /scan` both need a real `scanimage` on `PATH` (or a
device to auto-detect) to succeed without hardware present — expect
`503 no_scanner_detected` in a dev container without a scanner
attached, which is the correct, tested behaviour.

## Configuration

| Variable              | Purpose                                                              |
| ---------------------- | --------------------------------------------------------------------- |
| `SANE_RUNTIME_SOCKET`  | Path to the Unix-domain socket to serve on. Default `/run/sane-runtime/sane.sock`, matching `scan-bridge`'s `config.Paths.SaneSocket` (ADR 0009). Overridable per-run with `--socket`. |

`--socket` (flag) takes precedence over `SANE_RUNTIME_SOCKET` (env)
takes precedence over the compiled-in default, mirroring the
precedence order documented for `scan-bridge`.

## API surface

| Endpoint  | Method | Contract |
| --------- | ------ | -------- |
| `/health` | GET    | Always `200` — process liveness only. |
| `/ready`  | GET    | `200` if `scanimage -L` reports at least one device, `503` otherwise (including a failed `scanimage -L` call). |
| `/scan`   | POST   | JSON request in, `multipart/mixed` response out. See below. |

### `POST /scan`

Request body (`application/json`, unknown fields rejected):

```json
{
  "request_id": "...",
  "device": "",
  "source": "ADF Duplex",
  "resolution": 300,
  "mode": "Color",
  "format": "tiff",
  "max_pages": 0,
  "timeout_seconds": 300
}
```

`format` is the **capture** format (`tiff` or `pnm`) — always native
and lossless. It is not the profile's final document format (e.g.
`pdf`); that conversion is `scan-processor`'s job downstream, not
sane-runtime's.

Success (`200`) is `multipart/mixed; boundary=...`:

- Part 0 — `application/json`, the scan metadata
  (`request_id`, `page_count`, `duration_ms`, `device`, `source`,
  `resolution`, `mode`).
- Parts 1..N — one per scanned page, in scan order, `Content-Type:
  image/tiff` (or `image/x-portable-anymap` for `pnm`) and
  `Content-Disposition: form-data; name="page"; filename="page-N.tiff"`.

Errors use the same `{"error": "...", "hint": "..."}` envelope as
`scan-bridge`'s `internal/api`:

| Condition                                  | Status | `error`               |
| ------------------------------------------- | ------ | ---------------------- |
| Bad request body (validation or unknown field) | 400 | `invalid_request`      |
| No scanner attached / not found            | 503    | `no_scanner_detected`  |
| A scan is already in progress              | 409    | `scanner_busy`         |
| ADF reports no documents                   | 422    | `no_documents`         |
| Device error (cover open, jam, I/O)        | 422    | `device_error`         |
| Scan exceeded its timeout                  | 504    | `scan_timeout`         |
| Any other non-zero `scanimage` exit        | 500    | `scan_failed`          |

`/scan` is single-flight: `internal/scanapi.Server` guards the scan
path with `sync.Mutex.TryLock()`, so a second concurrent `POST /scan`
gets `409 scanner_busy` immediately rather than queuing. Job queuing
across multiple in-flight requests is Task 8's concern and is
deliberately out of scope here.

`/metrics` is out of scope for this task (Task 12).

## Testing

```bash
go test ./... -v          # unit tests, no hardware or scanimage binary required
go test ./... -race
go vet ./...
gofmt -l .
```

- `internal/scanner/argv_test.go` — table-driven tests of the pure
  `buildArgv` function (no exec).
- `internal/scanner/devices_test.go` — table-driven tests of the pure
  `scanimage -L` output parser.
- `internal/scanner/exec_scanner_test.go` — exercises the real
  `exec.CommandContext` + stderr-classification path against
  `testdata/fixture-scanimage.sh`, a shell script standing in for
  `scanimage(1)` (device auto-select via `-L`, happy path, device
  error, ADF-empty, and a real context-deadline timeout).
- `internal/scanapi/handlers_test.go` — exercises the HTTP layer
  against an in-memory fake `scanner.Scanner`, including the
  concurrency test that gates a `Scan` call on a channel to prove the
  second concurrent request gets `409` before the first completes.
- `cmd/sane-runtime/main_test.go` — flag handling, the Unix-listener
  helper (stale-socket removal, permissions), and one end-to-end test
  that serves `GET /health` over a real socket and confirms a real
  `SIGTERM` triggers the same graceful-shutdown path a container
  runtime uses.

## See also

- [`CONTAINER_SUITE.md`](../../CONTAINER_SUITE.md) sec. 4/9 — daemon
  and hardware-access specification
- [`ARCHITECTURE.md`](../../ARCHITECTURE.md) — system data flow
- `docs/decisions/0008-sane-runtime-owns-scanner.md` — device-access
  privilege decision
- `docs/decisions/0009-bridge-sane-unix-socket.md` — transport
  decision (Unix socket, not TCP)
