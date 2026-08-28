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

!!! info "Updates come from your bridge, not from this site"

    Once the panel knows its **Bridge URL**, it checks that bridge for
    newer firmware and reports one as **Firmware Update** on its own
    dashboard. There is also a **Check for Update** button: it asks the
    bridge to look at GitHub at once, and the panel then reads the
    answer three times, so it does not have to wait for the next
    scheduled check.

    Usually the result is there within about a minute and a half. It can
    take up to **eleven minutes**: the bridge keeps five minutes between
    two GitHub calls, so a press shortly after another check has to wait
    that out before the bridge even looks, and the look itself may take
    another five. **Pressing the button again restarts that sequence
    rather than speeding it up** — if nothing has appeared, waiting is
    faster than pressing.

    How often it checks depends on how the last check went. While none
    has succeeded — a wrong Bridge URL, a bridge that is not up — it
    asks every **minute**, so correcting the setting shows a result
    almost at once. Once one succeeds it settles to every **30
    minutes**, and it goes back to every minute as soon as a check
    fails. Note what that does and does not promise: a bridge that goes
    away is noticed at the next scheduled check, so up to half an hour
    later — but from that moment the panel retries every minute, so it
    finds the bridge again a minute or two after it returns. With no
    Bridge URL set at all it does not check. Each check is one small
    request to your own bridge; it never reaches GitHub.

    The detour through the bridge is not a preference. The panel cannot
    reach this site, or GitHub, or anything else over HTTPS:

    ```text
    E esp-tls-mbedtls: mbedtls_ssl_setup returned -0x7F00
    E http_request.update: Failed to fetch manifest
    ```

    `-0x7F00` is `MBEDTLS_ERR_SSL_ALLOC_FAILED`. The TLS session cannot
    be allocated — the panel already carries Wi-Fi, the Bluetooth stack,
    LVGL and its own dashboard, and mbedTLS wants roughly 32 KB more on
    top. This is a memory ceiling, not a certificate problem: embedding a
    root CA would make it worse, because the failure happens before any
    certificate is examined.

    So `scan-bridge` does that part. It asks GitHub for the latest
    release every five hours, downloads the firmware, **checks it against
    the release's own `SHA256SUMS`**, and only then offers it at
    `http://<your-bridge>:18080/firmware/manifest.json`. A file that
    fails its checksum is discarded and the bridge keeps serving the
    release it already had. The manifest never names a build the bridge
    cannot hand over.

    Nothing is required of you afterwards: the mirror is on by default.
    If your bridge must not talk to the public internet, set
    `firmware.enabled = false` under `[firmware]` in `config.toml` and
    use the upload form instead.

!!! warning "An existing panel needs one manual update first"

    This only starts working from the firmware that introduced it. A
    panel already in the field is still running a build that polls the
    HTTPS manifest on this site — the fetch that has never once
    succeeded on this hardware — so it will **never** pick the new
    version up on its own.

    Install it once by hand:

    1. [Download `cyd-scan-panel.ota.bin`](#download-the-firmware) from
       the release.
    2. Open the panel's own dashboard at `http://<panel-ip>/` and use
       the **OTA Update** upload form. (Or re-flash over USB from this
       page — either works.)
    3. Set the **Bridge URL** if it is not already set.

    From then on the panel finds updates by itself.

!!! info "What protects an update"

    The integrity guarantee is the manifest's MD5, and the panel verifies
    it **while writing**. A truncated or altered download is discarded and
    the running firmware survives — an interrupted update cannot brick the
    panel. Between GitHub and your bridge, the SHA-256 checksums add a
    second, stronger check the panel is not able to do itself.

    Between your bridge and the panel the traffic is plain HTTP on your
    own network. Someone who can rewrite traffic on that network can
    serve both a forged manifest and a matching binary, and the MD5 check
    would pass. That is why the panel **reports** updates but never
    installs one by itself: the window that matters is the moment you
    press install, not on every poll. The full reasoning is in ADR 0024
    and ADR 0025 in the repository.

## Requirements

| | |
| --- | --- |
| **Browser** | Chrome or Edge, desktop. Web Serial is not available in Firefox, Safari, or on iOS. |
| **Cable** | A USB **data** cable — charge-only cables give you no serial port. |
| **Board** | ESP32-2432S028R ("Cheap Yellow Display") |
