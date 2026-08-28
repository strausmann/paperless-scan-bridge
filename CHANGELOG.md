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

- **The panel now updates itself, from your bridge.** `scan-bridge`
  mirrors the panel firmware from this repository's GitHub Releases,
  verifies every file against the release's own `SHA256SUMS`, and serves
  it on unauthenticated routes: `GET /firmware/manifest.json`,
  `GET /firmware/{generation}/{name}`, `GET /firmware/{name}` and
  `POST /firmware/refresh`. The panel polls its bridge **every minute
  while it has never had a successful check** — the `UNKNOWN` state a
  wrong Bridge URL leaves behind, so correcting it shows a result almost
  at once — and **every 30 minutes** thereafter, plus a **Check for
  Update** button. The bridge asks GitHub every 5 hours; the two
  cadences are independent, because the panel reads the bridge's cache
  and never reaches GitHub itself.

  The detour exists because the panel cannot reach GitHub, or the docs
  site, or anything else over TLS: with Wi-Fi, the Bluetooth stack, LVGL
  and its own dashboard resident, the ESP32 cannot allocate a TLS
  session context (`MBEDTLS_ERR_SSL_ALLOC_FAILED`). Self-update has
  therefore never once worked on this hardware. Now it does, and the
  bridge additionally performs a SHA-256 check the panel could not do
  for itself.

  Two ordering rules make it safe. The manifest is swapped only **after**
  every file is downloaded and its checksum verified — otherwise the
  panel would offer an update whose download 404s or overruns its
  55-second client timeout. And `POST /firmware/refresh` returns `202`
  immediately rather than waiting: the panel reaches it through a
  synchronous `http_request` on its main loop, where a blocking wait is
  a watchdog reboot. See ADR 0024 and the new ADR 0025.

  The manifest points at generation-qualified paths — the release tag
  plus a digest of its checksums — and the previous generation stays
  cached, so an install clicked hours after the check still downloads
  the binary that check's MD5 describes rather than whatever landed
  since. The digest is in the path because `gh release upload
  --clobber` lets one tag carry different binaries at different times. The cache is re-verified against its recorded
  checksums before a refresh skips a download and when it is adopted at
  startup, so a file damaged after it was mirrored is repaired rather
  than served forever behind an unchanged release tag. And because the refresh route is
  unauthenticated, outbound GitHub calls carry a five-minute floor —
  otherwise a caller in a loop could exhaust the anonymous quota and
  stop real updates arriving.

  **Panels already in the field do not get this automatically.** Their
  running firmware still polls the HTTPS manifest that has never worked
  on this hardware, so the first build carrying the new update path has
  to be installed once by hand — the dashboard's upload form or USB.
  After that, updates arrive on their own.

  Off by one setting (`firmware.enabled = false`) for deployments that
  must not talk to the public internet; the routes then answer the
  project's uniform `501` envelope.
- Panel firmware is attached to every GitHub Release —
  `cyd-scan-panel.factory.bin`, `cyd-scan-panel.ota.bin`, `manifest.json`
  and a `SHA256SUMS` to check a download against. Until now the firmware
  existed in exactly one place: the published docs site, holding whatever
  came off `main` last and identified by a commit SHA. No copy of the
  build that shipped with `v1.0.0` or `v1.1.0` existed anywhere, so
  "roll back to the version that worked" had nothing to roll back to.
  Both install pages now carry a **direct download link** to
  `cyd-scan-panel.ota.bin` — the file the dashboard's upload form
  wants — plus the `curl` line, the MD5 check against the manifest, and
  a warning that this path holds one build and is overwritten by the
  next deploy. The CYD hardware pages link straight to it in both
  languages, since that is where someone looking for the panel's
  firmware actually starts.
- Scan scratch space moves to **tmpfs** in the reference stack, so
  scanned pages never reach the host's disk. Every scan writes raw TIFF
  pages, has them read back and deletes them again within the same
  request — on a named volume that is a write-erase cycle per scan for
  data that never needs to survive a reboot, which is the access pattern
  an SD card tolerates worst. Sized by `SCAN_BRIDGE_SCRATCH_SIZE`. The
  documentation also stops presenting a Raspberry Pi as a requirement:
  it is the reference and the cheap way to put a host next to the
  scanner, but any Linux Docker host within USB reach works, and an
  existing one is the better choice when there is one.

