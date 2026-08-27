# syntax=docker/dockerfile:1.7
#
# sane-runtime — the SANE-backed scan daemon of paperless-scan-bridge.
#
# Two-stage build: stage 1 compiles the Go binary from this module
# (self-contained go.mod, stdlib-only, no go.sum); stage 2 installs
# the avision SANE backend (Kodak ScanMate i1120) on debian:12-slim
# and runs the binary over a Unix-domain socket.
#
# GOWORK=off is load-bearing: the repo-root go.work pins
# `go 1.25.12`, which is newer than most locally installed Go
# toolchains and would otherwise force a toolchain download or fail
# outright the moment this module is built outside the repo-root
# workspace context (as it is here — build context is this module
# directory, not the repo root). This module's own go.mod (`go
# 1.25.0`) is authoritative for the build.
#
# See README.md ("Status", "Configuration", "Run") for why this
# Dockerfile does not match the CONTAINER_SUITE.md sec. 5.3 sketch
# (no scanbd, no sane-airscan, no etc/scan-runtime.toml — the actual
# module never grew that surface) and ADR 0008 / ADR 0009 / ADR 0013
# for the privilege and transport decisions this image is built
# against.

ARG GO_VERSION=1.27
ARG DEBIAN_VERSION=12

# ---- Build stage ----
FROM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

# No go.sum exists (no third-party dependencies — only stdlib and
# this module's own internal packages), so there is no separate
# `go mod download` cache-priming step to do before copying sources,
# unlike components/scan-bridge/Dockerfile.
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
        -o /out/sane-runtime \
        ./cmd/sane-runtime

# ---- Runtime stage ----
# TODO(phase 1.4): pin this base image by digest once Renovate is
# wired, matching the same open TODO in
# components/scan-bridge/Dockerfile (ADR 0011 — digest-pinning is the
# target but not yet enforced without Renovate keeping it fresh).
FROM debian:${DEBIAN_VERSION}-slim AS runtime

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        sane-utils \
        libsane1 \
    && rm -rf /var/lib/apt/lists/* \
    # Prune the backend allow-list to avision only (Kodak ScanMate
    # i1120, per HARDWARE_COMPATIBILITY.md). The packaged dll.conf
    # enables every bundled SANE backend by default; each one adds
    # USB/network probe overhead on `scanimage -L` and image size for
    # a driver this deployment never uses.
    && printf 'avision\n' > /etc/sane.d/dll.conf

COPY --from=build /out/sane-runtime /usr/local/bin/sane-runtime

# --- Hardening tradeoff (documented, not silently skipped) ---
# ADR 0013 mandates non-root + read-only rootfs + cap_drop:[ALL] for
# all three custom images. sane-runtime is the one component that
# must reach the physical USB scanner (ADR 0008: device passthrough
# via device-cgroup permissions + udev, never --privileged), and the
# non-root UID/GID <-> udev-group wiring
# (CONTAINER_SUITE.md sec. 2.6/5.3 sketch: UID 10002, group "scanner"
# GID 10003) is not implemented anywhere yet on the host side —
# deploy/udev/ is still empty (.gitkeep only). This image therefore
# runs as root, which is a deliberate, temporary exception scoped to
# the Task 13 hardware smoke test, not a claim that ADR
# 0008/0013 are satisfied. Follow-up (tracked outside this task):
# create the sanesvc:scanner user, match its GID to the host udev
# rule's group once that rule exists, add `USER sanesvc:scanner`, and
# switch --device passthrough to the narrower device-cgroup rule ADR
# 0008 calls for instead of the whole /dev/bus/usb bus.

# No EXPOSE: sane-runtime is reached exclusively over the
# Unix-domain socket at SANE_RUNTIME_SOCKET (default
# /run/sane-runtime/sane.sock, see README.md "Configuration" and
# ADR 0009 — HTTP over a Unix socket, never TCP).

ENTRYPOINT ["/usr/local/bin/sane-runtime"]
