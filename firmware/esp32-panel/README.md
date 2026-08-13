# CYD scan-control panel firmware (v1)

ESPHome firmware for a wall-/desk-mountable touch panel that lists the
scan-bridge's configured scan profiles and triggers a scan over HTTP. See
[Issue #9](https://github.com/strausmann/paperless-scan-bridge/issues/9)
for the full design (this firmware implements phase D, "Firmware", against
the API shape currently live on the bridge — see "v1 scope and known
limitations" below for what is deferred).

## Board

**ESP32-2432S028R** — the widely-cloned "Cheap Yellow Display" (CYD).
WROOM-32 module, **no PSRAM**, 2.8" 320x240 ILI9341 TFT with a resistive
XPT2046 touch controller, an active-low RGB status LED, and a
GPIO21-driven backlight. This firmware targets that specific board
revision; the pin map in `cyd-scan-panel.yaml` will not match other CYD
variants (e.g. the ESP32-2432S024 or the capacitive-touch ESP32-8048
boards) or generic ESP32 dev boards with a bolted-on display.

## Prerequisites

- [ESPHome](https://esphome.io/) **≥ 2024.5.0** (the `http_request`
  action syntax used here — `http_request.get`/`http_request.post` with
  `on_response`/`on_error` — needs that version or newer). Install with
  `pip install esphome` or use the ESPHome Docker image; see the
  [ESPHome installation docs](https://esphome.io/guides/installing_esphome).
- A USB-A-to-USB-C (or micro-USB, depending on the board revision) cable
  for the first flash.
- Network access from wherever you run `esphome` to the board over USB,
  and later over Wi-Fi for OTA updates.

## Configuration: secrets

Copy the example file and fill in real values:

```bash
cp secrets.yaml.example secrets.yaml
```

`secrets.yaml` is gitignored — it never gets committed.

| Key                  | Value                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------ |
| `wifi_ssid`/`wifi_password` | Your normal Wi-Fi credentials.                                                       |
| `ap_password`         | Password for the panel's fallback SoftAP (shown as `<friendly name> Setup`).             |
| `api_encryption_key`  | Random 32-byte key, base64-encoded, for the ESPHome native API. Generate with `python3 -c "import secrets, base64; print(base64.b64encode(secrets.token_bytes(32)).decode())"`. |
| `ota_password`        | Password for OTA updates after the first flash.                                          |
| `bridge_token`        | Bearer token for the scan-bridge API. **Source:** the Vaultwarden item *"paperless-scan-bridge test token (hhplex01)"*. Ask whoever manages the HomeLab Vaultwarden instance for read access if you don't have it. |

The bridge base URL (`http://hhplex01:18080` by default) is **not** a
secret — it is a `substitutions:` value in `cyd-scan-panel.yaml` itself.
Override it there (or with `esphome run --substitution
bridge_base_url=...`) if your bridge runs somewhere else.

## Flashing

First flash, over USB, from this directory:

```bash
esphome run cyd-scan-panel.yaml
```

This compiles the firmware and flashes it over the serial connection
(pick the USB port when prompted). [ESP Web Tools](https://esphome.github.io/esp-web-tools/)
(browser-based flashing over Web Serial, no local toolchain) is a later
option once a hosted `manifest.json` exists for this firmware — not part
of v1.

After the first flash, subsequent updates can go over the air:

```bash
esphome upload cyd-scan-panel.yaml
```

## First boot: Wi-Fi provisioning

The firmware ships with [Improv Wi-Fi](https://www.improv-wifi.com/)
provisioning over both BLE and the same USB-serial connection used for
flashing (`esp32_improv` + `improv_serial`). If the panel cannot join
`wifi_ssid` from `secrets.yaml`, it also starts a fallback SoftAP named
`<friendly name> Setup` with `ap_password` — connect to that and use the
captive portal to configure Wi-Fi manually.

## Touch calibration (required after first flash)

The XPT2046 calibration values in `cyd-scan-panel.yaml`
(`calibration_x_min`/`_max`, `calibration_y_min`/`_max`) are
**placeholders** — every physical panel needs its own calibration.
After flashing:

1. Watch the logs (`esphome logs cyd-scan-panel.yaml`) while tapping
   each corner of the screen and note the raw touch coordinates ESPHome
   reports.
2. Update the four `calibration_*` values in `cyd-scan-panel.yaml` to
   match, and re-flash.
3. Verify all six button slots respond accurately across the whole
   screen, not just near the calibration points you tested.

There is no on-device calibration wizard in v1 — see "Known limitations"
below.

## Plain HTTP (not TLS)

`cyd-scan-panel.yaml` sets `verify_ssl: false` and talks to
`http://hhplex01:18080` (plain HTTP) by default. This is intentional for
v1: the bridge is only reachable on the LAN today, and the bearer token
therefore only ever travels over the local network, not the public
internet. **If the bridge is ever put behind a TLS-terminating reverse
proxy** (Traefik, a Pangolin resource, etc.), change `bridge_base_url` to
`https://...` and remove `verify_ssl: false` (or point it at the right
CA) so the token is not sent in the clear over an untrusted path.

## v1 scope and known limitations

This is a first, deliberately minimal implementation. What it does:

- Reads the scan profiles from `GET /profiles` on boot and every 30s,
  and fills up to **six** fixed button slots (name as the button label,
  description as a smaller sub-label), hiding any slots beyond the
  number of profiles returned. The bridge currently exposes exactly one
  profile ("default"); more profiles show up automatically, up to six.
- Shows a "Bridge: OK/ERR/--" indicator (from `GET /health`, polled
  every 15s) and a "WiFi: OK/--" indicator in the top bar.
- Tapping a profile button sends `POST /scan {"profile": "<name>"}` with
  the bearer token, disables all six buttons and turns the status LED
  amber while the request is in flight, then shows the result: green
  flash + "Done: `<profile>`" on `200`, "No paper in feeder" (amber-red)
  on `422`, "Unauthorized" on `401`/`403`, "Unknown profile" on `404`,
  a generic "Error `<code>`" otherwise, and "Bridge unreachable" on a
  network-level failure (timeout, DNS, connection refused).

What it deliberately does **not** do yet (see Issue #9, phases beyond
"D — Firmware"):

- **No on-device web config portal.** The bridge URL, bearer token,
  Wi-Fi credentials, and layout are all **build-time** values (Wi-Fi via
  Improv is the one exception — that part is runtime, everything else
  needs a re-flash to change). A runtime settings UI (like BambuHelper)
  is tracked as a follow-up in Issue #9, not part of this firmware.
- **No portrait layout.** Only the 320x240 landscape 2x3 grid described
  in Issue #9's mockups is implemented; portrait (240x320, 1-column) is
  a later option.
- **No job polling / `GET /jobs/{id}`.** The live bridge dispatches
  `POST /scan` synchronously and returns the finished result inline
  (200 OK with the scan outcome, or an error status) — there is no job
  store to poll yet, so this firmware doesn't either. If/when the bridge
  adds async job dispatch, the "scanning..." state here will need to
  switch to polling.
- **No richer profile fields.** Issue #9 envisions
  `display_order`/`display_enabled`/`color`/`label` on each profile, once
  the container-side profile-management UI (phases A-C) lands. The
  currently deployed `GET /profiles` only returns `name` and
  `description`, so that's all this firmware reads; profile order is
  whatever the bridge returns (append-order today, no client-side
  sorting).
- **Improv provisioning has no physical confirmation step**
  (`authorizer: none`). Anyone with local BLE or serial access to the
  panel during a Wi-Fi provisioning window can set its network. Fine for
  a LAN-only panel in a private home; revisit if that assumption ever
  changes.
- **LVGL buffer sizing has not been hardware-verified.** The board has
  no PSRAM, and the LVGL/display defaults used here (no explicit
  `lvgl: buffer_size:` override) have not been confirmed against real
  RAM headroom on physical hardware — if `esphome run` reports a memory
  allocation failure, tuning the buffer size is the first thing to try.

## Hardware verification status

**Not yet flashed to real hardware.** The pin map, SPI bus split,
display/touch options, and backlight/LED wiring in `cyd-scan-panel.yaml`
are cross-verified against multiple cited working ESP32-2432S028R
ESPHome configurations, and (where `esphome` is available) the config
passes `esphome config` (schema/substitution lint — see the PR that
introduced this firmware for the exact command and output). None of that
is a substitute for flashing it to an actual board. Please report back
(open a follow-up issue referencing #9) once you've flashed it — in
particular the touch calibration values, whether the LVGL memory budget
is fine as-is, and whether all six button slots render and respond
correctly.