- The deployment tooling Phase 1 has been promising since Phase 0 now
  exists: `deploy/bootstrap/install.sh` (Docker, the NFS mount, the udev
  rule — the three host modifications the container-first principle
  permits, and nothing else, all idempotent and with a `--dry-run`),
  `deploy/compose/scan-bridge.yml` (the published Topology B stack,
  pulling pinned GHCR images), `deploy/udev/99-paperless-scan-bridge.rules`,
  a `Tiltfile` for the development loop, and `renovate.json`. `scan-bridge`
  also gains a real `healthcheck` subcommand: the image is distroless, so
  there is no curl for a container healthcheck to run, and the binary
  already in the image is the only thing that can probe `/ready`.
- Scan profiles gain `png` as a fourth output format (roadmap Epic A3)
  and `max_pages` to cap how many sheets one scan pulls through the
  feeder (Epic A5). `png` is lossless where `jpeg` is not — a scanned
  form re-encoded as JPEG carries ringing around every letter — and like
  `jpeg` it holds one page per file, so `page_grouping: combined` with
  several pages is rejected rather than silently truncated. A
  `max_pages` of `0` is the default and drains the ADF, exactly as
  before; `1` is the single-sheet case. There is deliberately **no** separate
  `single_sheet` flag: it would mean the same thing and only create a
  contradiction to resolve. Everything below the profile already
  supported the cap — `sane-runtime` turns it into `scanimage
  --batch-count` — the bridge was simply sending a hardcoded `0`.
- The panel now says which build it is running. Its dashboard header
  read `paperless-scan-bridge CYD scan-control panel (v2, Issue #9,
  secret-free, landscape)` — a description of the design, not of the
  binary — and nothing anywhere exposed the version. The build already
  carried one: CI stamps `project.version` with the short commit SHA it
  also writes into the manifest, and the update platform compares
  against it, but it was never rendered. The header now shows it, two
  entities carry it (**Firmware Version** and, separately, **ESPHome
  Version** — a firmware bug is often an upstream regression, and the
  project version cannot answer which release built the binary), and it
  is logged once at boot so a pasted log excerpt identifies its own
  build.

- Multi-arch container builds via `docker buildx bake` for all three
  components (linux/amd64 + linux/arm64, the reference deployment is a
  Pi 5), pushed to GHCR on `main` and built-and-discarded on pull
  requests. Closes three Phase 1 roadmap items at once. New:
  `docker-bake.hcl`, `.golangci.yml`, `.yamllint.yml`, `.hadolint.yaml`.
- The German site grows from six pages to eleven and finally gets the
  landing page. Until now `/de/` opened on the plain docs template while
  `/en/` had the hardware-chain hero, and Architecture, Storage
  topologies, No third-party requests, Kodak ScanMate i1120 and CYD
  scan-control panel existed in English only. All five are translated and
  the German homepage now uses the **same** `home.html` template — every
  visible string in it is read from the page's own front matter with the
  English text as the fallback, so one template serves both builds rather
  than two copies that drift. Its CSS, JS and fonts are referenced with
  absolute `/en/` paths, the same-origin trade-off `panel-tools.js`
  already makes. The reference pages that change most often — scan
  profiles, profile schema, troubleshooting, API reference — stay English
  on purpose, and the homepage says so.

- The scan panel now logs what it is actually doing. Its log used to be
  dominated by two framework components — `xpt2046` printing a line per
  touch sample and `http_request.idf` one per response header — so a
  whole poll cost two lines saying only `content-length: 19`, and which
  endpoint was called, what came back, and why it failed were nowhere.
  Both are raised to INFO and every request now logs method, URL,
  status, byte count and duration, with truncated response bodies and a
  plain-language reason on each failure. The scan path goes further and
  walks the result: `scan_id`, bridge-side duration, every document with
  filename and page count, every destination with status and `task_id`,
  plus OCR-confidence and warnings — and raises an explicit error when a
  destination delivery failed, which the panel otherwise hides behind a
  green "Done" (the bridge does not treat a delivery failure as a scan
  failure). The bearer token is never logged.

- The scan panel now reports **Reset Reason**, **Uptime**, **Loop Time**
  and three heap sensors on its own dashboard. Reported symptom: under
  heavy tapping the profile buttons vanish, the Wi-Fi and Bridge
  indicators go bad, and everything repairs itself seconds later — which
  is exactly what the panel's boot sequence looks like from outside, but
  a reboot's plausible causes (watchdog, panic, brownout) call for
  opposite fixes and could not be told apart. `reset_reason` separates
  them in one reading, without a cable or a local toolchain. No
  behavior change.

