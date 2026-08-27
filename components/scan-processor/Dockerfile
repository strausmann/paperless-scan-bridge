# syntax=docker/dockerfile:1.7
#
# scan-processor — the OCR/image-processing daemon of
# paperless-scan-bridge (design doc sec. 4.4, ADR 0003, ADR 0011, ADR
# 0013).
#
# Two-stage build: stage 1 compiles the Go binary from this module
# (self-contained go.mod, stdlib-only, no go.sum — same shape as
# sane-runtime's Dockerfile); stage 2 installs tesseract-ocr plus one
# tesseract-ocr-<code> language pack per entry in
# internal/procapi/api.go's allowedOCRLanguageCodes (deu, eng, fra,
# ita, spa, nld, por — keep this list and that one in lockstep, see
# that variable's doc comment), ImageMagick, and qpdf on
# debian:12-slim, and runs the binary over a Unix-domain socket.
#
# Unlike sane-runtime (which needs root for USB device passthrough,
# see that Dockerfile's documented exception), scan-processor touches
# no hardware — it fully satisfies ADR 0013's baseline: non-root
# fixed UID, read-only-rootfs-compatible (the only writable path is
# the socket directory, expected to be a mounted volume/tmpfs — see
# design doc sec. 4.2/8), and Compose is expected to add
# `cap_drop: [ALL]` (Task 8, deployment-only, not built here).
#
# GOWORK=off is load-bearing for the same reason documented in
# sane-runtime/Dockerfile: the repo-root go.work pins a newer Go
# toolchain than may be installed on the build host, and the build
# context here is this module directory, not the repo root — this
# module's own go.mod (`go 1.25.0`) is authoritative for the build.

ARG GO_VERSION=1.27
ARG DEBIAN_VERSION=12

# ---- Build stage ----
FROM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

# No go.sum exists (no third-party dependencies — only stdlib and
# this module's own internal packages), so there is no separate
# `go mod download` cache-priming step, matching sane-runtime's
# Dockerfile.
COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/root/.cache/go-build \
    GOWORK=off \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build \
        -trimpath \
        -ldflags "-s -w \
            -X main.version=${VERSION} \
            -X main.commit=${COMMIT} \
            -X main.buildDate=${BUILD_DATE}" \
        -o /out/scan-processor \
        ./cmd/scan-processor

# ---- Runtime stage ----
# TODO(phase 1.4): pin this base image by digest once Renovate is
# wired (ADR 0011 — digest-pinning is the target but not yet
# enforced), matching the identical open TODO in
# components/sane-runtime/Dockerfile and
# components/scan-bridge/Dockerfile.
FROM debian:${DEBIAN_VERSION}-slim AS runtime

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        tesseract-ocr \
        tesseract-ocr-deu \
        tesseract-ocr-eng \
        # Additional language packs -- weitere Sprachen hier ergaenzen.
        # Each one added here MUST also be added to
        # internal/procapi/api.go's allowedOCRLanguageCodes, or
        # requests naming it fail late (422 at the pipeline) instead
        # of the cheap, immediate 400 that allowlist exists to give.
        tesseract-ocr-fra \
        tesseract-ocr-ita \
        tesseract-ocr-spa \
        tesseract-ocr-nld \
        tesseract-ocr-por \
        imagemagick \
        qpdf \
    && rm -rf /var/lib/apt/lists/* \
    # --- Documented hardening/functionality tradeoff ---
    # Debian's ImageMagick ships a policy.xml that denies the "PDF"
    # coder by default (CVE-2016-3714 mitigation — ImageMagick's own
    # PDF delegate historically had file-access issues). This
    # component's non-OCR TIFF->PDF conversion path
    # (buildConvertFormatArgs in internal/pipeline/exec_argv.go, used
    # when OCR is disabled and OutputFormat=pdf) needs "read|write"
    # rights on that coder to function; without this, ImageMagick
    # refuses with "not authorized". Every *other* PDF-producing path
    # in this component (tesseract's own PDF output mode; qpdf's
    # merge) does not go through ImageMagick's PDF delegate at all,
    # so this relaxation's blast radius is limited to the one
    # explicit conversion path that needs it — not a blanket
    # re-enable of every coder ImageMagick restricts by default.
    && sed -i \
        's/rights="none" pattern="PDF"/rights="read|write" pattern="PDF"/' \
        /etc/ImageMagick-6/policy.xml

# scansvc:scansvc — fixed non-root UID/GID, per ADR 0013. Unlike
# sane-runtime, this component needs no host device access and no
# udev-group coordination, so a plain fixed-UID system user is
# sufficient (no follow-up analogous to sane-runtime's "sanesvc:scanner"
# TODO).
RUN groupadd --system --gid 10010 scansvc \
    && useradd --system --uid 10010 --gid scansvc --no-create-home --shell /usr/sbin/nologin scansvc \
    && mkdir -p /run/scan-processor \
    && chown scansvc:scansvc /run/scan-processor

COPY --from=build /out/scan-processor /usr/local/bin/scan-processor

USER scansvc:scansvc

# No EXPOSE: scan-processor is reached exclusively over the
# Unix-domain socket at SCAN_PROCESSOR_SOCKET (default
# /run/scan-processor/scan-processor.sock, see README.md
# "Configuration" and design doc sec. 4.2 — HTTP over a Unix socket,
# mirroring ADR 0009's sane-runtime precedent).
VOLUME ["/run/scan-processor"]

ENTRYPOINT ["/usr/local/bin/scan-processor"]
