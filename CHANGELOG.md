# Changelog

All notable changes to `paperless-scan-bridge` are documented in this
file.

The format is based on [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## Categories used

Each release entry uses some or all of these categories:

- **✨ Added** — for new features
- **♻️ Changed** — for changes in existing functionality
- **⚠️ Deprecated** — for soon-to-be-removed features
- **🗑️ Removed** — for removed features
- **🐛 Fixed** — for bug fixes
- **🔒 Security** — for vulnerabilities and security-relevant changes
- **🔧 Compatibility** — for compatibility constraints between component versions
- **📝 Documentation** — for documentation-only changes that affect the user

## Versioning notes

Each of the three custom container images versions independently:

- `scan-bridge`
- `sane-runtime`
- `scan-processor`

Repository releases (which include all three plus configuration,
documentation, and deployment artifacts) use a unified version. A
repository release at `v1.2.3` may bundle, for example, `scan-bridge
v1.2.0`, `sane-runtime v1.1.5`, and `scan-processor v1.2.3`. The
release notes for each tag list the exact component versions
included.

Compatibility constraints between component versions are documented
in the **Compatibility** section of each release entry.

---

## [Unreleased]

Changes that are merged to `main` but not yet released. Maintained
between releases as a running list.

### Added

- Repository scaffolding and GitHub configuration boilerplate: the
  Phase 1 directory tree under `components/`, `deploy/`,
  `homeassistant/`, `n8n/`, `backup/`, `monitoring/`, `security/`,
  `ha/`, `docs/`, `tests/` (each preserved by an annotated
  `.gitkeep`), plus `.gitignore`, `.gitattributes`, `.editorconfig`,
  the `.github/` issue and PR templates, `CODEOWNERS`,
  `dependabot.yml`, the `ci.yml` and `docs.yml` workflow stubs, the
  `.pre-commit-config.yaml` (hooks staged manual), and a `Makefile`
  orchestrator that lists every Phase 1 test target.

### Changed

### Fixed

### Security

---

## [0.1.0] — 2026-04-30

The initial public release. This release completes Phase 0 of the
roadmap: repository foundation, documentation, and license. No
working code is included; that arrives in Phase 1 (`v0.2.0`).

This release exists so that early viewers, contributors, and search
engines have a stable reference point. The project goal, the scope,
and the architectural direction are now public artifacts that can be
linked to and built upon.

### Added

- Repository created at `github.com/strausmann/paperless-scan-bridge`
- MIT license
- `README.md` with project overview, quickstart, repository layout,
  roadmap summary, and trademark notices
- `CONCEPT.md` — master concept document covering vision, goals,
  scope, target users, use cases, technology decisions, risks, and
  decision log
- `ARCHITECTURE.md` — technical architecture with three-layer model,
  component inventory, three storage topologies, and trade-offs
- `CONTAINER_SUITE.md` — detailed specification of the three custom
  container images including Dockerfiles, build pipeline, USB
  handling, image strategy, and release process
- `ROADMAP.md` — four-phase delivery plan with checkable tasks
- `CONTRIBUTING.md` — contribution workflow, code style, test
  expectations, and the container-first principle
- `CODE_OF_CONDUCT.md` — Contributor Covenant 2.1
- `SECURITY.md` — vulnerability disclosure policy with CVSS-based
  severity levels and 48-hour acknowledgement commitment
- `THREAT_MODEL.md` — STRIDE-based analysis with 23 documented
  threats, six trust zones, three attacker profiles, and a residual
  risk inventory
- `DISASTER_RECOVERY.md` — three-layer backup architecture (hourly
  PostgreSQL, nightly restic, weekly off-site), seven disaster
  scenario runbooks, restore procedures, key management, and the
  quarterly restore test process
- `HARDWARE_COMPATIBILITY.md` — compatibility level system, Kodak
  ScanMate i1120 reference entry, six likely-compatible scanner
  families seeded for community testing, trigger and storage
  backend tables
- `AGENTS.md` — repository description targeted at AI coding
  assistants, including conventions and explicit boundaries
- This `CHANGELOG.md`

### Documentation

- Documentation site planned at
  [scan-bridge.strausmann.de](https://scan-bridge.strausmann.de)
  using Zensical as the static site generator
- Site infrastructure (custom domain, GitHub Pages workflow,
  Zensical configuration) tracked for the v0.2.0 release alongside
  Phase 1 implementation work

### Compatibility

- This release contains no executable code; compatibility constraints
  do not apply
- Documentation references future component versions that do not yet
  exist; these are forward-looking and will materialize in v0.2.0
  through v0.5.0

### Notes

This release is suitable for:

- Reading and providing feedback on the architectural direction
- Linking to the project from related discussions
- Forking as a template for similar documentation-first projects
- Subscribing to releases to be notified when Phase 1 lands

This release is not suitable for:

- Running anything (there is nothing to run yet)
- Production use of any kind
- Hardware compatibility validation (the runtime does not exist)

---

## Future releases

Anticipated milestone versions, derived from the roadmap. These are
not commitments; the actual cadence depends on available time. Listed
to give contributors and watchers a sense of the trajectory.

### v0.2.0 — Minimum viable stack (Phase 1)

Anticipated additions:

- `scan-bridge` daemon v0.2.0 — Go binary, REST API, profile
  dispatch, BoltDB job persistence, Prometheus metrics
- `sane-runtime` container v0.2.0 — Debian slim with SANE, scanbd,
  Go HTTP wrapper
- `scan-processor` container v0.2.0 — Go pipeline with deskew,
  blank page detection, atomic NFS write
- Bash bootstrap script under `deploy/bootstrap/`
- Reference Compose stack for Topology B (NFS direct) under
  `deploy/compose/`
- Tilt configuration for local development
- GitHub Actions for multi-arch container builds, GHCR push, cosign
  signing, SBOM generation
- First version of the documentation site

### v0.3.0 — Trigger paths (Phase 2)

Anticipated additions:

- Hardware button support in `sane-runtime` via scanbd
- Home Assistant blueprint for IKEA STYRBAR
- Home Assistant blueprint for IKEA SYMFONISK Sound Remote Gen 2
- Home Assistant blueprint for IKEA RODRET
- n8n workflow exports
- scanservjs integration in the Compose stack
- Documentation: trigger path comparison, blueprint usage

### v0.4.0 — Production hardening (Phase 3)

Anticipated additions:

- restic backup automation under `backup/`
- PostgreSQL hourly dump pipeline
- Restore test automation in CI
- Prometheus exporters and Grafana dashboards
- Synthetic health check container
- SOPS secrets management with age keys
- CrowdSec integration for SSH and webhook protection
- Watchtower with explicit allowlist
- Compose stacks for Topology A (local FS + restic) and Topology C
  (iSCSI LUN)

### v1.0.0 — Maturity (Phase 4)

The first stable release. Criteria:

- All three components running in production at the maintainer's site
  for at least 90 days
- At least 15 verified scanner models in the hardware compatibility
  list
- At least one successful disaster recovery exercise documented
- Quarterly restore tests completed for at least two consecutive
  quarters
- Contributors other than the maintainer have merged at least three
  PRs

After v1.0.0, the versioning settles into normal Semantic Versioning
maintenance: bug fix releases as v1.x.y, feature releases as v1.X.0,
breaking changes as v2.0.0 with a documented migration path.

---

## Changelog maintenance

### When to add entries

Every PR that affects user-visible behavior, interfaces, or operational
procedures should include a CHANGELOG entry under `[Unreleased]`. PRs
that only refactor internal code or add tests do not require entries.

### Entry style

- One sentence per entry, in the imperative mood ("Add", "Fix",
  "Change", not "Added" or "Adds")
- Reference the affected component in parentheses where relevant:
  `Add /profiles endpoint (scan-bridge)`
- Reference the issue or PR number at the end: `Fix race condition
  in atomic write (#142)`
- Group entries by category within each release

### Release process

When cutting a release:

1. Move all `[Unreleased]` entries into a new dated release section
2. Determine the version number per Semantic Versioning rules
3. List the included component versions in the **Compatibility**
   section
4. Add release notes context as a paragraph at the top of the entry
   if the release is significant
5. Tag the merge commit with `vX.Y.Z`
6. Push the tag; CI generates the GitHub Release with this changelog
   entry as the release notes body

### Older entries

This changelog is forward-only. Older entries are not edited after
release except for typo fixes. If an entry is later found to be
incorrect, a correction goes into the next release entry rather than
modifying historical records.

---

## Links

- [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)
- [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html)
- [Repository releases page](https://github.com/strausmann/paperless-scan-bridge/releases)
- [Roadmap](ROADMAP.md)
