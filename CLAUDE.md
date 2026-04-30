# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository status

This repository is currently in **Phase 0** (foundation / documentation
skeleton — see `ROADMAP.md`). The directory tree described in
`README.md` and `AGENTS.md` (`components/`, `deploy/`, `tests/`, etc.)
does **not yet exist on disk**. Most documents here are forward-looking
specifications for the code that will be written in Phase 1+.

When asked to "add a feature" or "implement X", first verify whether the
target file/directory exists. If it does not, scaffolding the layout
described in `AGENTS.md` is usually the right first step — but flag the
fact that you are creating new structure rather than editing existing
code.

There is no `Makefile`, no `go.work`, no Compose files, and no CI
workflows committed yet. Commands listed in `CONTRIBUTING.md`
(`make test`, `tilt up`, `go work sync`, …) describe the *intended*
developer workflow once Phase 1 lands; do not assume they currently run.

## Project identity

`paperless-scan-bridge` is a container-first stack that turns a
SANE-compatible USB scanner attached to a Raspberry Pi into a
hands-free ingestion pipeline for Paperless-ngx, with a Synology NAS as
the storage hub. Reference hardware: Kodak ScanMate i1120 on a Pi 5
running Ubuntu Server 24.04 arm64.

This is a personal home-lab project. Do not introduce examples,
terminology, or scenarios from any commercial or customer context.

## Non-negotiable architectural principles

These constrain every change. `AGENTS.md` is the canonical reference;
the highlights:

- **Container-first, host-thin.** The only acceptable host modifications
  on the Pi are: install Docker + compose plugin, mount the Synology NFS
  share via `/etc/fstab`, install udev rules under `/etc/udev/rules.d/`.
  If a feature appears to need host-level installation (SANE, scanbd,
  Python runtimes, language toolchains on the host), propose a
  containerized alternative first.
- **Three custom container images, no more.** All code we ship lives
  under `components/{scan-bridge,sane-runtime,scan-processor}`.
  Paperless-ngx, scanservjs, watchtower, node-exporter and similar are
  *adopted upstream images* — we provide compose/config, never forks.
- **Synology is the single source of truth for documents.** The Pi is
  an ingestion node, not a storage node.
- **No cloud dependencies for core functionality.** No AWS/GCP/SaaS.
  Optional integrations must be clearly labeled.
- **No `latest` tags in compose files.** Pin specific versions; renovate
  bumps them.

## Technology choices (do not silently swap)

| Concern              | Choice                                      |
| -------------------- | ------------------------------------------- |
| Daemon + pipeline    | Go (single static binary, ARM64 cross-build) |
| Container build      | `docker buildx bake` (multi-arch)           |
| Local dev loop       | Tilt                                        |
| Docs                 | Zensical (MkDocs Material successor), EN + DE |
| CI / registry        | GitHub Actions / GHCR                       |
| Config formats       | YAML for compose, TOML for Go services      |
| Secrets              | SOPS + age                                  |
| Backup               | restic                                      |

Replacements need explicit justification in the issue/PR.

## Coding conventions (enforced by pre-commit + CI once wired up)

- **Go:** `gofmt`/`goimports` clean, `golangci-lint` clean, errors
  wrapped as `fmt.Errorf("context: %w", err)` (context **before** the
  error), `context.Context` propagated through every I/O function, HTTP
  handlers thin and separate from business logic, table-driven tests
  with the standard `testing` package.
- **Shell:** `#!/usr/bin/env bash` + `set -euo pipefail` immediately,
  `shellcheck` clean at strict level (`-S style`).
- **YAML:** two-space indent, sequence items aligned with key (no extra
  indent), booleans as `true`/`false` only.
- **Markdown:** 80-column prose wrap, ATX headings, one-sentence-per-line
  acceptable in long-form docs for diff-friendliness.
- **Dockerfiles:** `hadolint` clean, multi-stage when build tools are
  involved, non-root by default, base images pinned by digest in
  production-track images.
