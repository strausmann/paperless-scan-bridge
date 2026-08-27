# 0024 — The panel updates from the bridge over plain HTTP; MD5 stays the integrity guarantee

- **Status:** Proposed
- **Date:** 2026-08-27
- **Deciders:** strausmann
- **Supersedes:** [0023](0023-panel-self-update-manifest.md)

## Context

ADR 0023 decided that the panel self-updates by polling the `manifest.json`
published next to the documentation site on GitHub Pages, with the MD5 in that
manifest as the integrity guarantee and TLS certificate verification off.

**It does not work on this hardware.** Measured on the reference panel:

```
E esp-tls-mbedtls: mbedtls_ssl_setup returned -0x7F00
E esp-tls: create_ssl_handle failed
E http_request.update: Failed to fetch manifest from
    https://scan-bridge.strausmann.de/firmware/manifest.json
```

`-0x7F00` is `MBEDTLS_ERR_SSL_ALLOC_FAILED` (`mbedtls/ssl.h`). The TLS session
context cannot be allocated. The panel carries Wi-Fi, the Bluedroid BLE stack
that `esp32_improv` pulls in, LVGL and the bundled `web_server` at once, and
mbedTLS wants roughly another 32 KB for its record buffers on top. The dashboard
shows `Firmware Update: UNKNOWN` and has never once seen a build.

Three things follow from *where* it fails.

1. **A root certificate would not help, and would hurt.** `mbedtls_ssl_setup`
   runs before any certificate is examined. Embedding a CA costs additional
   memory at the exact point that is already exhausted. This is also why the
   failure happens with `verify_ssl: false` already set — ADR 0023's open
   question about whether verification actually works here is moot: no session
   is established to verify anything in.
2. **Plain HTTP from GitHub Pages is not available.** `Enforce HTTPS` is on and
   Pages answers `301 Moved Permanently` on port 80. The current source cannot
   be reached without TLS.
3. **TLS was never authenticating anything here.** `verify_ssl: false` is set
   for the plain-HTTP LAN calls to the bridge, and it governs the update
   download too. What TLS bought was resistance to passive observation of a
   public firmware image — not authentication.

There is a precedent worth reading rather than re-deriving. Tasmota solves the
same constraint on the same class of chip by leaving TLS out of the update path
entirely: its documentation states plainly that for OTA *"https is not
supported"*, and its official server serves firmware over port 80
(`http://ota.tasmota.com/tasmota32/release/tasmota32.bin` → `200 OK`). On these
chips, memory is the scarce resource, and integrity is cheaper to guarantee with
a digest than with a transport.

Finally, the current design puts an **internet dependency on a core function**.
The panel is useless without the bridge, which is on the LAN; the only reason it
needs to reach the public internet at all is to ask whether a firmware build
exists.

## Decision

We will **serve the update manifest and the firmware image from `scan-bridge`
over plain HTTP on the LAN**, and the panel will poll that instead of GitHub
Pages.

The integrity guarantee is **unchanged from ADR 0023**: the manifest carries the
image's MD5, ESPHome verifies it while writing, and a mismatch discards the
download and leaves the running firmware intact. Only the *source* changes.

Also unchanged from ADR 0023, and re-affirmed here:

- **Checking is automatic; installing stays a deliberate click.** No automation
  installs firmware on its own.
- **CI keeps publishing the manifest and both images to the docs site.** That is
  what the browser installer at `/install/` uses for first-time USB flashing,
  and it stays the public source of truth for what a release contains. The
  bridge mirrors it; it does not replace it.

## Options considered

- **Option A — manifest and image from the bridge over HTTP (chosen):** removes
  TLS from the update path, so the memory ceiling that broke ADR 0023 stops
  being a factor rather than being worked around. Removes an internet dependency
  from a core function, which is what `CLAUDE.md`'s "no cloud dependencies for
  core functionality" asks for. Matches the shape upstream projects on this
  hardware converge on. Costs: the bridge gains a small responsibility, and the
  panel can only discover updates while the bridge is reachable — which is the
  same condition under which it can do anything at all.
- **Option B — shrink the TLS buffers (`CONFIG_MBEDTLS_SSL_IN_CONTENT_LEN`
  16384 → 4096):** one line, no ADR, and it might work. Rejected as the primary
  fix: it keeps the internet dependency, it leaves the panel one large TLS
  record away from failing again, and it treats a symptom of a design that puts
  TLS on a chip that cannot afford it. Kept in reserve if Option A ever needs to
  reach a non-LAN source.
- **Option C — free heap by dropping BLE:** would buy the most memory, and costs
  Improv provisioning, which is the only cable-free way to put a panel on Wi-Fi.
  Rejected: it trades a working feature for one that is not.
- **Option D — keep manual `.bin` upload only:** no new work and no new
  exposure, but re-introduces exactly the surprise ADR 0023 existed to remove.
- **Option E — embed a root CA:** does not address the failure, which occurs
  before certificate handling and is caused by memory pressure that a CA bundle
  increases. Rejected on the evidence above.

## Consequences

- **Positive:** the update path works. It has no internet dependency, no
  certificate expiry to outlive, and no TLS memory ceiling. The panel's two
  network peers reduce to one — the bridge it already trusts with the bearer
  token.
- **Negative / trade-offs:** the manifest and image travel unencrypted over the
  LAN. Someone who can rewrite traffic on that LAN can serve a manifest and a
  matching image and install arbitrary firmware at the moment an operator
  chooses to update. This is the **same residual risk ADR 0023 accepted**,
  relocated from the public internet path to the local network — a smaller
  population of potential attackers, not a larger one. It remains bounded by the
  manual-install constraint, and both language versions of the published
  `/install/` page are updated in the same change — they now state that
  self-update does not work today, why, and what replaces it.
- **Negative:** `scan-bridge` gains a responsibility that is not scanning. Kept
  as small as possible: serve two files, do not build them, do not decide when
  to update.
- **Neutral / follow-ups:** how the bridge obtains the image (bundled into its
  container image at build time, fetched once and cached, or pointed at a
  mounted directory) is an implementation question, not a decision this ADR
  settles. Whichever is chosen, the MD5 the bridge publishes must be computed
  from the file it will actually serve — publishing a manifest whose digest does
  not match its own binary makes every update fail closed, which is safe but
  silently broken.

## References

- Panel log, 2026-08-27 (`mbedtls_ssl_setup returned -0x7F00`);
  `mbedtls/ssl.h`: `#define MBEDTLS_ERR_SSL_ALLOC_FAILED -0x7F00`
- `firmware/esp32-panel/cyd-scan-panel.yaml` (`update:`, `ota:`,
  `http_request: verify_ssl: false`)
- ADR [0023](0023-panel-self-update-manifest.md) (superseded),
  [0006](0006-auth-model.md) (the bearer token the panel already holds),
  [0022](0022-panel-ble-management-surface.md) (why BLE stays)
- [Tasmota — Upgrading](https://tasmota.github.io/docs/Upgrading/)
  ("https is not supported")
- [ESPHome — OTA Update via HTTP Request](https://esphome.io/components/ota/http_request/)
  (mandatory MD5 verification)
