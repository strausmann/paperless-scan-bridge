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
  — the `.bin` for the dashboard's upload form, which is the working
  update path while self-update is broken.

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
