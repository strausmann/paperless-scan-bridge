# Hardware

Any scanner with a working SANE backend should work. "Should" is doing
real work in that sentence — SANE backend quality varies enormously, and
hardware-button support in particular is device-specific and often
absent.

## Compatibility levels

| Level | Meaning |
| --- | --- |
| **A — Reference** | The maintainer owns it and tests against it |
| **B — Verified** | A contributor ran the full verification and reported back |
| **C — Reported working** | Someone says it works; no structured report |
| **D — Likely compatible** | Untested, but the SANE backend suggests it should work |
| **X — Known incompatible** | Documented not to work, with the reason |

## Verified scanners

| Model | USB ID | SANE backend | Level | Notes |
| --- | --- | --- | --- | --- |
| [Kodak ScanMate i1120](kodak-scanmate-i1120.md) | `040a:6013` | `avision` | A | Reference device. ADF + duplex, 75–600 DPI. Hardware-button support is partial. |

The full list, including known-incompatible devices, untested
likely-compatible candidates, trigger devices, and storage backends,
lives in
[`HARDWARE_COMPATIBILITY.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/HARDWARE_COMPATIBILITY.md).

## Trigger hardware

Not a scanner — a companion device. The [CYD scan-control
panel](cyd-scan-panel.md) is a touch screen that lists scan profiles and
triggers a scan over HTTP, flashable straight from a browser.

## Testing your own scanner

The verification procedure has four stages:

1. **SANE detection** — does a one-off SANE container see the device on
   the USB bus?
2. **First scan** — does `scanimage` produce a usable page?
3. **Bridge integration** — does the stack drive it end to end?
4. **Hardware buttons** (optional) — do button events reach the host?

The exact commands for each stage are in section 10 of
`HARDWARE_COMPATIBILITY.md`.

## Contributing a report

Hardware compatibility reports are the most welcome kind of pull
request. To add a device:

1. Add a row to `HARDWARE_COMPATIBILITY.md`
2. Add the udev rule to `deploy/udev/99-paperless-scan-bridge.rules`
3. Add any SANE configuration under `components/sane-runtime/config/`
4. Add model notes as `docs/en/hardware/<vendor>-<model>.md`

Negative results are just as valuable as positive ones — a documented
"this does not work, and here is why" saves the next person the same
investigation.
