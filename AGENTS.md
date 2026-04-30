# AGENTS.md

> This file describes the repository for AI coding assistants such as
> Claude Code, Cursor, Copilot, and similar tools. Human contributors
> should read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## Project identity

`paperless-scan-bridge` is a container-first stack that turns any
SANE-compatible USB document scanner attached to a Raspberry Pi into a
hands-free ingestion pipeline for Paperless-ngx. The reference platform
is a Kodak ScanMate i1120 on a Raspberry Pi 5 running Ubuntu Server
arm64, with documents stored on a Synology NAS.

The repository is a personal home-lab project and is not affiliated with
any employer. Do not introduce examples, terminology, or scenarios
borrowed from any commercial or customer context.

## Architectural principles

These principles drive every design decision. Honor them in any code you
generate.

**Container-first.** The Pi host should require only Docker, an NFS
mount, and udev rules. All scanning, processing, and dispatch logic runs
in containers. Do not propose installing SANE, scanbd, Python runtimes,
or language toolchains on the Pi host directly. The acceptable host
modifications are:

1. Install Docker and the compose plugin
2. Mount the Synology NFS share via `/etc/fstab`
3. Install the udev rules file under `/etc/udev/rules.d/`

If a feature seems to require host-level installation, propose a
container-based alternative first.

**Three custom components.** Source for the three container images we
ship lives under `components/`:

- `components/scan-bridge/` — Go daemon, REST API, profile dispatch,
  Prometheus metrics. Single static binary, distroless base image.
- `components/sane-runtime/` — SANE drivers and scanbd, Debian slim
  base. Built with bash entrypoint; thin Go health-check helper.
- `components/scan-processor/` — Go service for image processing, blank
  page detection, PDF assembly, atomic NFS writes.

**Adopted upstream images.** Paperless-ngx, scanservjs, watchtower,
node-exporter — these are upstream images. We provide compose files and
configuration but do not fork the images.

## Repository layout

```
paperless-scan-bridge/
├── components/             # Source for the three custom container images
│   ├── scan-bridge/        # Go daemon
│   ├── sane-runtime/       # Bash + Go, SANE container
│   └── scan-processor/     # Go service for PDF pipeline
├── deploy/                 # Deployment artifacts
│   ├── compose/            # Docker Compose stacks
│   ├── bootstrap/          # Pi bootstrap script (Bash, runs once)
│   ├── ansible/            # Optional Ansible layer for fleet deployment
│   └── udev/               # Stable USB device path rules
├── homeassistant/          # Importable HA blueprints (YAML)
├── n8n/                    # Exported n8n workflows (JSON)
├── backup/                 # restic wrappers, PG dumps, restore runbooks
├── monitoring/             # Prometheus exporters, Grafana dashboards
├── security/               # Hardening profiles, CrowdSec collections
├── ha/                     # Cold-standby setup and runbooks
├── docs/                   # Zensical source for the documentation site
├── tests/                  # Bats, integration tests, CI configuration
└── .github/                # Workflows, issue templates, PR template
```

## Technology choices

These choices are deliberate and should not be replaced without explicit
discussion in an issue or PR description.

| Concern              | Choice                  | Reasoning                                                    |
| -------------------- | ----------------------- | ------------------------------------------------------------ |
| Daemon language      | Go                      | Single static binary, ARM64 cross-build, small container     |
| Pipeline language    | Go                      | Same toolchain, good PDF libraries (pdfcpu)                  |
| Container build      | docker buildx bake      | Multi-arch, parallel, single command                         |
| Local development    | Tilt                    | Live rebuild on file change, container-first dev loop        |
| Documentation        | Zensical                | Successor to MkDocs Material, MIT, multi-language native     |
| Doc site hosting     | GitHub Pages + custom domain | `scan-bridge.strausmann.de`                              |
| Doc languages        | English (primary), German | i18n via Zensical, EN under `/`, DE under `/de/`           |
| CI/CD                | GitHub Actions          | Native, free for public repos                                |
| Container registry   | GHCR                    | Native to GitHub, no separate auth                           |
| Configuration        | YAML for compose, TOML for Go services | Standard conventions per ecosystem                |
| Secrets              | SOPS with age keys      | Git-compatible, no external service                          |
| Backup               | restic                  | Deduplication, encryption, well-tested                       |
| Storage default      | Local FS + restic to NAS | Inotify works, backup explicit                              |

If you propose a different technology, justify the change inline and
note the migration cost.

## Coding conventions

**Go code:**

- `gofmt` and `goimports` clean
- `golangci-lint run` passing with the project's `.golangci.yml`
- Errors wrapped with `fmt.Errorf("context: %w", err)`
- Context propagated through every function that does I/O
- Tests use the standard `testing` package; table-driven where it helps
- HTTP handlers separated from business logic — handlers thin, services
  pure where possible
