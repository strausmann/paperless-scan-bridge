// Multi-arch image builds for the three custom components.
//
// The reference deployment is a Raspberry Pi 5 on arm64, but everything
// is developed and tested on amd64, so both platforms are built from the
// same source. This is the `docker buildx bake` file CLAUDE.md names as
// the container-build tool and ci.yml's build job had as a placeholder
// until 2026-08-27 (issue #86).
//
// Each context is the component directory, NOT the repository root: the
// three Dockerfiles copy `go.mod`/`go.sum` from their own directory, and
// building from the root fails on a missing file. compose.yaml already
// sets the contexts this way; the two must not drift.

variable "REGISTRY" {
  default = "ghcr.io/strausmann/paperless-scan-bridge"
}

// Overridden by CI with the short commit SHA and the release tag. `dev`
// on a local `docker buildx bake` with nothing set, which matches the
// compose default (`PSB_VERSION:-dev`) so a locally built image and a
// locally composed one report the same version.
variable "VERSION" {
  default = "dev"
}

variable "COMMIT" {
  default = "unknown"
}

variable "BUILD_DATE" {
  default = "unknown"
}

// ADR 0011 forbids `latest` in compose files. It is still published here
// as a moving pointer for `docker pull` convenience, but nothing this
// repository deploys ever references it -- compose pins a version.
variable "TAG_LATEST" {
  default = "false"
}

// Tag cascade for a real release, matching the tagging convention used
// across the other HomeLab image builds: the exact version plus floating
// major and major.minor pointers, plus `latest`.
//
//     1.2.0   immutable, what a production compose pins
//     1.2     moves with every patch of that minor
//     1       moves with every release of that major
//     latest  moves with every release
//
// The leading `v` from the git tag is stripped: the convention is
// `1.2.0`, not `v1.2.0`. `VERSION` arrives from CI as `git describe`
// output, so it carries the `v` on a release build.
//
// On a non-release build (`TAG_LATEST` false) `VERSION` is a commit SHA
// or `dev` — neither can be split into semver parts, so that path
// publishes the single raw tag and nothing else.
function "image_tags" {
  params = [image]
  result = TAG_LATEST == "true" ? [
    "${REGISTRY}/${image}:${trimprefix(VERSION, "v")}",
    "${REGISTRY}/${image}:${join(".", slice(split(".", trimprefix(VERSION, "v")), 0, 2))}",
    "${REGISTRY}/${image}:${split(".", trimprefix(VERSION, "v"))[0]}",
    "${REGISTRY}/${image}:latest",
  ] : ["${REGISTRY}/${image}:${VERSION}"]
}

group "default" {
  targets = ["scan-bridge", "sane-runtime", "scan-processor"]
}

target "_common" {
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    VERSION    = VERSION
    COMMIT     = COMMIT
    BUILD_DATE = BUILD_DATE
  }
  labels = {
    "org.opencontainers.image.source"   = "https://github.com/strausmann/paperless-scan-bridge"
    "org.opencontainers.image.licenses" = "MIT"
    "org.opencontainers.image.version"  = VERSION
    "org.opencontainers.image.revision" = COMMIT
    "org.opencontainers.image.created"  = BUILD_DATE
  }
}

target "scan-bridge" {
  inherits   = ["_common"]
  context    = "components/scan-bridge"
  dockerfile = "Dockerfile"
  tags = image_tags("scan-bridge")
  labels = {
    "org.opencontainers.image.title"       = "scan-bridge"
    "org.opencontainers.image.description" = "REST API, profile dispatch and metrics for the scan pipeline"
  }
}

target "sane-runtime" {
  inherits   = ["_common"]
  context    = "components/sane-runtime"
  dockerfile = "Dockerfile"
  tags = image_tags("sane-runtime")
  labels = {
    "org.opencontainers.image.title"       = "sane-runtime"
    "org.opencontainers.image.description" = "SANE drivers and USB integration; owns the physical scanner"
  }
}

target "scan-processor" {
  inherits   = ["_common"]
  context    = "components/scan-processor"
  dockerfile = "Dockerfile"
  tags = image_tags("scan-processor")
  labels = {
    "org.opencontainers.image.title"       = "scan-processor"
    "org.opencontainers.image.description" = "Deskew, blank-page removal, PDF assembly and atomic NFS write"
  }
}

// Single-platform variant for CI's pull-request runs and for local
// smoke tests: `--load` cannot load a multi-platform manifest into the
// local daemon, so a PR build that wants an image it can actually run
// has to narrow the platform list.
target "local" {
  inherits  = ["scan-bridge"]
  platforms = ["linux/amd64"]
}
