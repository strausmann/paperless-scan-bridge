# Scan-system vision — epic breakdown and triage

- **Date:** 2026-08-13 (updated same day — see the "Update 2026-08-13" note
  below)
- **Status:** Operator reviewed; ADRs 0016–0020 drafted (`Proposed`,
  pending merge) for the items decided in this update. Three sub-questions
  remain genuinely open (see below); everything else is either **[Ready to
  dev]** or **[Decided (ready after ADR)]**.
- **Purpose:** capture the full scan-system vision the operator described in one
  sitting, break it into epics and features, and triage every feature into
  **[Ready to dev]** (extends something that already exists, no open decision),
  **[Decided (ready after ADR)]** (the operator made the call in the
  2026-08-13 update below; an ADR now records it), or **[Needs
  clarification]** (a real decision or unknown is still open). This
  document is the detailed companion to the phase bullets added to
  [`ROADMAP.md`](../../ROADMAP.md) — it does not replace the roadmap, it is
  the reasoning behind the bullets.
- **How to read the tags:** **[Ready to dev]** means an implementer can start
  from the cited ADR/schema/task without asking the operator anything first.
  **[Decided (ready after ADR)]** points at the ADR that now records the
  decision — an implementer starts from that ADR. **[Needs clarification]**
  (only three sub-questions remain, see below) lists the exact open
  questions inline.
- **Update 2026-08-13 (same day):** the operator reviewed this triage and
  decided the destination-routing, document-assembly/taxonomy,
  profile-storage/ordering, scanner-power-control, and Home-Assistant/MQTT
  questions. Five ADRs (0016–0020) were drafted to record them; every
  affected epic below is updated in place, the original open-question text
  is replaced by the decision plus any sub-question the ADR itself left
  open. The **only three sub-questions still genuinely open** across the
  whole document are: (1) fileee account/auth specifics (A1/A7), (2) the
  MQTT broker credential source (D1), and (3) whether ADR 0004's Synology
  archival stays mandatory-always or becomes purely per-profile (A1,
  explicit operator confirmation needed).
- **Not created yet:** no GitHub issues exist for any of this. The proposed
  issue breakdown is in the pull request description, for the operator to
  green-light.

## Grounding summary (what already exists, checked against the live repo)

Before the triage, the facts this document leans on, verified against the
current `main`:

- The profile schema (`components/scan-bridge/internal/profiles/profiles.go`,
  also documented in `CONTAINER_SUITE.md` §4.6) has `Source`, `Resolution`,
  `Mode`, `Format` (`pdf`/`jpeg`/`tiff` — **no `png` yet**), `TargetSubdir`,
  `Deskew`, `RemoveBlank`, `RotatePages`, `PageSize`, `TimeoutSeconds`, and a
  `MetadataTemplate` with `PaperlessTags`/`PaperlessCorrespondent`. There is
  no destination field (Paperless is the only implicit target), no OCR
  toggle, no page-count/page-object-shape field, and no document-type-to-
  action mapping.
- `POST /scan` (ADR 0005/0006) is implemented and synchronous today — it
  dispatches to `sane-runtime` over the Unix socket (ADR 0009) and returns
  the finished result inline. `GET /jobs`, `GET /jobs/{id}`, `POST
  /jobs/{id}/cancel`, and `GET /ready` are wired but return `501` (see
  `components/scan-bridge/internal/api/routes.go`) — the async job store
  (Phase 1.2 Task 8) has not landed.
- `internal/metrics/metrics.go` exports exactly one collector,
  `scan_bridge_build_info`; everything else (`scan_bridge_jobs_total`,
  per-stage duration histograms, queue depth, per-profile usage) is an
  explicit `TODO(phase 1.4)` in that file.
