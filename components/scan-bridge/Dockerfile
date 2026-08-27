# syntax=docker/dockerfile:1.7
# go.mod declares `go 1.27.0`; GO_VERSION here must stay at or above
# that or `go mod download`/`go build` fail with "go.mod requires go
# >= 1.27.0" before a single source file is even compiled. Discovered
# building this Dockerfile locally for the Task 15 compose stack — an
# earlier default predated a go.mod bump that was never mirrored here.
# Matches sane-runtime/Dockerfile's GO_VERSION=1.27.
ARG GO_VERSION=1.27
# Alpine and Go are not independent here: upstream publishes only some
# pairings, and the pairing is what a `docker pull` resolves. 3.21 was
# the oldest Alpine paired with a 1.25.x golang tag; there is no
# golang:1.27-alpine3.21 at all, so moving Go to 1.27 forced this up
# too. Available 1.27 Alpine variants are 3.23 and 3.24 — verified
# against Docker Hub's tag list, not guessed. When GO_VERSION moves,
# check this pairing before assuming the build still resolves.
ARG ALPINE_VERSION=3.24

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
# TODO(phase 1.4): pin this base image by digest once Renovate is
# wired (AGENTS.md "Coding conventions" requires digests in
# production-track images). Pinning a digest manually here without
# Renovate keeping it fresh would silently leave the base image
# behind on security updates.
FROM gcr.io/distroless/static-debian12:nonroot

USER nonroot:nonroot

COPY --from=build /out/scan-bridge /usr/local/bin/scan-bridge
COPY --from=build /src/internal/profiles/defaults.yaml /etc/scan-bridge/profiles.yaml

EXPOSE 8080 9090

VOLUME ["/var/lib/scan-bridge"]

ENTRYPOINT ["/usr/local/bin/scan-bridge"]
CMD ["--config", "/etc/scan-bridge/config.toml"]
