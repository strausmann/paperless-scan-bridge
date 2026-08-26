# 0022 — A BLE management surface on the panel requires an authorizer; Improv stays provisioning-only

- **Status:** Proposed
- **Date:** 2026-08-26
- **Deciders:** strausmann
- **Tags:** scan-bridge, api, deploy

## Context

The CYD scan-control panel is configured today through two paths: Improv
(over BLE or serial) for Wi-Fi credentials, and the panel's own ESPHome
`web_server` dashboard — bundled into the firmware, reachable at the panel's
IP — for everything else (Bridge URL, Bridge Token, grid geometry) plus live
status.

That leaves a gap the `/manage/` page makes visible. A panel with no usable
network is not unreachable — the firmware falls back to a `Scan Panel Setup`
SoftAP with a captive portal, and the bundled dashboard is fully usable over
it. But that fallback costs the operator their own network connection: joining
the panel's access point means leaving the Wi-Fi they were on, which on a
phone or a laptop with a single radio is a disruptive, easily-abandoned detour.

Bluetooth is the transport that avoids it, and over Bluetooth the panel speaks
only Improv — **nothing but Wi-Fi credentials and a state code**. So there is
no way to read the panel's status or correct a wrong Bridge URL without either
a working network or a trip through the SoftAP.

Comparable products solve this with a custom GATT service. LTS Design's
Respooler does exactly that: one service, two characteristics — one notifying
status, one accepting commands — driven from a static Web Bluetooth page
(`LukasT03/LTS-Control-Web`). The pattern is proven and small.

It is also implementable here. ESPHome's `esp32_ble_server` supports custom
services and characteristics with `notify`, templatable values and `on_write`
automations, and the BLE stack is **already compiled into this firmware**
because `esp32_improv` pulls it in — so the marginal cost is code, not flash
budget for a new stack.

The blocker is not feasibility. It is that the panel currently runs
`esp32_improv:` with `authorizer: none`, because it has no physical confirm
button. Today that means anyone within radio range during setup can push Wi-Fi
credentials at it — accepted, because the blast radius is a LAN-only panel
joining a network.

A configuration surface changes that blast radius entirely. The panel holds the
**bridge bearer token**: the credential that authorizes `POST /scan`. Exposing
read or write access to it over an unauthenticated BLE service would let anyone
in radio range read the credential that triggers scans, or repoint the panel at
a bridge they control. Unlike the Respooler — where the worst case is a
mis-spooled filament reel — the worst case here is unauthorized access to a
document pipeline.

## Decision

We will **keep the browser-facing BLE surface limited to Improv Wi-Fi
provisioning** until an authorization model for the panel exists. A custom GATT
management service (status + configuration) is **deferred, not rejected**, and
is gated on a superseding ADR that first settles:

1. **How a session is authorized** — the reference board has no physical
   button, but it does have a touchscreen. Three candidates were weighed and
   the operator has indicated a preference for combining the last two:

   - *On-screen PIN* — the panel renders a code the browser must echo back.
     Improv's own `AUTHORIZATION_REQUIRED` state models this directly. Strong,
     but it needs a keypad flow in the browser page and a PIN renderer on a
     display already crowded with the profile grid.
   - **Touch-to-confirm (preferred)** — a BLE session stays unauthorized until
     someone physically taps a prompt on the panel's LVGL UI. Proves physical
     presence, which is the property that actually matters here, and reuses a
     UI the firmware already drives. Maps onto Improv's
     `AUTHORIZATION_REQUIRED` → `AUTHORIZED` transition without inventing a
     protocol.
   - **Pairing window after boot (preferred, as the second factor)** — BLE
     accepts authorization attempts only for a short period after power-on.
     Bounds the exposure window to a moment the operator chose, and gives a
     recovery path for a panel whose touchscreen is unresponsive or whose
     screen cannot be read.

   Combining them means an attacker needs both physical presence *and* the
   post-boot window — and a panel that has been running untouched for days
   presents no attack surface at all. The superseding ADR settles the exact
   window length, what happens when it expires, and whether touch-confirm is
   mandatory inside it or only outside it.
2. **What the surface may expose** — specifically whether the bearer token is
   write-only (settable, never readable) or excluded from BLE entirely.
3. **Whether BLE stays advertising permanently** or only while unprovisioned.

Until that ADR is accepted, status and configuration remain the responsibility
of the panel's own bundled `web_server` dashboard, and the published
`/manage/` page states this limitation explicitly rather than implying a
capability that does not exist.

## Options considered

- **Option A — Improv-only until an authorizer exists (chosen):** ships the
  real, useful capability now (a panel with no network can be put on one from a
  browser), and refuses to widen an unauthenticated radio interface to a
  credential. Costs: configuration still needs Wi-Fi or a cable.
- **Option B — custom GATT service now, `authorizer: none`:** matches the
  Respooler UX immediately, and would let anyone in radio range read or replace
  the bridge bearer token. Rejected: the panel's threat surface is not the
  Respooler's.
- **Option C — custom GATT service now, token excluded, everything else
  exposed:** narrower, but still lets a stranger repoint the panel's Bridge URL
  or read the profile list, and it splits configuration across two UIs with no
  clear rule for which field lives where. Rejected as a half-measure that still
  needs the authorizer discussion.
- **Option D — drop BLE entirely, cable only:** simplest and safest, but throws
  away a capability the firmware already has and makes first setup worse.
  Rejected.

## Consequences

- **Positive:** the shipped BLE surface cannot leak or accept the bridge
  credential. `/manage/` is honest about its scope. No firmware change and no
  new flash cost for the current release.
- **Negative / trade-offs:** configuring a panel still needs a working network
  or a detour through the `Scan Panel Setup` SoftAP — the capability is not
  missing, it is merely inconvenient on a single-radio device. Users who know the Respooler will expect more from a page
  called "Manage" — the page therefore says what it cannot do, and why, in the
  page body rather than in a footnote.
- **Neutral / follow-ups:** when the authorizer ADR lands, `esp32_improv`'s own
  `authorizer:` setting should be revisited in the same change — it is the same
  question for the same radio, and the touch-confirm mechanism sketched above
  would let Improv itself move off `authorizer: none` at the same time. The `/manage/` page's "A fuller Bluetooth
  surface" section is the place to link the outcome.

## References

- `docs/en/manage/index.md` (the page this decision scopes);
  `firmware/esp32-panel/cyd-scan-panel.yaml` (`esp32_improv: authorizer: none`,
  `web_server: local: true`)
- ADR [0006](0006-auth-model.md) (the bearer token this protects),
  [0013](0013-container-hardening-baseline.md) (hardening posture)
- `THREAT_MODEL.md`; firmware `README.md` "Security model"
- [Improv Wi-Fi](https://www.improv-wifi.com/) — protocol scope;
  [improv-wifi/sdk-ble-js](https://github.com/improv-wifi/sdk-ble-js)
- [ESPHome `esp32_ble_server`](https://esphome.io/components/esp32_ble_server/)
  — the component a future custom service would use
- Prior art: [LukasT03/LTS-Control-Web](https://github.com/LukasT03/LTS-Control-Web)
  (one service, two characteristics: notify status + write commands)
