# scan-processor

The OCR/image-processing daemon of the `paperless-scan-bridge` suite.
Takes a job's raw TIFF pages from `scan-bridge`, runs them through
deskew, blank-page removal, rotation correction, OCR (Tesseract,
`deu+eng` default), format conversion, and multi-page assembly, and
hands back the assembled document(s) — see
[`docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md`](../../docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)
sec. 4 for the full design, and the repo-root
[`AGENTS.md`](../../AGENTS.md) for the project-wide guardrails this
code is held to.

This is one of three custom container images (ADR
[0003](../../docs/decisions/0003-three-custom-images.md)).

## Status

Task 6 of the design doc's task breakdown. `POST /process` and
`GET /health` are real; the wire contract they implement is frozen by
`components/scan-bridge/internal/procclient` (Task 5, merged first) —
see that package's doc comment for the authoritative shape.

The processing backend (`internal/pipeline.ExecPipeline`) shells out
to `convert(1)` (ImageMagick), `tesseract(1)`, and `qpdf(1)`. **None
of those binaries are installed on this repo's CI runners**, so:

- `go test ./...` (what CI runs) exercises the full HTTP contract —
  request decoding, response encoding, error-status mapping,
  `page_grouping` orchestration — against a hand-rolled fake
  `pipeline.Pipeline`, and unit-tests the exec-argument-building
  helpers as pure functions. No binary is invoked.
- `go test -tags integration ./internal/pipeline/...` drives the real
  toolchain (skips per-test if a binary is missing) and is meant to be
  run explicitly, e.g. inside the built container image or on a
  development host with ImageMagick/Tesseract/qpdf installed.

## Layout

```
components/scan-processor/
├── go.mod
├── Dockerfile
├── cmd/scan-processor/main.go       # flags/env, unix listener, signal handling, graceful shutdown
└── internal/
    ├── procapi/                     # HTTP handlers, routes, multipart request/response (de)coding
    └── pipeline/                    # Pipeline interface, ExecPipeline, exec-argument builders
```

`internal/pipeline` does not import `internal/procclient` or anything
under `components/scan-bridge` — the dependency direction is
one-way (`procclient`/`scan-bridge` depend on the frozen wire
contract; `scan-processor` implements it independently). See the doc
comments on `internal/pipeline/pipeline.go` and
`internal/procapi/api.go` for why.

## Build

