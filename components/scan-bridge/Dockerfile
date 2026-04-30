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