- Add `title_template` to scan profiles: an optional per-profile pattern
  that produces the document title a destination receives, with
  `{profile}`, `{document_type}`, `{scan_id}`, `{date}`, `{time}` and
  `{datetime}` placeholders. Until now nothing ever populated the title
  field, so Paperless-ngx fell back to the uploaded filename — the scan
  ID — and every document arrived named like
  `7cc2ba0a36df384ca12f977b2bc64ddc`. Opt-in: a profile without
  `title_template` sends no title, exactly as before.
- `sane-runtime` now logs one structured line per HTTP request, matching
  `scan-bridge`'s schema, so a scan reads as two corresponding lines
  across the two containers. Previously only failures were logged: a
  successful request produced nothing, and the container's log after a
  completed scan showed only its startup line — leaving no way to tell
  "the request never arrived" (socket or permissions) from "it arrived
  and the scanner is slow". `duration_ms` covers the full multipart
  response, so `POST /scan` reports the real scan time rather than the
  time to the first header.
- The scan panel now checks for firmware updates on its own and reports
  them on its dashboard, instead of requiring someone to locate a `.bin`
  and upload it by hand (ADR 0023). It polls the same `manifest.json`
  the browser installer uses — CI now publishes the OTA image alongside
  the factory image and extends the manifest with the `ota` block
  ESPHome's update platform reads, including the firmware's MD5. The
  panel verifies that MD5 while writing, so an interrupted or altered
  download is discarded and the running firmware survives. Checking is
  automatic; **installing stays a deliberate click** — see the ADR for
  why that constraint matters while TLS certificate verification is off
  on this build.

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

- The first blog post, and `ROADMAP.md` reconciled with the code.
  The roadmap listed `sane-runtime`, `scan-processor` and the
  documentation as open when all three were done, and claimed
  `GET /ready` and `POST /scan` returned `501` when only the `/jobs`
  endpoints do. Phase 1.3 was marked "planned" while everything in it
  marked *Ready to dev* had shipped. `CLAUDE.md`'s repository-status
  section carried the same stale claims and is corrected too.

- The `/manage/` page now states the Bluetooth precondition *above* the
  connect button instead of below it. Improv's BLE service only
  advertises while the panel has no Wi-Fi, so a panel that is already
  connected never appears in the browser's device list — correct, but
  indistinguishable from a broken page if the button comes first: an
  operator clicks, waits out the scan, and reads "no compatible devices
  found" before ever reaching the paragraph explaining why. Content
  unchanged in both languages; only the reading order moved.

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

- Go moves from 1.25 to **1.27**, the current stable release, across all
  five places that declare it: `go.work`, the three `go.mod` files and
  the three Dockerfiles. Two couplings had to move with it and are now
  written down rather than rediscovered — `golangci-lint` parses source
  with the `go/types` of the release it was itself built with, so a
  lagging binary panics with `file requires newer Go version` instead of
  quietly missing checks (bumped to v2.13.1, the release that added
  go1.27 support); and `golang:1.27-alpine3.21` does not exist upstream
  at all, so `scan-bridge`'s Alpine base moves to 3.24. Dependabot now
  covers `gomod` and `docker` for all three components instead of only
  `scan-bridge`, so the next Go release arrives as a pull request.

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

- **Starting a scan from the panel rebooted the panel.** Two numbers
  made it certain: `http_request.timeout` was `8s` while a duplex scan
  takes about twenty seconds, and the ESP-IDF task watchdog fires at
  `5s` with `CONFIG_ESP_TASK_WDT_PANIC` on. The scan POST blocks the
  main loop for its whole duration, so every scan the panel ever started
  panicked the device five seconds in — and because the bridge already
  had the request, the scanner went on scanning while the panel that
  asked for it rebooted. That is exactly the reported "the paper was
  pulled and the panel crashed". `watchdog_timeout` is now at the highest
  value ESPHome permits (`60s` — it rejects more) and the HTTP timeout
  `55s`, which has to stay under it. **A scan that takes longer than 55
  seconds therefore cannot be started from the panel**: it reports
  "Bridge unreachable" while the scan itself completes, because the
  bridge already has the request. Three of the four shipped profiles
  allow more (180 / 300 / 600 seconds) and are out of reach from the
  panel until the `/jobs` endpoints land.
- The profile grid no longer stays empty for up to five minutes after a
  boot. `on_boot` refreshes once, and if that comes to nothing — the
  bridge still starting, or the Bridge Token not yet restored from flash
  — the only retry was the 300-second interval. A 15-second retry now
  runs while the grid is empty and stops as soon as it is not.