```bash
cd components/scan-processor
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

This module is part of the repo-root `go.work` workspace
(`go work use ./components/scan-processor`), so `go build`/`go test`
run from within any workspace module also see it.

## Run

Local smoke run without Docker or the OCR toolchain (against a fake
`pipeline.Pipeline` is exercised by the tests; this section is for a
real `ExecPipeline` run, which needs `convert`/`tesseract`/`qpdf` on
`PATH`):

```bash
go run ./cmd/scan-processor --socket /tmp/scan-processor.sock
```

In another terminal:

```bash
curl --unix-socket /tmp/scan-processor.sock http://localhost/health
```

`POST /process` needs a `multipart/mixed` body (JSON control payload
part 0, TIFF page parts 1..N) — see "API surface" below for the exact
shape, or drive it via `scan-bridge`'s `internal/procclient` client
package once Task 7 wires the two together.

## Configuration

| Variable                     | Purpose                                                                                        |
| ----------------------------- | ------------------------------------------------------------------------------------------------ |
| `SCAN_PROCESSOR_SOCKET`       | Path to the Unix-domain socket to serve on. Default `/run/scan-processor/scan-processor.sock` (design doc sec. 4.2). Overridable per-run with `--socket`. |
| `SCAN_PROCESSOR_CONVERT_BIN`  | Override the `convert(1)` binary path. Empty resolves via `PATH`. Overridable per-run with `--convert-bin`. |
| `SCAN_PROCESSOR_TESSERACT_BIN` | Override the `tesseract(1)` binary path. Empty resolves via `PATH`. Overridable per-run with `--tesseract-bin`. |
| `SCAN_PROCESSOR_QPDF_BIN`     | Override the `qpdf(1)` binary path. Empty resolves via `PATH`. Overridable per-run with `--qpdf-bin`. |

Flag takes precedence over the matching environment variable, which
takes precedence over the compiled-in default — same precedence order
`sane-runtime` and `scan-bridge` document.

### OCR languages

The runtime image installs these Tesseract language packs (this
Dockerfile's `apt-get install` list, one `tesseract-ocr-<code>`
package per code):

| Code  | Language   |
| ----- | ---------- |
| `deu` | German     |
| `eng` | English    |
| `fra` | French     |
| `ita` | Italian    |
| `nld` | Dutch      |
| `por` | Portuguese |
| `spa` | Spanish    |

`POST /process`'s `ocr.languages` (and, one layer up, a scan
profile's own `ocr.languages`, see
[`docs/en/getting-started/profile-schema.md`](../../docs/en/getting-started/profile-schema.md#ocr))
is validated against exactly this set —
`internal/procapi/api.go`'s `allowedOCRLanguageCodes` — before the
pipeline ever runs; a code outside it is rejected with `400
invalid_request` rather than reaching `tesseract(1)` at all. Adding a
language means adding it in **both** places (that Go variable and this
Dockerfile) — see either's doc comment.

The user picks which of these apply **per scan profile**, not
globally: a German-only mailbox names `[deu]`, a mixed one
`[deu, fra]`, and so on (omitted entirely, `ocr.enabled: true` falls
back to `[deu, eng]`, matching this project's long-standing HomeLab
default). Naming every installed language on every profile is
possible but not recommended — Tesseract's own guidance is that
passing more language packs than a page actually contains slows
recognition down and, because languages using similar scripts (e.g.
`deu`/`nld`, `spa`/`ita`) share a lot of dictionary overlap,
measurably increases misrecognitions from words being "corrected"
into the wrong language's spelling. Pick the language(s) that
profile's documents actually use.

### OCR confidence gate

Every OCR pass also produces a Tesseract `tsv` configfile output
alongside the searchable PDF (one tesseract invocation, two output
files — `internal/pipeline/exec_argv.go`'s `buildOCRArgs`), which
carries a 0..100 confidence score per recognized word
(`internal/pipeline/exec_argv.go`'s `parseOCRTSV`). The mean of those
per-word scores (averaged across every surviving page of a document
for `page_grouping: combined`) becomes that document's
`ocr_confidence`; when it falls below the effective threshold, the
document is flagged `low_confidence: true` with a matching entry in
`warnings` — see "API surface" below for the exact response fields.

**The gate never fails a request.** It is advisory only: a
low-confidence document is still returned, assembled, and delivered
exactly like a high-confidence one — the flag exists so
`scan-bridge`/Paperless (or a caller reading the response directly)
can route it to manual review instead of trusting a page that
Tesseract itself was not confident about.

The threshold is `ocr.min_confidence` (a profile field, `0..100`,
default `80` — matching this component's own
`internal/pipeline.defaultMinOCRConfidence`) — omitted or `0` applies
the default, same "zero means apply the documented default" contract
`ocr.languages` already has.

### Auto language detection

Setting a profile's `ocr.languages` to exactly `[auto]` (no other
entries — `internal/pipeline/exec_argv.go`'s `isAutoLanguageRequest`)
requests a **two-pass** auto-detect flow
(`internal/pipeline/exec_pipeline.go`'s `ocrPageAuto`) instead of a
fixed language set, per page:

1. OCR the page with the project's default (`deu+eng`).
2. Score that pass's recognized text against a small, hand-picked
   stopword list per installed language
   (`internal/pipeline/exec_argv.go`'s `detectLanguage`) to guess the
   page's actual language.
3. If the guess is empty, or already covered by `deu+eng`, stop —
   nothing more to gain from a second pass.
4. Otherwise, re-OCR once with just the detected language and keep
   whichever of the two passes scored the higher mean confidence (the
   confidence gate's own signal doubles as the pass-selection
   criterion here).

This deliberately never asks Tesseract for more than two languages in
one invocation — passing every installed language pack to `-l`
simultaneously was evaluated and found to measurably *reduce*
recognition quality (the same dictionary-overlap effect the "OCR
languages" section above warns about, just amplified across more
languages at once), so `auto` costs at most one extra tesseract pass,
never a worse result than a single fixed-language pass would give.

**This is a pragmatic heuristic, not a real language-identification
model.** No such library is a dependency of this module (`go.mod`
carries none — only the Go standard library). Its accuracy limits are
real and worth knowing before relying on it in production:

- **Short OCR text** (a page with only a handful of recognized words)
  gives the stopword scorer little to work with and often yields no
  guess at all, in which case the `deu+eng` pass 1 result is kept
  unchanged.
- **Closely related languages** that share common short words —
  Spanish/Italian/Portuguese in particular — can be misidentified
  against each other.
- **Heavily garbled OCR output** (a poor scan, wrong orientation, a
  mostly-graphical page) produces text the heuristic cannot score
  meaningfully, same effect as short text.

The confidence gate above is the safety net for all three: a wrong
`auto` guess still goes through the same `ocr_confidence`/
`low_confidence` flagging as a manually configured language list, so
a misdetection surfaces as a flagged document for review rather than
silently-bad, unflagged OCR. For a profile whose documents are
reliably in one non-default language, naming that language explicitly
(e.g. `[fra]`) remains more accurate and cheaper (one pass, not up to
two) than `auto`.

## API surface

| Endpoint   | Method | Contract |
| ---------- | ------ | -------- |
| `/health`  | GET    | Always `200` — process liveness only. |
| `/process` | POST   | `multipart/mixed` request in, `multipart/mixed` response out. See below. |

### `POST /process`

Single-flight: a second concurrent request while one is already
running gets `409 processor_busy` immediately, mirroring
`sane-runtime`'s `POST /scan` behaviour for the same reason (this
component drives at most one processing job at a time).

Request body — `multipart/mixed`, part 0 `application/json`:

```json
{
  "request_id": "...",
  "ocr": {
    "enabled": true,
    "languages": ["deu", "eng"],
    "min_confidence": 80
  },
  "deskew": true,
  "remove_blank": true,
  "rotate_pages": false,
  "page_grouping": "combined",
  "output_format": "pdf",
  "timeout_seconds": 120
}
```

`ocr.min_confidence` is optional (`0..100`, omitted or `0` applies the
default of `80`) — see "OCR confidence gate" above. `ocr.languages`
may instead be exactly `["auto"]` to request the two-pass auto-detect
flow described in "Auto language detection" above, instead of a fixed
language set.

parts 1..N are the job's TIFF pages, `Content-Type: image/tiff`, in
order.

Success (`200`) is `multipart/mixed; boundary=...`:

- Part 0 — `application/json`, the process metadata (`request_id`,
  `documents: [{index, page_count, filename, content_type, warnings,
  ocr_confidence, low_confidence}]`, `duration_ms`). `ocr_confidence`
  (`0..100`) and `low_confidence` are the confidence gate's result for
  that document — both are the zero value (`0`/`false`) when
  `ocr.enabled` was `false`.
- Parts 1..N — the assembled document(s)' bytes, in the same order as
  the metadata's `documents` array — one part when `page_grouping` is
  `combined`, one per surviving source page when `per_page`.

Errors use the `{"error": "...", "hint": "..."}` envelope, matching
`sane-runtime`'s and `scan-bridge`'s `internal/api` shape:

| Condition                                             | Status | `error`              |
| ------------------------------------------------------- | ------ | ---------------------- |
| Bad request body (validation, unknown field, no pages) | 400    | `invalid_request`     |
| `page_grouping`/`output_format` value not supported, or a JPEG `combined` request with more than one page | 400 | `unsupported_format` |
| A process request is already in progress               | 409    | `processor_busy`      |
| OCR or another processing stage failed                | 422    | `processing_failed`   |
| Processing did not finish within `timeout_seconds`     | 504    | `processing_timeout`  |
| Anything else                                          | 500    | `process_failed`      |

`timeout_seconds` omitted or `0` falls back to a 120-second default
(matching the design doc's example profile).

## Pipeline stages

Applied in order, each independently skippable per the request's
flags (design doc sec. 4.3):

1. Input validation (pages present, `page_grouping`/`output_format`
   recognised).
2. Deskew (`convert -deskew 40%`), if `deskew`.
3. Blank-page removal (`identify -format "%[fx:mean]"`, mean
   brightness ≥ 0.98 classifies a page blank), if `remove_blank`.
4. Rotation correction (`tesseract --psm 0` orientation detection +
   `convert -rotate`), if `rotate_pages`.
5. OCR (`tesseract ... -l <languages> pdf tsv`, default `deu+eng`), if
   `ocr.enabled`. The always-requested `tsv` output feeds the
   confidence gate (mean per-word confidence vs. `ocr.min_confidence`,
   default `80` — see "OCR confidence gate" above); `ocr.languages:
   ["auto"]` runs the two-pass auto-detect flow instead of a fixed
   language set (see "Auto language detection" above). For
   `output_format=pdf` this produces the final searchable per-page PDF
   directly (Tesseract's own PDF output mode) — no separate "assemble
   then OCR" step. For `jpeg`/`tiff`, OCR still runs (a page that
   defeats OCR entirely is still a failure, and the confidence gate
   still applies), but its text layer is discarded since neither
   format can carry
   one.
6. Format conversion (`convert`) for pages not already produced by the
   OCR step.
7. Multi-page assembly: `combined` merges every surviving page into
   one document (`qpdf --empty --pages ... --` for PDF, `convert` for
   a multi-page TIFF); `per_page` emits one document per surviving
   page. A `combined` request for `output_format=jpeg` with more than
   one surviving page is rejected (`400 unsupported_format`) — JPEG
   cannot hold multiple pages per file.

`scan-processor` does not know about profiles, destinations, or
Paperless — it receives processing parameters and page bytes and
returns assembled document bytes plus per-document page counts and
warnings, per design doc sec. 4.3's closing paragraph.