- `go.work` ties the components together as a workspace

**Shell scripts:**

- `#!/usr/bin/env bash` at the top
- `set -euo pipefail` immediately after the shebang
- `shellcheck` clean at the strict level (`-S style`)
- Functions documented with a leading block comment
- Arguments quoted unless word-splitting is intentional

**YAML:**

- `yamllint` clean against the project config
- Two-space indentation
- Sequence items aligned with the key (no extra indent)
- Booleans as `true`/`false`, never `yes`/`no` or `on`/`off`

**Markdown:**

- 80-column wrap for prose
- No hard wraps inside code blocks
- ATX-style headings (`#`, `##`)
- Reference-style links allowed but inline preferred
- One sentence per line is acceptable for diff-friendliness in docs

**Dockerfiles:**

- `hadolint` clean
- Multi-stage builds for any image with build-time tools
- Pin base images by digest in production-track images
- Run as non-root user where possible
- Document every `RUN` step's purpose with a comment

**Commit messages:** Conventional Commits format. Examples:

- `feat(scan-bridge): add /profiles endpoint`
- `fix(sane-runtime): handle USB device disconnect during scan`
- `docs(architecture): clarify NFS polling tradeoff`

## Boundaries — things to avoid

These are not arbitrary preferences. Each one prevents a specific
failure mode I have observed in similar projects.

- **Do not introduce host-level installations.** If a feature requires
  it, propose a container alternative first. Document why if there is
  truly no alternative.
- **Do not depend on Insiders or proprietary tools.** Everything in this
  repository must be reproducible from public, MIT/Apache/BSD-licensed
  sources.
- **Do not pin to "latest" tags in compose files.** Use specific
  versions; renovate keeps them up to date.
- **Do not add cloud dependencies.** No AWS, no GCP, no third-party
  SaaS for core functionality. Optional integrations are fine if
  clearly labeled.
- **Do not generate marketing copy.** Documentation should be technical
  and honest about trade-offs and known limitations.
- **Do not silently swallow errors.** Log them, return them, or panic if
  the program cannot continue. Empty catch blocks are forbidden.

## Testing expectations

Every PR must pass:

- `go test ./...` for any modified Go package
- `shellcheck` for any modified shell script
- `yamllint` for any modified YAML file
- `markdownlint` for any modified Markdown file
- The `bats` tests under `tests/bats/` for scripts that have them
- The integration tests under `tests/integration/` if compose files
  are touched

Run the full suite with `make test`.

For Ansible role changes (under `deploy/ansible/`), Molecule tests
must pass against Ubuntu 22.04, Ubuntu 24.04, and Debian 12 containers.

## Common tasks

When asked to add a scan profile:

1. Edit `components/scan-bridge/internal/profiles/defaults.yaml` to add
   the profile entry
2. Update the JSON schema under `components/scan-bridge/api/schema/profile.json`
3. Add a test case in `components/scan-bridge/internal/profiles/profiles_test.go`
4. Document the new profile in `docs/getting-started/scan-profiles.md`

When asked to add hardware compatibility:

1. Edit `HARDWARE_COMPATIBILITY.md` to add the row
2. Add the udev rule in `deploy/udev/99-paperless-scan-bridge.rules`
3. If the scanner needs SANE configuration, add it to
   `components/sane-runtime/config/`
4. Update `docs/hardware/` with any model-specific notes

When asked to write a blog post:

1. Add a Markdown file to `docs/blog/posts/en/` for the English version
2. Add a parallel file to `docs/blog/posts/de/` for the German version
3. Use the front matter template from `docs/.templates/blog-post.md`
4. Image assets go in `docs/static/images/blog/<slug>/`

## Security-relevant notes

- Container images do not run as root unless explicitly required
- The scan-bridge daemon runs as a non-privileged user inside the
  container; the only privileged operation is the device cgroup
  permission for `/dev/bus/usb`
- All secrets are loaded from environment variables or SOPS-encrypted
  files; never hardcode credentials
- The bootstrap script is auditable — it writes only to documented
  paths and never pulls binaries from arbitrary sources

## What this repository is not

To prevent scope creep, here is what this repository deliberately does
not try to be:

- It is not a generic DMS — that is Paperless-ngx
- It is not a SANE distribution — that is the upstream sane-project
- It is not a Home Assistant fork — we ship blueprints, nothing more
- It is not a Synology package — DSM is treated as a black-box NFS server
- It is not a Kubernetes operator — Compose is the reference deployment

If a feature request crosses these boundaries, the right answer is
usually "that lives in another project."
