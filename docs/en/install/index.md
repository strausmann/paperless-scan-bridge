# Install the scan panel

Flash the CYD scan-control panel firmware straight from this page — no
toolchain, no downloads, no command line. The binary is compiled by CI
from [`firmware/esp32-panel/cyd-scan-panel.yaml`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/cyd-scan-panel.yaml)
on every push to `main` and served from this site.

## 1. Flash

Plug the panel into this computer over USB, then:

<esp-web-install-button manifest="/firmware/manifest.json">
  <span slot="activate">Install CYD Scan Panel firmware</span>
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

<script type="module" src="/javascripts/esp-web-tools/install-button.js"></script>

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

## Requirements

| | |
| --- | --- |
| **Browser** | Chrome or Edge, desktop. Web Serial is not available in Firefox, Safari, or on iOS. |
| **Cable** | A USB **data** cable — charge-only cables give you no serial port. |
| **Board** | ESP32-2432S028R ("Cheap Yellow Display") |