- The Phase 1.2 plan's SQLite migration (`docs/superpowers/plans/
  2026-04-30-phase-1.2-webhook-architecture.md`, Task 3) already sketches a
  `scan_count_total` column on the profile table — but ADR 0010 explicitly
  **defers** moving profiles from YAML to a database until the management UI
  (Phase 1.4) lands, and says so should not be pre-empted. The frequency-
  sortable metric the panel needs is architecturally anticipated, just not
  built.
- The ESP32 panel (issue #9, **closed** — shipped as `firmware/esp32-panel/
  cyd-scan-panel.yaml`, PRs #31/#32) is real, running firmware, not a
  concept. What it does today: fixed **6-button landscape-only grid**
  populated from `GET /profiles` (name + description only, no ordering), a
  `Bridge: OK/ERR` + `WiFi: OK` status pair in the top bar, tap-to-scan via
  synchronous `POST /scan` with LED colour feedback (amber while in flight,
  green/red on result) that resets after a delay. It explicitly ships **no
  `api:` block** (no Home Assistant native-API entity, no fixed encryption
  key — the README calls this out as a deliberate trade-off for a
  publishable, secret-free binary) and **no fixed OTA password** (anyone on
  the LAN can push new firmware; that is accepted for now).
- `CONCEPT.md` §18.4 already has an open question, **Q4**, on exactly the
  "OCR yes/no" axis: "should the bridge do it, or should Paperless? Currently
  Paperless." `ARCHITECTURE.md`'s `scan-processor` responsibilities already
  document an **optional local OCR pass with tesseract, off by default** —
  the toggle mechanism is architecturally anticipated, just not
  profile-configurable yet.
- ADR 0004 (Synology NAS = single source of truth for documents) is written
  entirely in terms of Paperless's consume directory. It says nothing about
  a second destination — a fileee upload path was not on the table when it
  was accepted.
- `homeassistant/blueprints/`, `ha/`, and `monitoring/{prometheus,grafana-
  dashboards}/` exist only as empty `.gitkeep` placeholders — Phase 2/3 HA
  and monitoring work has not started.
- `zensical.toml` sets `docs_dir = "docs/en"`; `docs/roadmap/`, like
  `docs/research/` and `docs/superpowers/`, is deliberately outside that
  directory and is not published — no navigation entry needed for this file.

---

## Epic A — Profile "Baukasten" (destination, OCR, format, page handling, document routing)

The core idea: every profile becomes a free combination of independent
options, instead of the current fixed Paperless-only, single-shape output.

### A1 — Destination: Paperless-ngx or fileee, and which account/target

**[Decided (ready after ADR)]** — ADR
[0016](../decisions/0016-destination-routing-pluggable-interface.md): a
pluggable `Destination` interface + registry in `scan-bridge`/
`scan-processor`. Built-in modules: NFS, SMB, Paperless-ngx (API), fileee
(API), and a generic HTTP-POST destination. A profile picks one or more
destinations and, per profile, whether output goes to intermediate storage
(NFS/SMB) first or straight to an API (SD-wear avoidance, pre-upload OCR).
Multiple destinations per profile are allowed.

Affected: `profiles.go` (new `destinations` field), ADR 0004 (see the open
sub-question below), `internal/output`/`scan-processor` (not yet built).

Remaining open sub-questions (not resolved by ADR 0016):

1. **ADR 0004 interaction — needs operator confirmation:** does Synology
   archival stay mandatory for every profile regardless of chosen
   destinations, or become purely per-profile like any other destination?
   See ADR 0016's "Interaction with ADR 0004" section and ADR 0004's
   2026-08-13 note.
2. **fileee account/auth specifics** — which fileee account an upload
   targets (company/contact ID? static config-level account?) and the exact
   client mechanism (`go-fileee` library vs. `fileee-mcp-server` vs. a new
   direct client) still need to be worked out against fileee's actual API
   before implementation.

### A2 — OCR on/off per profile

**[Ready to dev]**

`ARCHITECTURE.md` already documents an optional local `tesseract` OCR pass
in `scan-processor`, off by default because Paperless does OCR better on the
bigger Docker host. Making that toggle **profile-configurable** (instead of
a single global default) is an additive schema change (`ocr: bool`, default
`false`) plus wiring the existing (not-yet-built, but already-specified)
`scan-processor` OCR pass to read it. Default language pair `deu+eng` is
already implied by the target audience and needs no new decision. Directly
answers `CONCEPT.md` §18.4 Q4 by making it per-profile instead of global.

### A3 — Output format: tiff / jpeg / png / pdf

**[Ready to dev]**

The `Format` enum in `profiles.go` already covers `pdf`/`jpeg`/`tiff`.
Adding `png` is a one-line enum extension plus a `scan-processor` encoder
case once that component exists (Phase 1.3). No open decision — the set the
operator named is a strict superset of what is already modelled.

### A4 — Duplex vs. single-page

**[Ready to dev — already substantially supported]**

The `Source` field already carries `"ADF Front"` vs `"ADF Duplex"` (see the
example profiles in `ARCHITECTURE.md` and `CONTAINER_SUITE.md` §4.6) — this
axis is modelled today via free-text SANE source strings. The only
remaining work is validating/documenting the accepted source values more
strictly (currently any non-empty string passes `validateProfile`) — a
tightening, not a new feature.

### A5 — Feeder behaviour: scan exactly one page vs. drain the whole ADF

**[Ready to dev]**

Distinct from A4 (duplex is about *sides*, this is about *how many sheets
the ADF pulls*). Add a `max_pages` (or `single_sheet: bool`) field to
`ScanParams`-equivalent profile scan settings; `sane-runtime`'s scan loop
(not yet built — Phase 1.2/1.3 territory) stops after N pages instead of
draining the feeder. Additive schema field, no destination/architecture
decision involved.

### A6 — Multi-page result shape: one combined object vs. one object per page

**[Decided (ready after ADR)]** — ADR
[0017](../decisions/0017-document-assembly-and-type-taxonomy.md): a
per-profile page-grouping setting (combined vs. per-page), read by
`scan-processor`'s assembly step.

Remaining open sub-question (not resolved by ADR 0017): fileee's actual
multi-page/multi-object model needs a concrete look before a
destination-specific default is chosen (same fileee-API-shape question as
A1's open sub-question).

### A7 — Document type/kind → target-specific labels and actions

**[Decided (ready after ADR)]** — ADR
[0017](../decisions/0017-document-assembly-and-type-taxonomy.md): a
per-profile document-type field maps, via per-profile config, to
destination-specific labels/tags and actions (Paperless tags/
correspondent, extending the existing `MetadataTemplate`; fileee labels).
The taxonomy and the mapping live in the profile's own config and are
interpreted by the destination module chosen in A1/ADR 0016 — there is no
central enum the core has to understand.

Remaining open sub-question (not resolved by ADR 0017): fileee
account/auth specifics (same as A1) — what fileee's own label/action
mechanism concretely offers still needs to be checked against its API.

---

## Epic B — Panel advanced UX (ESPHome firmware)

The panel already ships (v2, secret-free, 6-button fixed landscape grid).
Every item below is a **v3** addition to that firmware, not a new component.

### B1 — Configurable grid size

**[Ready to dev]**

`cyd-scan-panel.yaml`'s LVGL layout hard-codes 6 slots. Making the slot
count configurable (a `text`/`number` entity persisted like `Bridge URL`/
`Bridge Token` today, or a compile-time substitution) is a firmware-only
change; LVGL supports dynamic grids. No backend change required beyond what
B2 (paging) also needs.

### B2 — Paging buttons when more profiles exist than fit the grid

**[Ready to dev]**

Natural companion to B1. Once the profile count can exceed the visible grid,
prev/next buttons paginate the already-fetched `GET /profiles` list
client-side. No new backend endpoint needed — `GET /profiles` already
returns the full list; the firmware currently just renders the first six.

### B3 — Sorting: alphabetical / manual / by usage frequency / mixed

**[Decided (ready after ADR)]** — ADR
[0018](../decisions/0018-profile-storage-ordering-frequency.md): four
ordering modes (alphabetical, manual, usage-frequency, mixed —
operator-configurable pinned-slot count + frequency-sorted remainder),
computed **bridge-side** (`GET /profiles` returns an already-sorted list,
firmware stays dumb, per ADR 0005). Requires the SQLite persistence layer
ADR 0010 deferred — this ADR is that deferral's resolution; profile scan
parameters stay YAML-authored as ADR 0010 decided.

Remaining open detail (implementation-level, not blocking): the exact
SQLite schema, the mixed-mode default pinned-slot count, and the
migration path from pure-YAML profiles are left to the plan that
implements ADR 0018. "Manual" ordering still needs the Phase 1.4
management UI (issue #9 phase B) to be set conveniently.

### B4 — Display rotation (portrait/landscape)

**[Ready to dev]**

The firmware README already lists this as a known, explicitly scoped gap:
*"No portrait layout... is a later option."* This is already-anticipated
work with a clear target (240×320 portrait vs the current 320×240
landscape), no open decision.

### B5 — Scan-status shown until completion

**[Ready to dev — extends existing behaviour]**

The firmware already shows a per-tap status label and LED colour (amber
in-flight → green/red on result) that **resets after a delay**. Making the
"scanning…" state persist prominently on-screen until the result arrives
(rather than a brief flash) is a firmware UI refinement of code that
already exists and already threads state through `POST /scan`'s response.

### B6 — Chain-status indicator (green/red/blue, top bar)

**[Ready to dev, once `/ready` lands — currently blocked by an existing,
already-planned dependency]**

The firmware already renders `Bridge: OK/ERR` (from `GET /health`) and
`WiFi: OK/--`. A single colour-coded indicator for "everything in the chain
reachable" maps naturally onto `GET /ready`, which is already wired in
`routes.go` (currently `501`, waiting on the dispatch subsystem per its own
in-code comment) and already documented in `CONTAINER_SUITE.md` §4.4 as
"returns 200 OK only if SANE container is reachable and at least one
profile is loaded." No new backend design needed — this is "finish the
already-planned `/ready` endpoint, then point the firmware at it." The
**blue = scanner offline** state needs `/ready`'s response body to
distinguish *which* link failed (bridge up but sane-runtime down vs.
bridge itself down) — that response shape is a small, uncontroversial
addition when `/ready` is implemented.

---

## Epic C — Scanner power management

### C1 — Power the scanner on for a job via a Zigbee2MQTT-compatible smart plug

**[Decided (ready after ADR)]** — ADR
[0019](../decisions/0019-scanner-power-control-pluggable-interface.md): a
pluggable `PowerControl` interface + registry **in `scan-bridge`** (not the
panel, not `sane-runtime`). First backends: MQTT (Tasmota-over-MQTT and
Zigbee2MQTT-bridged devices) and Tasmota-HTTP-direct. A webhook backend is
deferred. Turn-on happens on scan trigger.

Remaining open detail (implementation-level, not blocking): specific
smart-plug models are a hardware-compatibility-matrix concern, not an
architecture question; whether `POST /scan` blocks on scanner warm-up or a
separate "wake" step is required is left to the implementing plan.

### C2 — Auto-off after configurable idle time

**[Decided (ready after ADR)]** — ADR
[0019](../decisions/0019-scanner-power-control-pluggable-interface.md): the
idle-off timer (configurable duration) lives in `scan-bridge`, started
after each completed scan.

Remaining open detail: the idle-timeout default value, and whether it is
global or per-profile, are left to the implementing plan.

---

## Epic D — Metrics and Home Assistant

### D1 — Scan-count database and additional metrics, readable in Home Assistant

**[Decided (ready after ADR)]** — ADR
[0020](../decisions/0020-mqtt-home-assistant-integration.md): `scan-bridge`
publishes metrics and status to the existing homelab MQTT broker using
Home Assistant MQTT discovery — no Prometheus integration for this surface
(the existing `internal/metrics` Prometheus endpoint is unaffected and
stays for other consumers, e.g. Grafana).

Remaining open sub-question — **needs operator input:** which MQTT broker
host/port/credential source to use is not decided by ADR 0020 (see the
References section of that ADR); which specific metrics beyond scan-count
matter still needs a concrete list.

### D2 — Smarthome status: update availability, version, connection status

**[Decided (ready after ADR)]** — ADR
[0020](../decisions/0020-mqtt-home-assistant-integration.md): resolved by
keeping `scan-bridge` as the single HA-facing component. The panel's
no-`api:` stance is untouched — it never talks to HA directly, it reports
to `scan-bridge` (as it already does via `GET /health`/`GET /profiles`/
`POST /scan`), and `scan-bridge` relays whatever is HA-relevant onward via
MQTT. "Connection status" means `scan-bridge`'s own connectivity to its
downstream dependencies and to the panel — not the panel talking to HA.

---

## Epic E — Firmware / OTA

### E1 — On-screen "update available" indicator with tap-to-update

**[Decided — operator note, not a full ADR]** OTA works via the existing
**ESP Web Tools manifest** (`firmware/manifest.json`, already used for the
initial browser-based flash) **plus GitHub Releases**: a new release
publishes an updated manifest/binary, and the panel's "is a new version
available" check is a plain-HTTP fetch/compare against the published
manifest — consistent with the firmware's no-`api:`, secret-free design
(no ESPHome API server, no Home Assistant integration needed).

Remaining open detail (implementation-level): whether tapping the
indicator triggers a self-OTA pull or just surfaces the notification (with
flashing still via the browser installer) is firmware work for whoever
implements this, not an architecture decision.

### E2 — Automatic updates with a scheduled window (e.g. nightly at 4am)

**[Ready to dev, once E1's source-of-truth question is answered]**

ESPHome natively supports scheduled automations (`time:` + `interval:`
components) and the OTA mechanism to actually flash already exists in the
current firmware. Once E1 resolves *how* the panel learns a new version
exists, wiring that check into a nightly cron-like trigger is
straightforward ESPHome YAML — no new architectural question of its own.

---

## Epic F — Documentation (Scalar)

### F1 — OpenAPI spec + Scalar rendering for scan-bridge

**[Ready to dev]**

`CONTAINER_SUITE.md` §4.4 already references `components/scan-bridge/api/
openapi.yaml` as "the full OpenAPI 3.1 spec" — that file does not exist yet
(aspirational reference). Generating it against the *actually implemented*
routes (`GET /health`, `GET /version`, `GET /profiles`, `GET
/profiles/{name}`, `POST /scan`, plus the `501`-stub surface) and rendering
it with Scalar on the docs site is additive documentation work with a
already-named target location and no open decision.

### F2 — Scalar API docs "for all our systems"

**[Decided — operator note, not a full ADR]** **Scalar per-component**:
each repository/system renders its own Scalar docs against its own OpenAPI
spec (this repository does so for `scan-bridge`, per F1) rather than one
cross-repository aggregation owned by this roadmap. A homelab-wide rollup,
if wanted, is a separate `homelab-management`-level initiative this
roadmap only links to, not builds.

---

## Summary table

| Epic | Feature | Tag |
| ---- | ------- | --- |
| A | A1 destination (Paperless/fileee + account) | Decided — ADR 0016 (2 sub-questions open) |
| A | A2 OCR on/off per profile | Ready to dev |
| A | A3 output format incl. png | Ready to dev |
| A | A4 duplex vs single-page | Ready to dev (largely exists) |
| A | A5 one-page vs drain-ADF feeder behaviour | Ready to dev |
| A | A6 multi-page: combined vs per-page object | Decided — ADR 0017 |
| A | A7 document type → labels/actions | Decided — ADR 0017 |
| B | B1 configurable grid size | Ready to dev |
| B | B2 paging buttons | Ready to dev |
| B | B3 sorting (alpha/manual/frequency/mixed) | Decided — ADR 0018 |
| B | B4 display rotation | Ready to dev |
| B | B5 scan-status shown until done | Ready to dev |
| B | B6 chain-status indicator | Ready to dev (blocked on `/ready`, already planned) |
| C | C1 smart-plug power-on for a job | Decided — ADR 0019 |
| C | C2 idle auto-off | Decided — ADR 0019 |
| D | D1 scan-count DB + metrics in HA | Decided — ADR 0020 (broker creds source open) |
| D | D2 smarthome status (update/version/connection) | Decided — ADR 0020 |
| E | E1 update-available indicator | Decided — operator note (OTA via manifest + Releases) |
| E | E2 scheduled auto-update window | Ready to dev (E1 now resolved) |
| F | F1 Scalar docs for scan-bridge | Ready to dev |
| F | F2 Scalar docs for "all systems" | Decided — operator note (Scalar per-component) |

## New ADRs (drafted 2026-08-13, status `Proposed` — pending review/merge)

The five ADRs the operator green-lit in the same 2026-08-13 sitting that
produced this triage. Each is `docs/decisions/NNNN-*.md`; see the pull
request description for the full rationale:

- **ADR [0016](../decisions/0016-destination-routing-pluggable-interface.md)**
  (was N1, feature A1): pluggable `Destination` interface + registry (NFS,
  SMB, Paperless-ngx, fileee, generic HTTP-POST); interacts with ADR 0004
  (Synology mandatory-vs-per-profile question left open for the operator).
- **ADR [0017](../decisions/0017-document-assembly-and-type-taxonomy.md)**
  (was N2, features A6+A7): per-profile page-grouping (combined vs.
  per-page) and per-profile document-type → destination-specific
  labels/actions, interpreted by the destination module.
- **ADR [0018](../decisions/0018-profile-storage-ordering-frequency.md)**
  (was N3, feature B3): fulfills ADR 0010's deferred follow-up — display
  metadata + bridge-side ordering (alphabetical/manual/frequency/mixed) +
  SQLite-backed usage-frequency tracking.
- **ADR [0019](../decisions/0019-scanner-power-control-pluggable-interface.md)**
  (was N4, features C1+C2): pluggable `PowerControl` interface + registry
  in `scan-bridge` (MQTT + Tasmota-HTTP-direct first), idle-off timer in
  the bridge.
- **ADR [0020](../decisions/0020-mqtt-home-assistant-integration.md)**
  (was N5, features D1+D2): `scan-bridge` publishes metrics/status to the
  homelab MQTT broker via HA discovery, resolving the no-`api:` firmware
  tension by keeping HA integration entirely in the bridge.

## Other operator decisions (2026-08-13, recorded here — not full ADRs)

Smaller decisions from the same sitting that resolve open triage items
without warranting a standalone ADR (no architectural trade-off with real
alternatives to record — these are direct choices):

- **OTA source of truth (E1):** the published **ESP Web Tools manifest**
  plus **GitHub Releases** — see the updated E1 entry above.
- **Scalar docs scope (F2):** **per-component** — see the updated F2 entry
  above.
- **Zensical docs, unified across the ecosystem:** this repository already
  uses Zensical (Phase 0, `AGENTS.md`'s technology-choices table); the
  operator's direction is for other homelab OSS repositories to follow the
  same choice over time, for a consistent docs experience across projects.
  No action item for *this* repository beyond staying on Zensical.
- **Docs-site homepage:** the Zensical docs homepage for this project is
  an "impeccable" landing page (crisp, focused first impression) rather
  than a bare docs-index redirect — a Phase 0/1 documentation-polish item,
  not an architecture decision.
- **Compose `pull_policy` convention:** compose files follow the
  rolling-vs-pinned `pull_policy` convention (rolling image tags get
  `pull_policy: always`; pinned tags get no `pull_policy`, i.e. Docker's
  `missing` default). Today every image this project builds is pinned per
  ADR [0011](../decisions/0011-no-latest-pinned-versions.md), so this is
  currently a no-op — it stays the binding convention if a rolling-tag
  image is ever adopted (e.g. an upstream image without stable version
  tags).

## References

- [`ROADMAP.md`](../../ROADMAP.md) — phase bullets extended alongside this
  document.
- ADRs: [0004](../decisions/0004-synology-source-of-truth.md),
  [0005](../decisions/0005-trigger-agnostic-scan-endpoint.md),
  [0006](../decisions/0006-auth-model.md),
  [0009](../decisions/0009-bridge-sane-unix-socket.md),
  [0010](../decisions/0010-profiles-declarative-yaml.md),
  [0011](../decisions/0011-no-latest-pinned-versions.md),
  [0015](../decisions/0015-per-profile-token-optional-authz.md),
  [0016](../decisions/0016-destination-routing-pluggable-interface.md),
  [0017](../decisions/0017-document-assembly-and-type-taxonomy.md),
  [0018](../decisions/0018-profile-storage-ordering-frequency.md),
  [0019](../decisions/0019-scanner-power-control-pluggable-interface.md),
  [0020](../decisions/0020-mqtt-home-assistant-integration.md).
- Issues: [#9](https://github.com/strausmann/paperless-scan-bridge/issues/9)
  (panel design, closed/shipped),
  [#7](https://github.com/strausmann/paperless-scan-bridge/issues/7)
  (hardware-event research, open),
  [#15](https://github.com/strausmann/paperless-scan-bridge/issues/15)
  (Pi network/storage constraints, open),
  [#19](https://github.com/strausmann/paperless-scan-bridge/issues/19)
  (Phase 1.2 reconciliation, open, active).
- `firmware/esp32-panel/cyd-scan-panel.yaml` and its `README.md`.
- `CONCEPT.md` §18.4 (open questions Q4/Q5).
