# Scan-system vision — epic breakdown and triage

- **Date:** 2026-08-13
- **Status:** Draft, awaiting operator review
- **Purpose:** capture the full scan-system vision the operator described in one
  sitting, break it into epics and features, and triage every feature into
  **[Ready to dev]** (extends something that already exists, no open decision)
  or **[Needs clarification]** (a real decision or unknown is still open). This
  document is the detailed companion to the phase bullets added to
  [`ROADMAP.md`](../../ROADMAP.md) — it does not replace the roadmap, it is
  the reasoning behind the bullets.
- **How to read the tags:** **[Ready to dev]** means an implementer can start
  from the cited ADR/schema/task without asking the operator anything first.
  **[Needs clarification]** always lists the exact open questions inline —
  those are what we take back to the operator before an issue is created for
  that feature.
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

**[Needs clarification]**

Add a `destination` (or per-output `target`) concept to the profile schema so
a profile can upload to Paperless-ngx **or** fileee, and to a *specific*
account/target within that system.

Affected: `profiles.go` (new field), ADR 0004 (Synology-as-SoT is written
Paperless-only), `internal/output` (not yet built — currently only a stub
processor exists per `ARCHITECTURE.md`'s `scan-processor` description),
homelab context: `fileee-server` + `go-fileee` + fileee-MCP (internal,
unofficial `my.fileee.com` web-app API).

Open questions:

1. Does a fileee upload still land on the Synology first (keeping ADR 0004's
   "Synology is the canonical store" intact, fileee becomes a second
   consumer of the same file), or does it bypass Synology entirely (which
   would need ADR 0004 to be revisited or explicitly scoped as
   "Paperless-only")?
2. What is the fileee upload mechanism from this Go codebase — the existing
   `go-fileee` library, a call to the `fileee-mcp-server`, or a new client
   in `scan-processor`/`scan-bridge` directly?
3. Which fileee **account** does an upload target, and how is that
   referenced from a profile (a fileee company/contact ID? a static
   config-level account?) — fileee's internal API model needs to be mapped
   onto the profile schema.
4. Multiple destinations per profile (upload to both Paperless and fileee
   from one scan) — in scope for v1, or Paperless-XOR-fileee only?

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

**[Needs clarification]**

Affected: profile schema (new `page_grouping` or similar field), `scan-
processor` PDF/image-assembly logic (not built yet), and the destination
chosen in A1 — Paperless and fileee likely have different natural
semantics for "one document" (Paperless: one PDF with N pages is normal;
fileee's document model needs checking).

Open questions:

1. Is this a **per-profile** setting (as vision item A describes) or could
   it also need to be a per-scan override (thinking of Δ-10 in the Phase 1.2
   plan, the per-profile "override allow-list" pattern already used for
   other fields)?
2. What is fileee's actual multi-page/multi-object model — does uploading
   five separate single-page objects there behave sensibly (five documents,
   or fragments of one that need manual re-joining)? This needs a look at
   the fileee API before committing to a default.
3. Default per destination: same default for Paperless and fileee, or
   destination-specific defaults?

### A7 — Document type/kind → target-specific labels and actions

**[Needs clarification]**

E.g. "Eingangsrechnung" (incoming invoice), "Post" (mail), "Verträge"
(contracts) should trigger type-specific labels/tags **and actions** in
fileee and/or Paperless.

Affected: `MetadataTemplate` (currently only `PaperlessTags` +
`PaperlessCorrespondent` — Paperless-only, no "action" concept at all), A1's
destination choice, ADR 0015 (per-profile token / authorization model is
unrelated but the metadata-template shape it references is the same
struct).

Open questions:

1. What is the fixed **taxonomy** of document types/kinds the operator
   wants (the vision names three examples — is that the full v1 list, or
   are more expected)?
2. What does "**Aktionen auslösen**" (trigger actions) concretely mean in
   fileee — moving to a folder, applying a workflow, notifying a contact?
   Same question for Paperless beyond tagging (workflows? correspondent
   assignment? storage path templates, which Paperless already supports
   natively?).
3. Is the type chosen at profile-definition time (one profile = one
   document type, which the current one-profile-per-button panel model
   already implies) or can a single profile branch by a runtime signal
   (e.g. a barcode/QR read, which `CONCEPT.md`'s splitting fields already
   gesture at for a different purpose)?

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

**[Needs clarification]**

Affected: ADR 0010's explicit deferred decision ("issue #9 contemplates
moving to a DB + new fields `display_order`/`display_enabled`/`color`/
`label`... to be decided then"), the Phase 1.2 plan's already-sketched
`scan_count_total` column, and the open question from ADR 0005's linked
firmware-stays-dumb principle (`GET /profiles` should return "already
sorted... so the firmware stays dumb", per issue #9's original API
planning table).

Open questions:

1. **Where does sorting happen** — bridge-side (`GET /profiles` returns
   pre-sorted, firmware stays dumb, matching the existing design principle)
   or panel-side (firmware fetches raw + applies its own sort, which
   contradicts that principle but keeps the bridge simpler)? The existing
   documented intent favours bridge-side.
2. For the "**mixed**" mode (static pinned slots + frequency-sorted rest):
   how many static slots, and is the count configurable per deployment or
   fixed at two (matching the operator's own example)?
3. How is "usage frequency" computed — raw lifetime count (the
   `scan_count_total` column already sketched), a decaying/windowed count
   (last 30 days?), and does a tie-break rule matter?
4. Does "manual" ordering require the not-yet-built profile-management UI
   (Phase 1.4, drag-and-drop was explicitly scoped there in issue #9 phase
   B), or is a simpler config-file-order acceptable for v1?

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

**[Needs clarification]**

Affected: no existing code or ADR touches power control at all — this is a
new integration surface. Homelab context: Zigbee2MQTT + MQTT + Home
Assistant already exist in the HomeLab (HA-MCP is available); ESPHome (the
panel's own firmware stack) also has native Home Assistant integration.

Open questions:

1. **Control path**: through Home Assistant (bridge/panel calls an HA
   service), directly against the MQTT broker/Zigbee2MQTT topic, or via the
   smart plug's own HTTP API if it is Tasmota-flashed (the vision names both
   WiFi-Tasmota and Zigbee-only options)? Each has different auth/latency/
   failure-mode characteristics.
2. **Which device** owns this responsibility — `scan-bridge` (adds an MQTT/
   HA client dependency to a daemon that currently has none), `sane-runtime`
   (already owns the scanner lifecycle, arguably the natural owner), or an
   out-of-band automation entirely in Home Assistant (triggered by the same
   webhook the panel already calls)?
3. **Specific smart-plug model(s)** to target first (the vision names Nous
   A1/A5 as examples) — affects whether Tasmota-HTTP or Zigbee2MQTT is the
   more direct path.
4. **Startup latency**: the scanner needs time to warm up after power-on —
   does `POST /scan` block until the scanner is ready (readiness polling
   before the SANE call), or is there a separate "wake" step the caller
   (panel/HA) must sequence before scanning?

### C2 — Auto-off after configurable idle time

**[Needs clarification]**

Depends entirely on C1's control-path decision — the idle timer needs
somewhere to live (scan-bridge, since it already knows the last-scan
timestamp per profile once B3's frequency tracking exists; or a standalone
HA automation). No new open question beyond "which owner from C1", but
listed separately because it is a distinct feature with its own
configurability (the idle threshold itself, presumably per-deployment, not
per-profile).

---

## Epic D — Metrics and Home Assistant

### D1 — Scan-count database and additional metrics, readable in Home Assistant

**[Needs clarification]**

Affected: `internal/metrics/metrics.go`'s own `TODO(phase 1.4)` (job
totals, per-stage histograms, queue depth are already named there but not
built), the Phase 1.2 plan's `scan_count_total` column (sketched, not
built, deferred by ADR 0010), and no existing HA-facing exposure path at
all (`homeassistant/blueprints/` and `ha/` are empty).

Open questions:

1. Is "readable in Home Assistant" satisfied by HA's native **Prometheus
   integration** scraping the metrics endpoint (`internal/metrics` already
   exists and is the natural fit — no new component), or does the operator
   want push-based **MQTT discovery** sensors (which would need a new
   client in `scan-bridge`, and interacts with C1's broker choice)?
2. Which specific metrics beyond scan-count matter for "further automatic
   actions" — the vision says "weitere Metriken" without naming them; needs
   a concrete list to design counters/gauges for.

### D2 — Smarthome status: update availability, version, connection status

**[Needs clarification]**

Affected: same exposure-path question as D1, plus a direct tension with the
firmware's own documented design: `cyd-scan-panel.yaml` explicitly ships
**no `api:` block** — the README states this is deliberate (*"a public
binary cannot embed a per-device encryption key, and the panel doesn't need
Home Assistant discovery"*). Exposing panel version/connection/update
status *to* Home Assistant either needs that stance revisited (re-adding
`api:` conflicts with the "one public, secret-free binary" distribution
model — see E1 below) or needs a different channel entirely (the panel
reports status to `scan-bridge`, and `scan-bridge` — not the panel — is
what talks to Home Assistant/MQTT).

Open questions:

1. Does "Verbindungsstatus" (connection status) mean the **panel's**
   connectivity to the bridge (firmware-side), the **bridge's** connectivity
   to `sane-runtime`/Paperless/fileee (daemon-side, closer to D1's metrics
   path), or both?
2. Given the explicit no-`api:` design decision in the firmware, is the
   resolution "scan-bridge is the single source of truth for HA, the panel
   never talks to HA directly" (keeps the firmware's stated security
   posture intact) — this reads as the more consistent answer, but needs
   operator confirmation before it becomes a written decision.

---

## Epic E — Firmware / OTA

### E1 — On-screen "update available" indicator with tap-to-update

**[Needs clarification]**

Affected: `cyd-scan-panel.yaml` already has passwordless OTA support (the
mechanism to *push* an update exists); nothing detects *availability* today.

Open questions:

1. **Source of "new firmware available"**: the ESPHome dashboard/API (needs
   the device to run an ESPHome API server — conflicts with the no-`api:`
   stance from D2), a Home Assistant integration (same conflict), or the
   published **ESP Web Tools manifest.json** on the docs site (the panel
   would need to fetch and compare a version field from a URL over plain
   HTTP — consistent with the existing "no api:" secret-free design, but is
   a new client behaviour that does not exist today)?
2. Does the "button in the display" trigger a **self-OTA pull** (panel
   fetches its own new firmware from the docs-site-hosted manifest/binary)
   or does it just surface the notification, with the actual flash still
   happening via the browser installer? These are very different amounts of
   firmware work.

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

**[Needs clarification]**

Open question: this repository can only speak for its own API surface (F1).
"Alle unsere Systeme" (all our systems) implies a cross-repository/cross-
project documentation effort (`homelab-management`, `fileee-server`, other
Go services) — is that in scope for this repository's roadmap at all, or a
separate `homelab-management`-level initiative that this roadmap should
only link to once it exists?

---

## Summary table

| Epic | Feature | Tag |
| ---- | ------- | --- |
| A | A1 destination (Paperless/fileee + account) | Needs clarification |
| A | A2 OCR on/off per profile | Ready to dev |
| A | A3 output format incl. png | Ready to dev |
| A | A4 duplex vs single-page | Ready to dev (largely exists) |
| A | A5 one-page vs drain-ADF feeder behaviour | Ready to dev |
| A | A6 multi-page: combined vs per-page object | Needs clarification |
| A | A7 document type → labels/actions | Needs clarification |
| B | B1 configurable grid size | Ready to dev |
| B | B2 paging buttons | Ready to dev |
| B | B3 sorting (alpha/manual/frequency/mixed) | Needs clarification |
| B | B4 display rotation | Ready to dev |
| B | B5 scan-status shown until done | Ready to dev |
| B | B6 chain-status indicator | Ready to dev (blocked on `/ready`, already planned) |
| C | C1 smart-plug power-on for a job | Needs clarification |
| C | C2 idle auto-off | Needs clarification |
| D | D1 scan-count DB + metrics in HA | Needs clarification |
| D | D2 smarthome status (update/version/connection) | Needs clarification |
| E | E1 update-available indicator | Needs clarification |
| E | E2 scheduled auto-update window | Ready to dev (once E1 resolved) |
| F | F1 Scalar docs for scan-bridge | Ready to dev |
| F | F2 Scalar docs for "all systems" | Needs clarification |

## Proposed new ADRs (not written — proposals for operator sign-off)

See the pull request description for the full rationale; listed here for
completeness alongside the features that need them:

- **N1 — Destination routing & ADR 0004 interaction** (A1): does a fileee
  upload count as a second consumer of the Synology-canonical file, or does
  it need ADR 0004 rescoped to "Paperless-only, fileee is out of that
  ADR's claim"?
- **N2 — Document assembly semantics** (A6): multi-page-object vs.
  per-page-object, defaults per destination.
- **N3 — Profile storage migration + ordering/frequency model** (B3):
  supersedes/fulfils ADR 0010's explicitly deferred follow-up now that
  issue #9's UI groundwork question is answerable.
- **N4 — Scanner power control path** (C1/C2): HA-mediated vs. direct
  MQTT/Zigbee2MQTT vs. Tasmota-HTTP, and which component owns it.
- **N5 — Home Assistant/MQTT integration surface for scan-bridge** (D1/D2):
  reconciles the firmware's explicit no-`api:` stance with wanting
  smarthome-visible status, by keeping HA-facing integration in
  `scan-bridge` rather than the panel.

## References

- [`ROADMAP.md`](../../ROADMAP.md) — phase bullets extended alongside this
  document.
- ADRs: [0004](../decisions/0004-synology-source-of-truth.md),
  [0005](../decisions/0005-trigger-agnostic-scan-endpoint.md),
  [0006](../decisions/0006-auth-model.md),
  [0009](../decisions/0009-bridge-sane-unix-socket.md),
  [0010](../decisions/0010-profiles-declarative-yaml.md),
  [0015](../decisions/0015-per-profile-token-optional-authz.md).
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