- CI now actually builds, lints and tests the Go code. Every job in
  `ci.yml` was `echo "placeholder"`, and the `Makefile`'s `test-go`,
  `lint`, `test-shell`, `test-yaml` and `test-docker` targets printed
  `TODO Phase 1` — so three modules and 26 test files were never checked
  anywhere, while every pull request reported "Lint: pass / Test: pass /
  Build: pass". The linter's first run found two real defects: the
  deferred `Close` on the files that receive a scanned page and an
  assembled document was unchecked, so a failed final flush would have
  written a truncated PDF and reported success for it. Both are checked
  explicitly now. The first real test run caught a second problem the
  suite had been carrying silently: `scan-processor`'s OCR fixtures
  wrote an executable script and ran it immediately, which fails with
  `text file busy` whenever a concurrent test forks inside the window
  where the write descriptor is still open (golang/go#22315). The
  fixtures now wait until the script is genuinely executable.

- A long scan-profile name no longer draws outside its button on the
  panel. The name label had neither a width nor a `long_mode`, and an
  LVGL label with no width grows to fit its text — so a long name spilled
  past both edges and across its neighbours, which reads as the name
  repeating over itself. Both labels are now bounded to the button and
  ellipsize the overflow. The width is a percentage rather than the fixed
  96px the description label used, so it follows the configurable grid
  (1–3 columns) on its own.

- The scan panel no longer starts a scan when a finger merely travels
  across the display. Its buttons used `on_press`, which ESPHome maps to
  LVGL's `LV_EVENT_PRESSED` — fired the instant a finger lands on a
  widget, with no release required and again each time a still-pressed
  finger re-enters it. Dragging across the profile grid therefore fired
  a scan under every button crossed. All eleven handlers now use
  `on_click` (`LV_EVENT_CLICKED`: pressed *and* released on the same
  widget). This matters more than the usual button nitpick — a scan
  pulls a sheet through the feeder, runs about twenty seconds and
  uploads a document, so it has to come from a deliberate tap.

- Scan panel: setting Bridge URL and Bridge Token from the dashboard did
  nothing visible until the next poll or a reboot. A freshly flashed
  panel boots with both empty, so `on_boot` skips loading profiles and
  checking the bridge — and the two entities had no `on_value` handler
  to re-run those once configured. The grid stayed empty and the bridge
  indicator stayed grey, which reads as "I configured it and it does not
  work". Both entities now re-check on change.
- Scan panel: the touchscreen was inverted on both axes, so a tap on a
  profile button landed on the opposite corner and did nothing — the
  physical top-left corner reads raw `[3820, 3820]`, which the previous
  calibration mapped to `(316, 237)`, four pixels from the *bottom*-right.
  Both axes are now mirrored via `transform`, and the calibration
  endpoints are measured on the reference unit instead of the original
  placeholders: all four corners were read, so `swap_xy` is confirmed
  rather than assumed (`raw_x` is high at the top, `raw_y` high at the
  left). The endpoints are the per-edge means of those readings, not
  their extremes — each edge is measured twice and the two reads differ
  by up to 93 counts, so averaging splits that error instead of loading
  it onto one corner. The four corners now map to (0,0), (320,2), (2,240)
  and (316,237). Still per-unit: resistive panels vary, so re-measure for
  a new board — the firmware README has the procedure.
- Scan panel: the status line stayed on `idle` for the whole of a scan
  instead of showing `Scanning: <profile>...`, and the progress spinner
  never appeared. Both were set correctly — but `lvgl.label.update` only
  writes into LVGL's object tree, and the pixels are pushed in the
  component loop, which the synchronous scan request then held for the
  next twenty seconds. `do_scan` now yields briefly after updating the
  UI and before starting the request, so the render happens first.
- Scan panel: tapping a profile often did nothing. `http_request` is
  synchronous, so every status poll blocked the main loop and LVGL
  processed no input while it ran — a panel on a slow link logged
  `interval took a long time for an operation (1091 ms)`, i.e. over a
  second of dead touchscreen per poll. At the previous 15s/30s intervals
  the panel was unresponsive several percent of the time. `/ready` now
  polls every 60s and `/profiles` every 300s; neither needed to be that
  eager. The firmware README gains a procedure for telling a dropped tap
  apart from a miscalibrated one, since both look identical from the
  outside.

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
