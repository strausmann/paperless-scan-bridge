# Kodak ScanMate i1120

The reference device for this project: a 2009-era desk scanner with no
vendor Linux support, driven through the `avision` SANE backend.

| | |
| --- | --- |
| USB ID | `040a:6013` |
| SANE backend | `avision` |
| Compatibility level | A — Reference |
| Verified | 2026-04, re-verified 2026-04-30 |
| Sources | Flatbed, ADF simplex, `ADF Duplex` |
| Resolution | 75–600 DPI |
| Speed (vendor spec) | 20 ppm simplex, 10 ppm duplex over USB 2.0 |

## Backend status

The `avision` backend has been marked unmaintained upstream since 2020,
but it works reliably for this device on any modern Linux distribution
with kernel 5.15 or newer. The 2026-04-30 verification run used Ubuntu
Server 24.04 with the 6.8 kernel series; the result is not
kernel-specific.

A USB 3.0 host port does not make the scanner faster — the device itself
is USB 2.0.

## Hardware buttons: partial support

This is the single most important thing to know about this device, and
it directly shaped the project's architecture.

### What works

The **LCD function-indicator wheel** (the 1–9 selector) generates SANE
events on the read-only `--message` option, as strings of the form
`<n>:button1`. These are reliably captured by scanbd's stock
`function_knob` filter at 250 ms polling, verified across all nine
positions.

### What does not work

The hardware **Start button generates no SANE-visible event**. Not
through the `avision` backend's options, not through direct enumeration
with `scanimage -A`, not through scanbd polling. During a 60-second
scanbd capture session, repeated Start-button presses produced **zero**
events while indicator-wheel turns produced 21.

The **ADF paper sensor is equally opaque**. Inserting or removing paper
changes neither the `scanimage -A` output nor the `--message` field —
verified by snapshot diff and live scanbd polling. The only
paper-related signal a caller can rely on is `SANE_STATUS_NO_DOCS`,
returned as an error during an actual scan attempt.

!!! warning "No automatic 'paper in, scan starts'"

    The originally planned flow — insert paper, scanner starts by itself —
    is not achievable on this device through SANE. `scanbd` was dropped
    from the Phase 1.2 design as a direct result.

The captured logs, the negative evidence, and the open question of
whether a USB-level signal exists that the backend simply does not
decode are in
[`docs/research/scanner-hardware-events.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/docs/research/scanner-hardware-events.md).
A USB-capture investigation is tracked in
[issue #7](https://github.com/strausmann/paperless-scan-bridge/issues/7).

### Practical consequence

Webhook triggering over HTTP and Zigbee remotes via Home Assistant are
the primary trigger paths on this device. The indicator wheel works as a
*secondary* hardware trigger through scanbd's `function_knob` mapping.
The Start button does not work today.

## Diagnostics via NVRAM

The `avision` backend exposes a read-only `--nvram-values` string
containing the scanner model, firmware version, serial number,
manufacturing date, first-scan date, and total pad/ADF scan counters.
The monitoring stack reads this for operational dashboards — it is the
cheapest way to answer "how many pages has this roller seen?".

## Paper handling and maintenance

- Deterministic with standard office paper weights, 60–105 g/m².
- Heavier stock — cardboard, glossy photographs — needs manual feed
  through the front slot.
- ADF capacity: 50 sheets nominal, 30 sheets reliable.
- Clean the rollers monthly with a lint-free cloth and isopropyl
  alcohol.
