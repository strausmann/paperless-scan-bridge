# 0023 — The panel self-updates from a hosted manifest; MD5, not TLS, is the integrity guarantee

- **Status:** Proposed
- **Date:** 2026-08-27
- **Deciders:** strausmann
- **Tags:** deploy, ci, docs

## Context

Updating a flashed panel currently means opening its dashboard and uploading a
`.bin` by hand. That is ESPHome's push-OTA (`ota: platform: esphome`), the only
update path the firmware has. The panel never learns that a newer build exists.

This surprises people, and reasonably so: the project already publishes a
`manifest.json` next to the firmware on its own site. That manifest is an **ESP
Web Tools** manifest — it drives first-time installation over USB from a browser
and is not something a running panel ever reads.

ESPHome has the mechanism the shape of our setup implies: `update: platform:
http_request` polls a JSON manifest over HTTP and exposes an "update available"
entity, and `ota: platform: http_request` performs the download. The manifest it
wants is an ESP Web Tools manifest **extended** with an `ota` block per build
carrying the OTA binary's path and its MD5 — so one file can serve both
purposes.

Two facts decide how safe this is:

1. **MD5 verification is mandatory and enforced on the device.** ESPHome's
   documentation is explicit: if the MD5 in the manifest does not match what is
   computed while writing, the device keeps the original firmware and discards
   the download.
2. **This firmware does not verify TLS certificates today, and whether it
   could is unresolved.** `verify_ssl` is documented as "supported on ESP32
   only; must be explicitly set to false on other platforms" — that exclusion
   is about other *chips* (ESP8266, RP2040), not about the framework, and
   `esphome config` accepts `verify_ssl: true` on this exact configuration.
   What that does **not** establish is runtime behaviour: verification needs a
   CA bundle compiled in, and the panel's own log shows the request going
   through the ESP-IDF HTTP client (`http_request.idf`) even though the
   framework is Arduino. Whether a certificate is actually validated there has
   not been tested against the real device. The firmware currently sets
   `verify_ssl: false`, chosen for the plain-HTTP LAN calls to the bridge, and
   that value also governs the update download.

That combination bounds the risk precisely. An attacker who can tamper with the
firmware download alone achieves nothing — the MD5 from the manifest rejects it.
An attacker who can tamper with **both** the manifest and the binary, on the
same origin, controls both sides of the comparison and can install arbitrary
firmware on the panel.

## Decision

We will **ship self-update against the hosted manifest**, with the MD5 in that
manifest as the integrity guarantee, and **without** TLS certificate
verification, because it is not enabled on this build and enabling it has not
been shown to work here (see the open question below).

Three constraints make that defensible rather than merely convenient:

1. **Checking is automatic; installing is not.** `update: platform:
   http_request` only surfaces availability. Installation stays a deliberate
   action — no automation installs on its own. The window in which a
   person-in-the-middle could matter is therefore the moment an operator
   chooses to update, not every six hours forever.
2. **The manifest is served from GitHub Pages over HTTPS.** Unverified TLS is
   still TLS: it defeats passive observation and casual tampering, and an
   attacker needs an active position on the path. ESPHome's own documentation
   steers projects to GitHub Pages URLs for this component (GitHub *release*
   URLs redirect to long URLs that overflow the client's response buffer), so
   this is the shape upstream expects.
3. **The residual risk is written down where an operator sees it**, on the
   published `/install/` page — not only in this ADR.

## Options considered

- **Option A — self-update with MD5, unverified TLS (chosen):** removes a
  manual, error-prone step, keeps installation deliberate, and states the
  residual risk plainly. Costs: an active attacker on the network path who can
  rewrite both manifest and binary can install firmware.
- **Option B — turn on `verify_ssl: true` and confirm it works:** the way to
  close the gap properly, and possibly cheaper than first assumed —
  configuration validation already accepts it here. Deferred rather than
  rejected, because accepting a value is not the same as enforcing it: this
  needs a test against the real panel (a deliberately wrong certificate must
  cause a failed update, not a successful one). Until that test exists,
  claiming verification would be worse than admitting its absence. Tracked as
  the follow-up below.
- **Option C — keep manual upload only:** no new exposure, but leaves the
  surprise this ADR exists to remove, and keeps every update dependent on
  someone locating the right `.bin`.
- **Option D — self-update with automatic installation:** strictly worse than
  A. It widens the attack window from "when I choose to update" to "always" and
  removes the operator's chance to notice an unexpected update.

## Consequences

- **Positive:** the panel reports available updates on its own dashboard and
  installs them without a file picker. One manifest serves both USB
  installation and OTA. A corrupted or truncated download cannot brick the
  panel — the MD5 check discards it and the running firmware survives.
- **Negative / trade-offs:** an active person-in-the-middle able to rewrite
  both the manifest and the binary can install arbitrary firmware at the moment
  an operator installs an update. This is accepted, documented, and bounded by
  the manual-install constraint above.
- **Neutral / follow-ups:** the open question is whether `verify_ssl: true`
  actually validates on this build. It must be answered by test — pointing the
  panel at a host with an invalid certificate and confirming the update
  **fails** — not by reading configuration output. A superseding ADR records
  the result either way; if verification works, the manual-install constraint
  above can be revisited. The CI must keep the OTA
  binary and its MD5 in step with the factory image — publishing a manifest
  whose MD5 does not match its own binary would make every update fail closed,
  which is safe but silently broken.

## References

- `firmware/esp32-panel/cyd-scan-panel.yaml` (`framework: type: arduino`,
  `http_request: verify_ssl: false`, `ota:`)
- `.github/workflows/esphome-firmware.yml` (manifest generation; the emitted
  manifest is ~490 bytes, which is why `buffer_size_rx` is raised from its
  512-byte default)
- `docs/en/install/index.md` (where the residual risk is stated for operators)
- ADR [0011](0011-no-latest-pinned-versions.md) (pinning posture),
  [0022](0022-panel-ble-management-surface.md) (the panel's other radio-facing
  decision)
- [ESPHome — Managed Updates via HTTP Request](https://esphome.io/components/update/http_request/)
  (manifest schema, GitHub Pages guidance)
- [ESPHome — OTA Update via HTTP Request](https://esphome.io/components/ota/http_request/)
  (mandatory MD5 verification)
- [ESPHome — HTTP Request](https://esphome.io/components/http_request/)
  (`verify_ssl`, `buffer_size_rx`)
