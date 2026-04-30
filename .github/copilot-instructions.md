# GitHub Copilot instructions

These instructions guide GitHub Copilot (chat, code review, and inline
completions) when working in this repository. The canonical, richer
brief for AI assistants is [`AGENTS.md`](../AGENTS.md); this file
distills the points Copilot most needs at suggestion- and review-time.
Human contributors should read [`CONTRIBUTING.md`](../CONTRIBUTING.md).

## Repository status

Currently in **Phase 0** (foundation / documentation skeleton — see
[`ROADMAP.md`](../ROADMAP.md)). The directory tree described in
`README.md` and `AGENTS.md` (`components/`, `deploy/`, `tests/`, …)
does **not yet exist on disk**. Most documents are forward-looking
specifications. There is no `Makefile`, no `go.work`, no Compose files,
and no CI workflow committed yet — commands like `make test`,
`tilt up`, or `go work sync` describe the *intended* dev loop, not what
runs today.

## Project identity

`paperless-scan-bridge` is a container-first stack that turns a
SANE-compatible USB scanner attached to a Raspberry Pi into a
hands-free ingestion pipeline for Paperless-ngx, with a Synology NAS as
the storage hub. Reference hardware: Kodak ScanMate i1120 on a Pi 5
running Ubuntu Server 24.04 arm64. This is a personal home-lab project
— do not introduce examples, terminology, or scenarios from any
commercial or customer context.

## Architectural principles Copilot must respect

- **Container-first, host-thin.** The only acceptable host
  modifications on the Pi are: install Docker + compose plugin, mount
  the Synology NFS share via `/etc/fstab`, install udev rules under
  `/etc/udev/rules.d/`. **Never** suggest `apt install sane`,
  `pip install` on the host, language toolchains on the host, or
  `systemd` units that wrap host binaries. If a feature seems to need
  host-level installation, propose a containerized alternative first.
- **Three custom container images, no more.** All shipped code lives
  under `components/{scan-bridge,sane-runtime,scan-processor}`.
  Paperless-ngx, scanservjs, watchtower, node-exporter, etc. are
  *adopted upstream images* — provide compose/config, never fork.
- **Synology is the single source of truth for documents.** The Pi is
  an ingestion node, not a storage node.
- **No cloud dependencies for core functionality.** No AWS/GCP/SaaS
  suggestions for core paths. Optional integrations only, clearly
  labeled.
- **No `latest` tags in compose files.** Pin specific versions.
- **No silent error swallowing.** Empty `catch`/`recover` blocks and
  ignored `err` returns are forbidden; log, return, or panic.

## Technology choices (do not silently swap)

| Concern              | Choice                                              |
| -------------------- | --------------------------------------------------- |
| Daemon + pipeline    | Go (single static binary, ARM64 cross-build)        |
| Container build      | `docker buildx bake` (multi-arch)                   |
| Local dev loop       | Tilt                                                |
| Docs                 | Zensical (MkDocs Material successor), EN + DE       |
| CI / registry        | GitHub Actions / GHCR                               |
| Config formats       | YAML for compose, TOML for Go services              |
| Secrets              | SOPS + age                                          |
| Backup               | restic                                              |

If a suggestion would replace one of these (e.g. Python instead of Go,
Helm instead of Compose, Vault instead of SOPS), flag the change
explicitly with reasoning rather than just emitting it.

## Coding conventions

**Go**

- `gofmt` / `goimports` clean
- `golangci-lint run` clean against the project's `.golangci.yml`
- Errors wrapped as `fmt.Errorf("context: %w", err)` — context
  **before** the error
- `context.Context` propagated through every function that does I/O
- HTTP handlers thin and separate from business logic; services pure
  where possible
- Table-driven tests with the standard `testing` package

**Shell**

- `#!/usr/bin/env bash` + `set -euo pipefail` immediately after the
  shebang
- `shellcheck` clean at strict level (`-S style`)
- Functions documented with a leading block comment
- Arguments quoted unless word-splitting is intentional

**YAML**

- Two-space indentation
- Sequence items aligned with the key (no extra indent)
- Booleans as `true`/`false`, never `yes`/`no` or `on`/`off`

**Markdown**

- 80-column prose wrap, ATX-style headings
- One sentence per line is acceptable in long-form docs for
  diff-friendliness

**Dockerfiles**

- `hadolint` clean
- Multi-stage builds when build-time tools are involved
- Non-root user where possible
- Pin base images by digest in production-track images
- Comment the purpose of every `RUN` step

**Commit messages**

Conventional Commits: `type(scope): summary`. Types: `feat`, `fix`,
`docs`, `refactor`, `test`, `chore`, `ci`, `perf`, `build`, `style`.
Scopes are directories or component names: `scan-bridge`,
`sane-runtime`, `scan-processor`, `compose`, `ansible`, `docs`, `ci`,
`deploy`.

## Code-review focus areas

When Copilot reviews PRs in this repo, prioritize:

1. Violations of the container-first principle (host installs, host
   services, host language runtimes).
2. Hardcoded secrets, credentials, or non-SOPS secret handling.
3. Unpinned image tags (`:latest`, missing digest on production-track
   images).
4. Swallowed errors, missing `context.Context` propagation, or HTTP
   handlers doing business logic directly.
5. New cloud-service or SaaS dependencies on a core path.
6. Scope creep beyond the project boundaries (see below).
7. Markdown / YAML / shell lint regressions.

## Scope guardrails — what this repo deliberately is not

- not a generic DMS (that is Paperless-ngx)
- not a SANE distribution (that is upstream sane-project)
- not a Home Assistant fork (we ship blueprints only)
- not a Synology package (DSM is a black-box NFS server to us)
- not a Kubernetes operator (Compose is the reference deployment)

PRs that cross these lines should be flagged for scope rather than
silently improved.

## Further reading

- [`AGENTS.md`](../AGENTS.md) — full AI-assistant brief (canonical)
- [`ARCHITECTURE.md`](../ARCHITECTURE.md) — data flow, components,
  storage topologies
- [`CONTRIBUTING.md`](../CONTRIBUTING.md) — human workflow, full
  lint/test commands
- [`THREAT_MODEL.md`](../THREAT_MODEL.md),
  [`DISASTER_RECOVERY.md`](../DISASTER_RECOVERY.md) — security and
  recovery posture
- [`CLAUDE.md`](../CLAUDE.md) — sister file for Claude Code