- **Commit messages:** Conventional Commits. Scope is a directory or
  component (`scan-bridge`, `sane-runtime`, `scan-processor`, `compose`,
  `ansible`, `docs`, `ci`, `deploy`).

## Testing intent (post-Phase-1)

The full suite will be `make test`, decomposing into `make test-go`,
`test-shell`, `test-yaml`, `test-docker`, `test-docs`, plus
`test-ansible` / `test-molecule` for the Ansible layer. Integration
tests under `tests/integration/` will bring up the full compose stack
against a mocked SANE scanner. Until those targets exist, run the
underlying tools directly (`go test ./...`, `shellcheck path/to/script`,
`yamllint .`, `markdownlint <file>`).

## Common task playbooks

When asked to add a **scan profile**:
1. Edit `components/scan-bridge/internal/profiles/defaults.yaml`
2. Update JSON schema at `components/scan-bridge/api/schema/profile.json`
3. Add a test in `components/scan-bridge/internal/profiles/profiles_test.go`
4. Document in `docs/getting-started/scan-profiles.md`

When asked to add **hardware compatibility**:
1. Add a row to `HARDWARE_COMPATIBILITY.md`
2. Add the udev rule to `deploy/udev/99-paperless-scan-bridge.rules`
3. If SANE config is needed, add it under `components/sane-runtime/config/`
4. Add model notes in `docs/hardware/<vendor>-<model>.md`

When asked to write a **blog post**: parallel files in
`docs/blog/posts/en/` and `docs/blog/posts/de/` using the front matter
template at `docs/.templates/blog-post.md`; assets in
`docs/static/images/blog/<slug>/`.

## Scope guardrails — what this repo deliberately is not

- not a generic DMS (that is Paperless-ngx)
- not a SANE distribution (that is upstream sane-project)
- not a Home Assistant fork (we ship blueprints only)
- not a Synology package (DSM is a black-box NFS server to us)
- not a Kubernetes operator (Compose is the reference deployment)

Feature requests crossing these boundaries belong in another project.

## Pre-merge review validation

Before merging any PR, **always** fetch the PR's reviews and review
comments and address every legitimate point. Use the GitHub MCP tools
(`pull_request_read` with `method=get_reviews` and
`method=get_review_comments`) to enumerate them. For each item:

- If the point is correct → fix it on the PR branch and push, then
  re-check that no new comments arrived.
- If the point is wrong, outdated, or out of scope → reply on the
  thread explaining why before merging.

Never merge while unresolved, legitimate review feedback exists. This
applies equally to bot reviewers (Gemini Code Assist, Copilot, …) and
human reviewers. The user has stated explicitly: *"Das Feedback wollen
wir IMMER haben."*

## Code-review language

**Pull-Request-Reviews, Review-Kommentare und PR-Beschreibungen sind
auf Deutsch zu verfassen.** Das gilt für Zusammenfassungen,
Inline-Kommentare und Vorschläge — sowohl für Claude Code als auch für
Copilot, Gemini und andere KI-Assistenten. Code-Identifier,
Commit-Message-Beispiele, CLI-Befehle und zitierte Log-Ausgaben bleiben
unverändert in der Originalsprache. Commit-Messages selbst folgen
weiterhin den Conventional-Commits-Regeln (englisch).

## Further reading inside the repo

- [AGENTS.md](AGENTS.md) — the canonical brief for AI assistants; richer than this file
- [ARCHITECTURE.md](ARCHITECTURE.md) — full data-flow diagram and component
  responsibilities, including the three supported storage topologies
- [CONTRIBUTING.md](CONTRIBUTING.md) — human-facing workflow, full lint/test commands
- [CONCEPT.md](CONCEPT.md), [CONTAINER_SUITE.md](CONTAINER_SUITE.md) — long-form design notes
- [THREAT_MODEL.md](THREAT_MODEL.md), [DISASTER_RECOVERY.md](DISASTER_RECOVERY.md) — security and recovery posture
- [ROADMAP.md](ROADMAP.md) — current phase and what is in flight
