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

Once a panel is flashed you do not need this page again — or that is the
plan. Read the box below before you rely on it.

The upload form on the panel's own dashboard at `http://<panel-ip>/`
takes a `.bin` and works today. It keeps the panel's configuration:
Wi-Fi, Bridge URL, Bridge Token and the grid size live in a separate
flash partition that an update does not touch. Only a USB flash from
this page wipes them, because the installer erases the whole chip.

### Download the firmware

**[:material-download: cyd-scan-panel.ota.bin](/firmware/cyd-scan-panel.ota.bin)**
— this is the file the dashboard's upload form wants.

The other one next to it,
[`cyd-scan-panel.factory.bin`](/firmware/cyd-scan-panel.factory.bin), is
the full flash image the button at the top of this page writes over USB.
It **erases the panel's configuration**. Do not put it in the upload
form.

```bash
curl -fsSLO https://scan-bridge.strausmann.de/firmware/cyd-scan-panel.ota.bin
```

Check it against the MD5 the
[manifest](/firmware/manifest.json) publishes for that exact build:

```bash
md5sum cyd-scan-panel.ota.bin
curl -s https://scan-bridge.strausmann.de/firmware/manifest.json | grep md5
```

!!! warning "This path holds one build, and it is overwritten"

    `/firmware/` always carries whatever came off `main` last,
    identified by a commit SHA rather than a version. The next
    documentation deploy replaces it. There is no archive here and no
    way to go back to an earlier build from this URL.

    From the next release onwards the same files are also attached to
    every [GitHub Release][releases], identified by their version tag,
    kept permanently, and with a `SHA256SUMS` beside them:

    ```bash
    sha256sum -c SHA256SUMS --ignore-missing
    ```

    Releases before that carry no firmware assets — those builds no
    longer exist anywhere.

| | [`/firmware/`](/firmware/manifest.json) | [GitHub Release][releases] |
| --- | --- | --- |
| Holds | the newest build | the build that shipped with a tag |
| Named by | commit SHA | `v1.2.3` |
| Kept | until the next deploy | permanently |
| Verify with | MD5 from the manifest | `SHA256SUMS` |
| Available | now | from the next release on |

  [releases]: https://github.com/strausmann/paperless-scan-bridge/releases/latest

!!! warning "Self-update does not work on this hardware yet"

    The panel was built to poll `manifest.json` over HTTPS and report a
    newer build as **Firmware Update** on its dashboard. Measured on the
    reference unit, it has never once succeeded:

    ```text
    E esp-tls-mbedtls: mbedtls_ssl_setup returned -0x7F00
    E http_request.update: Failed to fetch manifest
    ```

    `-0x7F00` is `MBEDTLS_ERR_SSL_ALLOC_FAILED`. The TLS session cannot
    be allocated — the panel already carries Wi-Fi, the Bluetooth stack,
    LVGL and its own dashboard, and mbedTLS wants roughly 32 KB more on
    top. This is a memory ceiling, not a certificate problem: embedding a
    root CA would make it worse, because the failure happens before any
    certificate is examined. The dashboard shows `Firmware Update:
    UNKNOWN` and always has.

    Use the upload form until this changes.

!!! info "What will protect an update, once it works"

    ADR 0024 moves the manifest and the firmware image to `scan-bridge`
    itself, served over plain HTTP on your own network. That removes TLS
    from the update path rather than working around its cost, and it
    drops an internet dependency from a function that otherwise needs
    nothing outside your LAN.

    The integrity guarantee is unchanged: the manifest carries the
    firmware's MD5 and the panel verifies it **while writing**, so a
    truncated or altered download is discarded and the running firmware
    survives. An interrupted update cannot brick the panel.

    The residual risk moves with the source. Someone who can rewrite
    traffic on your LAN can serve both a forged manifest and a matching
    binary, and the MD5 check would pass — the same exposure the previous
    design accepted on the public internet path, now limited to your own
    network. This is why the panel **reports** updates but never installs
    one by itself: the window that matters is the moment you press
    install, not every six hours. The full reasoning is in ADR 0024 in
    the repository.

## Requirements

| | |
| --- | --- |
| **Browser** | Chrome or Edge, desktop. Web Serial is not available in Firefox, Safari, or on iOS. |
| **Cable** | A USB **data** cable — charge-only cables give you no serial port. |
| **Board** | ESP32-2432S028R ("Cheap Yellow Display") |
