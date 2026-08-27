# Install the scan panel

Flash the CYD scan-control panel firmware straight from this page — no
toolchain, no downloads, no command line. The binary is compiled by CI
from [`firmware/esp32-panel/cyd-scan-panel.yaml`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/cyd-scan-panel.yaml)
and published with this site, so it always matches the site you are
reading. The build runs with the documentation pipeline rather than on
every commit — the version string in the installer tells you which
commit you are about to flash.

## 1. Flash

Plug the panel into this computer over USB, then:

<esp-web-install-button manifest="/firmware/manifest.json">
  <button class="md-button md-button--primary" slot="activate">
    Install CYD Scan Panel firmware
  </button>
  <span slot="unsupported">
    This browser can't flash. Web Serial needs Chrome or Edge on a
    desktop — Firefox and Safari do not implement it, and neither does
    any browser on iOS.
  </span>
  <span slot="not-allowed">
    Flashing needs a secure context (HTTPS or localhost). This page is
    served over HTTPS, so if you see this, something is off.
  </span>
</esp-web-install-button>

Pick the serial port when the browser asks. The installer erases the
chip and writes the factory image, then offers to set up Wi-Fi in the
same tab.

!!! info "Why a single public binary is safe here"

    The firmware carries no Wi-Fi credentials, no bridge URL and no
    bearer token. Everything deployment-specific is set at runtime and
    stored in the panel's flash. That is what makes a browser installer
    possible at all — see the firmware README's "Secret-free firmware".

## 2. Connect it to Wi-Fi

The installer walks you through [Improv Wi-Fi](https://www.improv-wifi.com/)
right after flashing. If you skip it, or the panel later loses its
network, you have two more ways in:

- **[Bluetooth](../manage/index.md)** — the manage page provisions Wi-Fi
  over BLE, no cable needed.
- **Setup hotspot** — with no known network in range the panel opens a
  `Scan Panel Setup` access point (password `panelsetup`) with a captive
  portal.

## 3. Point it at your bridge

Open `http://<panel-ip>/` — the panel serves its own dashboard, bundled
into the firmware, so it works without internet access. Set:

| Setting | Value |
| --- | --- |
| **Bridge URL** | Where you publish `scan-bridge`, e.g. `http://<your-bridge-host>:18080` |
| **Bridge Token** | The plaintext whose SHA-256 digest is your `auth.token_hash` |
| **Grid Rows / Cols** | Optional, 1–3 each (default 2x3) |

Both persist across reboots; none of this needs a re-flash. Once set,
the top bar turns green **"Bridge: OK"** as soon as the bridge answers
`GET /ready` with `200`.

Full walkthrough, the indicator's state table and the known limitations:
[CYD scan-control panel](../hardware/cyd-scan-panel.md).

## Updating later

Once a panel is flashed you do not need this page again. It checks
`manifest.json` — the same file the button above uses — every six hours
and reports a newer build as **Firmware Update** on its own dashboard at
`http://<panel-ip>/`. Installing is one click there; no file picker, no
cable.

The upload form further down that dashboard still works and is the
recovery path when the panel cannot reach the internet.

!!! warning "What protects an over-the-air update, and what does not"

    The manifest carries the firmware's MD5, and the panel verifies it
    **while writing**. A truncated or altered download is discarded and
    the running firmware survives — an interrupted update cannot brick
    the panel.

    What is missing is TLS certificate verification. ESPHome can only
    verify certificates on the ESP-IDF framework, and this firmware is
    built with Arduino, so `verify_ssl` is off. An attacker in an active
    person-in-the-middle position on the network path could serve both a
    forged manifest and a matching binary, and the MD5 check would pass.

    This is why the panel **reports** updates but never installs one on
    its own: the window that matters is the moment you press install,
    not every six hours. The reasoning, and the ESP-IDF migration that
    would close the gap, are in ADR 0023 in the repository.

## Requirements

| | |
| --- | --- |
| **Browser** | Chrome or Edge, desktop. Web Serial is not available in Firefox, Safari, or on iOS. |
| **Cable** | A USB **data** cable — charge-only cables give you no serial port. |
| **Board** | ESP32-2432S028R ("Cheap Yellow Display") |
