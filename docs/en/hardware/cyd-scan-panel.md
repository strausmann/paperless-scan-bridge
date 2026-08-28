# CYD scan-control panel

A wall-/desk-mountable touch panel that lists the bridge's configured
scan profiles and triggers a scan with a tap — the hardware-independent
companion to the [Kodak ScanMate i1120](kodak-scanmate-i1120.md)'s
partial hardware-button support. See
[Issue #9](https://github.com/strausmann/paperless-scan-bridge/issues/9)
for the full design and
[`firmware/esp32-panel/README.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/README.md)
for everything below in more detail, including the security trade-offs
this firmware makes to be distributable as one public binary.

| | |
| --- | --- |
| Board | ESP32-2432S028R ("Cheap Yellow Display" / CYD) |
| Display | 2.8" 320x240 ILI9341 TFT, resistive XPT2046 touch |
| Firmware | ESPHome, secret-free — see below |
| Hardware verification | Not yet flashed to real hardware (see README "Hardware verification status") |

## Install and manage

Flashing and Bluetooth setup have their own pages, linked from the top
of the site:

- **[Install panel](../install/index.md)** — flash the firmware straight
  from the browser over USB (Chrome/Edge, Web Serial).
- **[Manage panel](../manage/index.md)** — get a panel onto Wi-Fi over
  Bluetooth (Chrome/Edge, Web Bluetooth).
- **[Download the firmware](../install/index.md#download-the-firmware)**
  — the `.bin` for the dashboard's upload form, kept as the recovery
  path. The normal update path is now automatic: the panel gets its
  firmware from your bridge (see below).

This page is the hardware reference behind those: what the panel is,
how it behaves once configured, and what it still cannot do.

## Setup, step by step

1. **Install** — plug the CYD in over USB and flash it from the
   [Install panel](../install/index.md) page, picking the serial port
   when Chrome/Edge asks.
2. **Wi-Fi (Improv)** — right after flashing, the installer walks you
   through [Improv Wi-Fi](https://www.improv-wifi.com/) provisioning in
   the same browser tab: pick your network, enter the password. You can
   also do this later over Bluetooth from the
   [Manage panel](../manage/index.md) page. If the panel can't join any
   network, it falls back to a `Scan Panel Setup` hotspot (password
   `panelsetup`) with a captive portal.
3. **Open the panel's own dashboard** — once it has an IP (check your
   router, or the ESP Web Tools log), open `http://<panel-ip>/` in a
   browser on the same network.
4. **Set Bridge URL and Bridge Token** — on that dashboard, set the
   scan-bridge's address (the host and port you publish `scan-bridge`
   on, e.g. `http://<your-bridge-host>:18080`) and its bearer token —
   the plaintext whose SHA-256 digest is in your `auth.token_hash`,
   kept in your password manager, not in this repository. Both persist
   across reboots; nothing here needs a re-flash. Once set, the top bar's
   Bridge indicator turns **green "Bridge: OK"** as soon as the bridge
   answers `GET /ready` with `200` — profiles loaded and the scanner
   backend reachable. **Blue "Scanner: offline"** means the bridge
   itself answered but the scanner backend specifically did not; **red**
   covers every other not-ready or unreachable case (wrong URL, bridge
   down, misconfigured). See the firmware README's "Scope and known
   limitations" for the full state table.
5. **Grid size (optional)** — the same dashboard has **Grid Rows** and
   **Grid Cols** (1–3 each, default 2x3 — today's fixed 6-button
   layout, unchanged unless you opt in). Raise either to show more
   profiles at once, up to 3x3 = 9. If the bridge has more profiles
   than fit on one page, the footer's `<`/`>` buttons page through the
   rest.
6. **Touch calibration** — the one step the browser installer can't do.
   Every physical panel is different; see the README's "Touch
   calibration" section (needs a local `esphome` install and a re-flash,
   over USB or OTA).

## Known limitations

!!! warning "Scans longer than 55 seconds cannot be started from the panel"

    `http_request` is synchronous, so `POST /scan` holds the panel's main
    loop for the whole scan, and a loop that does not return within the
    task watchdog window reboots the device. ESPHome caps that watchdog
    at 60 seconds, so the panel's client timeout sits at 55 — under it,
    deliberately, so the client gives up first and the panel survives to
    report an error instead of being killed mid-request.

    A longer scan reports **Bridge unreachable** on the panel. The scan
    itself still completes: the bridge already has the request and does
    not care that the caller left. Three of the four shipped profiles
    allow 180, 300 and 600 seconds and are out of reach from the panel
    for now.

    The fix is the `/jobs` endpoints (Phase 1.4) — fire the scan, poll
    for the result, never hold the loop. ESPHome's `http_request` has no
    async mode to use instead.

## Updates come from the bridge

Once the panel knows its **Bridge URL** it checks that bridge for newer
firmware and reports one as **Firmware Update** on its own dashboard. A
**Check for Update** button asks the bridge to look at GitHub at once;
the panel reads the answer at 8 s, 90 s and 660 s. The last one covers
the case where the bridge had to defer its GitHub call — it holds a
five-minute floor between them — and then took its own timeout.

The cadence follows how the last check went: **every minute** while
none has succeeded, **every 30 minutes** once one has, and back to every
minute as soon as one fails. A bridge that goes away is therefore
noticed at the next scheduled check — up to half an hour — and picked up
again a minute or two after returning. With no Bridge URL set it does not
check at all. Each check is one small request to your own bridge, never
to GitHub.

The panel never talks to GitHub. It cannot: with Wi-Fi, the Bluetooth
stack, LVGL and its own dashboard resident there is no memory left to
set up a TLS session (`MBEDTLS_ERR_SSL_ALLOC_FAILED`) — a memory
ceiling, not a certificate problem. So `scan-bridge` asks GitHub every
five hours instead, downloads the release, **verifies it against the
release's own `SHA256SUMS`**, and only then publishes it at
`http://<your-bridge>:18080/firmware/manifest.json`. A file that fails
its checksum is discarded and the bridge keeps serving what it had, so
the manifest never advertises a build the bridge cannot hand over.

Checking is automatic; **installing is still a deliberate click**. The
reasoning is in ADR 0024 and ADR 0025.

No dedicated portrait page layout (the button grid itself resizes for
either orientation, but the header/footer rows are still landscape-only
— see the firmware README's "Display orientation"), no job polling, no
on-device touch-calibration wizard, LVGL memory budget not
hardware-verified, grid size (1x1 up to 3x3 = 9 slots) and paging not
hardware-verified either, and the three-state Bridge indicator's colors
are config-verified only (schema-valid, resolve to the intended hex
values) — not yet confirmed to actually turn blue on the physical panel
when the scanner backend goes down — see the firmware README's "Scope
and known limitations" for the full, current list.
