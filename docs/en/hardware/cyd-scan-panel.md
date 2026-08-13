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

## Install from the browser

<esp-web-install-button manifest="/firmware/manifest.json">
  <span slot="activate">Install CYD Scan Panel firmware</span>
  <span slot="unsupported">
    Your browser doesn't support Web Serial. Use Chrome or Edge instead.
  </span>
  <span slot="not-allowed">
    Installing needs a secure context (HTTPS or localhost) — this page
    should already be one; if you see this, something's off.
  </span>
</esp-web-install-button>

<script type="module" src="/javascripts/esp-web-tools/install-button.js"></script>

Requires **Chrome or Edge** on desktop (Web Serial). Firefox and Safari
are not supported by the underlying [Web Serial
API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Serial_API),
not by anything on this page. The button above flashes the same
`cyd-scan-panel.factory.bin` that CI compiles from
[`firmware/esp32-panel/cyd-scan-panel.yaml`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/cyd-scan-panel.yaml)
on every push to `main` — see the "Secret-free firmware" section of the
README for why a single public binary works here at all.

## Setup, step by step

1. **Install** — plug the CYD in over USB, click the button above, pick
   the serial port when Chrome/Edge asks.
2. **Wi-Fi (Improv)** — right after flashing, the installer walks you
   through [Improv Wi-Fi](https://www.improv-wifi.com/) provisioning in
   the same browser tab: pick your network, enter the password. If the
   panel can't join any network, it falls back to a `Scan Panel Setup`
   hotspot (password `panelsetup`) with a captive portal.
3. **Open the panel's own dashboard** — once it has an IP (check your
   router, or the ESP Web Tools log), open `http://<panel-ip>/` in a
   browser on the same network.
4. **Set Bridge URL and Bridge Token** — on that dashboard, set the
   scan-bridge's address (defaults to this project's own
   `http://hhplex01:18080`) and its bearer token (**source:** the
   Vaultwarden item *"paperless-scan-bridge test token (hhplex01)"* for
   this project's own deployment — ask whoever manages your HomeLab
   Vaultwarden for access, or use your own bridge's token). Both persist
   across reboots; nothing here needs a re-flash.
5. **Touch calibration** — the one step the browser installer can't do.
   Every physical panel is different; see the README's "Touch
   calibration" section (needs a local `esphome` install and a re-flash,
   over USB or OTA).

## Known limitations

No portrait layout, no job polling, no on-device touch-calibration
wizard, LVGL memory budget not hardware-verified — see the firmware
README's "Scope and known limitations" for the full, current list.
