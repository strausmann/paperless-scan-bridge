# 0025 — The bridge mirrors the panel firmware from GitHub Releases

- **Status:** Proposed
- **Date:** 2026-08-27
- **Deciders:** strausmann
- **Tags:** scan-bridge, deploy
- **Refines:** [0024](0024-panel-updates-from-the-bridge-over-http.md)

## Context

ADR 0024 decided that the panel gets its update manifest and firmware image
from `scan-bridge` over plain HTTP on the LAN, because the ESP32 cannot
allocate a TLS session context alongside Wi-Fi, Bluedroid, LVGL and its own
`web_server`. That ADR deliberately left one question open:

> how the bridge obtains the image (bundled into its container image at build
> time, fetched once and cached, or pointed at a mounted directory) is an
> implementation question, not a decision this ADR settles

This ADR settles it. It does not supersede 0024 — 0024's decision (the panel
polls the bridge; MD5 stays the integrity guarantee) is unchanged.

Three facts constrain the answer.

1. **Baking the firmware into the bridge image couples two release cadences.**
   The panel firmware and the three container images are built by the same CI
   run but are not the same artifact, and a panel fix would then require
   redeploying the whole stack.
2. **A mounted directory makes the operator the update mechanism.** It works,
   and it is exactly the manual `.bin` upload ADR 0023 existed to remove.
3. **The bridge can reach GitHub and the panel cannot.** The bridge has a
   certificate store, ample memory, and — in every supported topology — the
   same internet connection that reaches Paperless-ngx. GitHub's release API
   answers anonymously (verified: `200` for
   `api.github.com/repos/strausmann/paperless-scan-bridge/releases/latest`), and
   the unauthenticated limit of 60 requests per hour per IP is far above what a
   five-hourly poll needs, so the mirror needs no credential.

## Decision

We will **have `scan-bridge` mirror the panel firmware from this repository's
GitHub Releases into a local cache, verify every file against the release's own
`SHA256SUMS`, and only then publish it** on three unauthenticated routes:

| Route | Purpose |
| --- | --- |
| `GET /firmware/{name}` | any file of the mirrored release, `manifest.json` included |
| `POST /firmware/refresh` | queue an immediate check; returns `202` at once |

The bridge polls every **5 hours**; the panel polls the bridge every **6**. The
asymmetry is deliberate: the bridge should always have looked more recently than
the panel asks.

Two ordering rules make this safe, and they are the substance of the decision
rather than implementation detail:

1. **The manifest is swapped only after every file is downloaded and its
   checksum verified.** Publishing a new version before its binary is on disk
   would make the panel offer an update whose download then 404s, or runs past
   the panel's 55-second client timeout while the bridge pulls ~1.7 MB. A
   failed refresh leaves the previously mirrored release serving unchanged.
2. **`POST /firmware/refresh` never blocks.** The panel reaches it through
   ESPHome's `http_request`, which is synchronous on the device's main loop; a
   handler that waited for a GitHub round trip would hold that loop past the
   60-second task watchdog and reboot the panel on the button press. The route
   queues the work and returns immediately.

The routes carry **no bearer token**. The panel must be able to update its way
out of a broken configuration before an operator has entered one, and the bytes
are a public release asset anybody can fetch from GitHub with no credential —
there is nothing here a token would protect.

ADR 0024's follow-up constraint is satisfied structurally: the manifest is
mirrored **verbatim** from the release. CI already asserts that the manifest's
MD5 describes the `.bin` shipped beside it, and `SHA256SUMS` covers both, so the
pair the bridge serves is the pair CI verified. The bridge never computes or
rewrites a digest.

## Options considered

- **Option A — mirror from GitHub Releases, verified against `SHA256SUMS`
  (chosen):** decouples the two release cadences, needs no credential, and
  keeps the bridge's new responsibility to "fetch, verify, serve". The internet
  dependency it introduces is not on a core function: if GitHub is unreachable
  the bridge keeps serving the release it already has, and scanning is
  unaffected.
- **Option B — bake the firmware into the `scan-bridge` image:** simplest to
  reason about and works offline, but ties a panel-only fix to a full stack
  redeploy, and grows the image by ~3.5 MB for something most requests never
  touch.
- **Option C — a mounted host directory the operator fills:** no new code, but
  reinstates the manual step ADR 0023 removed, and gives the operator a way to
  serve an unverified image by accident.
- **Option D — proxy GitHub per request rather than caching:** no cache to
  manage, but every panel check becomes a public round trip, a GitHub outage
  becomes a panel outage, and the response cannot be verified before the panel
  starts writing it.

## Consequences

- **Positive:** the panel's update path works end to end without either device
  needing TLS to a public host. The bridge verifies checksums the panel cannot,
  so the mirror is strictly stronger than the panel fetching for itself.
- **Positive:** a firmware release reaches every panel on the LAN within
  eleven hours unattended, or on a button press.
- **Negative / trade-offs:** `scan-bridge` now makes an outbound call to
  `api.github.com` every five hours. Deployments that must not talk to the
  public internet set `firmware.enabled = false`, and the three routes then
  answer the project's uniform `501` envelope.
- **Negative:** one more piece of persistent state. The cache lives under
  `paths.state_dir`, deliberately **not** on the tmpfs the scan scratch uses —
  otherwise every reboot re-downloads ~1.7 MB and the panel gets `503` in the
  meantime.
- **Neutral / follow-ups:** the panel's `update:` `source:` is now a placeholder
  (`http://bridge.invalid/…`, RFC 2606) overwritten at runtime from the Bridge
  URL entity. A panel with no Bridge URL set therefore reports an update check
  that fails DNS immediately, which is the intended, legible failure.

## References

- Issue [#111](https://github.com/strausmann/paperless-scan-bridge/issues/111);
  PR [#100](https://github.com/strausmann/paperless-scan-bridge/pull/100)
  (attaches the firmware and `SHA256SUMS` to every release — without it there is
  nothing to mirror)
- ADR [0024](0024-panel-updates-from-the-bridge-over-http.md) (refined here),
  [0023](0023-panel-self-update-manifest.md) (superseded by 0024),
  [0011](0011-no-latest-pinned-versions.md), [0006](0006-auth-model.md)
- `components/scan-bridge/internal/firmware/firmware.go`,
  `components/scan-bridge/internal/api/firmware.go`
- `firmware/esp32-panel/cyd-scan-panel.yaml` (`update:`, `button:`,
  `apply_update_source`, `check_for_update`)
- ESPHome `http_request_update.cpp`: the manifest check runs on its own
  FreeRTOS task (`xTaskCreate`), and a relative `ota.path` is resolved against
  the manifest's own URL — which is why the mirrored manifest needs no
  rewriting
