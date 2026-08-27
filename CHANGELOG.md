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

- The scan panel now checks for firmware updates on its own and reports
  them on its dashboard, instead of requiring someone to locate a `.bin`
  and upload it by hand (ADR 0023). It polls the same `manifest.json`
  the browser installer uses — CI now publishes the OTA image alongside
  the factory image and extends the manifest with the `ota` block
  ESPHome's update platform reads, including the firmware's MD5. The
  panel verifies that MD5 while writing, so an interrupted or altered
  download is discarded and the running firmware survives. Checking is
  automatic; **installing stays a deliberate click** — see the ADR for
  why that constraint matters given that this framework cannot verify
  TLS certificates.

- Add the project homepage as a Zensical custom home template (Issue
  #60): landing page and documentation now live on one site, the same
  way zensical.org itself does it. Only `docs/en/index.md` opts in via
  `template: home.html` — every other page keeps the default docs
  template. Hardware-chain hero diagram (scanner → Pi → NAS →
  Paperless-ngx), self-hosted `InterVariable` + `JetBrains Mono` (no
  Google Fonts request), light-primary with dark following the site's
  existing palette toggle. Real content only, including the same
  Phase-1 status stated everywhere else in the repository. Durable
  design record in `DESIGN.md`. German homepage (`zensical.de.toml`)
  not included — follow-up.
- Add an OCR confidence gate: `ocr.min_confidence` (0..100, default
  80) flags a document `low_confidence: true` in the `POST /scan`
  response when its mean OCR confidence falls below the threshold —
  advisory only, never fails the scan (scan-processor, scan-bridge).
- Add `ocr.languages: [auto]` for a pragmatic, two-pass OCR
  language-detection flow, falling back to the `deu+eng` default and
  the confidence gate above when the guess is wrong or unsupported
  (scan-processor).
- Repository scaffolding and GitHub configuration boilerplate: the
  Phase 1 directory tree under `components/`, `deploy/`,
  `homeassistant/`, `n8n/`, `backup/`, `monitoring/`, `security/`,
  `ha/`, `docs/`, `tests/` (each preserved by an annotated
  `.gitkeep`), plus `.gitignore`, `.gitattributes`, `.editorconfig`,
  the `.github/` issue and PR templates, `CODEOWNERS`,
  `dependabot.yml`, the `ci.yml` and `docs.yml` workflow stubs, the
  `.pre-commit-config.yaml` (hooks staged manual), and a `Makefile`
  orchestrator that lists every Phase 1 test target.

- Browser-based [ESP Web Tools](https://esphome.github.io/esp-web-tools/)
  installer for the CYD scan-control panel firmware (Issue #9, phase
  E): a new `esphome-firmware.yml` CI workflow compiles
  `firmware/esp32-panel/cyd-scan-panel.yaml` and produces an ESP Web
  Tools `manifest.json`, published alongside the docs site at
  `/firmware/`; a new page under Hardware embeds the install button.
  Flashing needs Chrome or Edge (Web Serial) and no local toolchain.
  Not yet verified against real hardware — see the firmware README's
  "Hardware verification status".

- CYD scan-control panel firmware: configurable grid size and profile
  paging (Issue #9, items B1/B2). Two new persistent `number` entities
  on the panel's own dashboard, **Grid Rows** and **Grid Cols** (1–3
  each, default 2x3 — today's fixed 6-button layout, unchanged unless
  a flasher opts in), resize the button grid at runtime up to 3x3 = 9
  slots, no re-flash needed. New `<`/`>` footer buttons page through
  profile lists longer than one page (the internal profile list is now
  capped at 100, up from the previous hard 6-slot limit). Not yet
  verified against real hardware — see the firmware README's "Hardware
  verification status".

- CYD scan-control panel firmware: display orientation substitutions
  for a portrait override (Issue #9, item B4). Nine new build-time
  `substitutions:` (`orientation`, `screen_width`/`screen_height`,
  `panel_rotation`, `touch_swap_xy`/`touch_x_min`/`touch_x_max`/
  `touch_y_min`/`touch_y_max`) drive `display.dimensions`,
  `lvgl.rotation` and the touchscreen `calibration`/`transform.swap_xy`,
  letting a local build flip the panel from landscape to portrait via a
  documented `packages:` override — no pin/SPI/runtime-script changes
  needed. Defaults reproduce the existing landscape build byte-for-byte
  (merged `display.dimensions`/`lvgl.rotation` unchanged). The B1 grid
  geometry lambdas now read the new `screen_width`/`screen_height`
  globals instead of two hardcoded 320x240 literals, so the button grid
  stays correctly laid out in either orientation. Not yet verified
  against real hardware — see the firmware README's "Hardware
  verification status".

- CYD scan-control panel firmware: a centered scan-in-progress spinner
  (Issue #9, item B5). A new LVGL `spinner:` widget is shown on top of
  the button grid for the full duration of `POST /scan` and hidden
  again in every terminal branch (success, each HTTP error status, and
  a network-level failure) — the existing "Scanning: `<profile>`..."
  status label and amber LED already persisted correctly for the whole
  in-flight request before this; the spinner makes that state visually
  louder than the 20px footer label alone. Not yet verified against
  real hardware — see the firmware README's "Hardware verification
  status".

- CYD scan-control panel firmware: a three-state, color-coded top-bar
  chain-status indicator (Issue #9, item B6). `check_bridge_health` now
  polls `GET /ready` instead of the old plain `GET /health`, mirroring
  scan-bridge's real readiness contract
  (`components/scan-bridge/internal/api/ready.go`) via the same
  `json::parse_json` + `root["error"]` body-parsing idiom `do_scan`
  already used for its own error responses: green "Bridge: OK" on `200`
  (profiles loaded and sane-runtime reachable), blue "Scanner: offline"
  on `503 {"error":"sane_runtime_unreachable"}` (the bridge process
  itself answered, only the scanner backend is down), and red
  "Bridge: ERR" for every other not-ready status, keeping the existing
  red "Bridge: --" (network-level failure) and "Bridge: not set" (no
  Bridge URL configured) states. Colors are applied via
  `lvgl.label.update`'s `text_color` on the existing
  `bridge_status_label`; the separate `status_led`/scan-in-progress
  spinner (B5) are untouched — they remain exclusive to `do_scan`'s scan
  feedback. Not yet verified against real hardware — see the firmware
  README's "Hardware verification status".

### Documentation

- Documentation site at
  [scan-bridge.strausmann.de](https://scan-bridge.strausmann.de):
  Zensical configuration, the English content tree under `docs/en/`
  (getting started, architecture, hardware, operations, blog), a German
  placeholder under `docs/de/`, the blog front-matter template, and a
  real build-and-deploy pipeline in `.github/workflows/docs.yml`
  replacing the previous stub. Zensical has neither native i18n nor a
  blog plugin yet, so English and German are two separate builds and
  the blog index is hand-maintained; both workarounds are tracked in
  [#13](https://github.com/strausmann/paperless-scan-bridge/issues/13).
- Corrected the Kodak ScanMate i1120 capability claims in
  `HARDWARE_COMPATIBILITY.md`: the Start button and the ADF paper
  sensor are **not** detectable through the `avision` SANE backend.
  Only the indicator button (positions 1–9) and NVRAM values can be
  read. The originally planned "insert paper → scan starts
  automatically" flow is therefore not achievable via SANE, and
  `scanbd` was dropped from the Phase 1.2 design. Empirically verified
  on the reference hardware on 2026-04-30; evidence and method in
  `docs/research/scanner-hardware-events.md`.
- OpenAPI 3.1 spec for `scan-bridge`
  (`components/scan-bridge/api/openapi.yaml`), grounded against the
  handlers actually implemented in `internal/api/` rather than the
  aspirational surface `CONTAINER_SUITE.md` §4.4 used to reference,
  rendered as an interactive [API
  reference](https://scan-bridge.strausmann.de/en/api-reference/) page
  via a self-hosted [Scalar](https://github.com/scalar/scalar)
  bundle (`.github/scripts/vendor-scalar.sh`, same
  pinned-and-digest-verified vendoring pattern as Mermaid and ESP Web
  Tools — no request to `proxy.scalar.com` or `fonts.scalar.com`).
  Issue #9, phase F1.

### Changed

- CYD scan-control panel firmware (`firmware/esp32-panel/`) is now
  **secret-free**: Wi-Fi credentials, the bridge URL and the bearer
  token are no longer build-time `!secret` values. Wi-Fi is provisioned
  in-browser via Improv right after flashing; the bridge URL and token
  are now `text` entities held in flash (NVS) and set from the panel's
  own `web_server` dashboard at its IP. This is what makes the ESP Web
  Tools installer above possible — a public binary cannot carry a
  per-device secret. **Breaking for existing v1 flashes:** re-flashing
  with this firmware clears the old build-time Wi-Fi/bridge
  configuration; the two `text` entities need to be set again from the
  dashboard afterwards. `secrets.yaml.example` was removed — nothing in
  the shipped config uses `!secret` anymore.

### Fixed

- Scan profiles now reject a `source` the scanner does not offer, at
  daemon startup instead of on the first scan. `validateProfile` only
  checked that `source` was non-empty, so an unusable value passed
  validation and surfaced later as a `400 invalid_request` from
  `sane-runtime`, on the caller. Two shipped profiles were affected:
  `private-simplex` and `receipts` set `source: "ADF"`, which the
  reference Kodak ScanMate i1120 does not advertise — `scanimage -A`
  reports exactly `ADF Front|ADF Duplex` — so neither profile could
  ever have scanned. Accepted values are `ADF Front`, `ADF Duplex` and
  `Flatbed`. Found while smoke-testing `sane-runtime` against the real
  device.

- CYD scan-control panel firmware: the status LED and on-screen status
  label now reset back to idle after every scan outcome, not just a
  successful one. Previously, only the `200` branch reset the display —
  an error (`422`/`401`/`403`/`404`/other) or a network failure left the
  red/amber LED and its message on screen indefinitely, until the next
  scan attempt overwrote it.

### Security

- CYD scan-control panel firmware: going secret-free also means the
  ESPHome native API (`api:`) and OTA updates no longer have a
  build-time encryption key / password — a public binary cannot embed
  either as a per-device secret, and there is currently no on-device way
  to set one before the first flash. Both are now LAN-trust, the same
  posture v1 already accepted for plain-HTTP bridge communication and
  unauthenticated Improv provisioning. Documented in the firmware
  README's new "Security model" section, including what to change if
  deploying on a less-trusted network.

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
