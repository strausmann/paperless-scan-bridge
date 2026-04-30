# Research: Scanner Hardware Events Beyond SANE

> **Status:** In progress — Phase 1 (SANE-level baseline) complete,
> Phase 2 (USB-level capture) planned
> **Started:** 2026-04-30
> **Reference device:** Kodak ScanMate i1120 (USB ID `040a:6013`)
> **Tracked issue:** TBD (file in `strausmann/paperless-scan-bridge`)

This document captures the empirical investigation of which Kodak
i1120 hardware events are accessible from user-space Linux through
the standard SANE / `avision`-backend stack, and which require going
deeper. The investigation runs in parallel to Phase 1.2 of the
bridge — Phase 1.2 ships a webhook-only triggering model that does
not depend on this work; positive findings here unlock additional
trigger sources in a later phase.

## 1. Motivation

A "hands-free document ingestion" stack ideally reacts to physical
cues:

- The user inserts paper → start scanning automatically
- The user presses the dedicated "Start" / "Scan" button → start
  scanning
- The user changes a profile selector on the scanner hardware →
  switch the active scan profile

Whether any of these are achievable depends entirely on what the
scanner exposes to user-space. The SANE backend (here: `avision`)
is the canonical interface, but it surfaces only what its authors
chose to implement. Lower layers — the USB control endpoints, vendor
control transfers, button-status interrupt endpoints — may carry
signals the SANE backend simply does not decode.

This document tracks (a) what the SANE layer reveals on the i1120
today, and (b) what we plan to learn at the USB layer.

## 2. Phase 1 findings — SANE / scanbd layer (2026-04-30)

All Phase 1 testing was conducted on the bridge's reference platform
(see `HARDWARE_COMPATIBILITY.md` § 3) with the scanner directly
attached, using container-based smoke tests (no host-side SANE
installation).

### 2.1 What is detectable

#### Indicator wheel (LCD function selector, 1–9)

The scanner has a small LCD on top displaying a number 1–9, changed
by two adjacent up/down buttons.

- **SANE option:** read-only string `--message`
- **Format on press:** `<n>:button1` where `<n>` is the new
  indicator value
- **Behaviour:** event-style (consume-on-read). The value is
  populated for one read after a press, then becomes empty again.
  Continuous polling at sub-second intervals is required to catch
  events reliably.
- **Verified:** all nine positions emitted distinct events during
  a 60-second scanbd capture at 250 ms polling interval (21 events
  total over the test window). scanbd's stock `function_knob`
  filter (`filter = "^message.*"`) matches these events directly.

#### NVRAM metadata

The avision backend exposes a read-only string option that returns
factory and lifetime data:

```
Vendor: KODAK
Model: i1120 SCANNER
Firmware: 1.10
Serial: 52488432
Manufacturing date: 2011-9-17
First scan date: 2012-5-2
Pad scans: 1569
ADF simplex scans: 1569
```

- **SANE option:** `--nvram-values` (read-only string)
- **Useful for:** ops dashboards, monitoring, lifetime tracking,
  hardware-replacement planning. The bridge's monitoring layer
  reads these on a slow cadence (e.g. every 15 minutes) and emits
  Prometheus gauges.

### 2.2 What is NOT detectable at the SANE layer

#### "Start" / "Scan" button

The dedicated trigger button on the scanner front did not produce
any SANE-visible event under the conditions tested.

- **Tests performed:**
  - Direct option enumeration (`scanimage -A -n`) before, during,
    and after presses — no change in any option's value
  - Manual polling of `--message` at ~1.5 s intervals during
    deliberate Start-button presses — no events
  - scanbd live-capture at 250 ms internal polling for 60 s with
    repeated Start-button presses — zero events captured for
    `button2`, `button3`, `scan`, or any pattern other than
    `<n>:button1`
- **Negative evidence is strong:** in the same 60-second window,
  21 indicator-wheel events were captured. The instrumentation was
  working; the Start button simply produces nothing the SANE layer
  surfaces.

#### ADF paper sensor (paper-present / paper-empty)

The scanner has an optical sensor in the ADF that detects paper
loading. The avision backend does not surface its state.

- **Tests performed:**
  - Snapshot diff: full `scanimage -A -n` output captured with no
    paper, then paper inserted, then captured again. Files were
    byte-identical (zero-line diff).
  - scanbd 250 ms polling for 25 s during paper-insert by hand — no
    events.
- **Negative signal is also strong:** the sensor is functional (the
  scanner correctly returns `SANE_STATUS_NO_DOCS` when a scan is
  initiated with no paper present), so the hardware *knows*. But the
  state is not pushed to the SANE option layer; it is only
  observable as a per-scan side-effect.

### 2.3 Implications for the bridge architecture

These findings shaped Phase 1.2's webhook-only trigger design (see
`docs/superpowers/specs/2026-04-30-phase-1.2-webhook-architecture-design.md`).
Specifically:

- **No `scanbd` in the Phase 1.2 `sane-runtime` container.** Without
  Start-button or paper-sensor signals, scanbd's value over direct
  webhook calls is limited to the indicator wheel — which is a
  useful but secondary input. Phase 1.2 leaves scanbd out and
  re-introduces it conditionally based on Phase 2 outcomes.
- **No "auto-scan on paper insert" feature in Phase 1.2.** The
  closest stable signal is `SANE_STATUS_NO_DOCS` returned from a
  scan attempt — i.e., poll-and-try, not push-on-event. Polling-
  scan is rejected for being noisy and wear-positive on the ADF
  rollers.
- **Webhook callers are the trigger source.** Home Assistant
  (Zigbee remotes), Paperless plugins, n8n workflows, ad-hoc
  scripts.

## 3. Phase 2 plan — USB-level capture

Phase 1 answered "what does SANE surface". Phase 2 will answer
"what does the hardware actually send on the USB bus", which
determines whether an upstream patch to the avision backend is
both possible and worthwhile.

### 3.1 Hypothesis

The Kodak i1120 *probably* transmits Start-button presses and
paper-sensor state as either:

- USB vendor-specific control transfers polled by the manufacturer's
  Windows driver, or
- Asynchronous interrupt-endpoint messages on a non-bulk endpoint
  the avision backend ignores

A look at the upstream `sane-backends` source for the `avision`
driver (file `backend/avision.c`) will quickly tell us whether the
button code path even exists for this device family. If it exists
but is gated on a model-specific quirks table, adding the i1120
might be a one-line patch.

### 3.2 Methodology

1. **Capture under Linux while operating from Windows.** Set up a
   Windows VM (Hyper-V, KVM with USB passthrough, or a physical
   Windows host with USBPcap) running the official Kodak/Alaris
   driver and capture-application stack. Press Start; insert and
   remove paper. Save the USB capture (`.pcapng`).
2. **Capture under Linux with `usbmon`.** On Linux, with the SANE
   `avision` backend held idle (no scan in progress), capture the
   USB bus while pressing Start and inserting/removing paper.
3. **Diff.** A button or sensor signal will appear in the Windows
   capture as a control transfer or interrupt message that does not
   appear in the idle Linux capture.
4. **Decode.** If a signal is found, decode the payload. Cross-
   reference with the avision backend's existing handling of
   similar Avision OEM models (e.g. AV600U, AV121, AV620U2) for
   parallels.
5. **Upstream-PR feasibility.** If the capture reveals a polling-
   based protocol that the avision backend already implements for
   other models, propose adding the i1120 to the relevant quirks
   table. If the protocol is genuinely new, the work scales up
   accordingly.

### 3.3 Tools

| Tool | Use |
| --- | --- |
| `usbmon` (kernel module) | Linux-side capture |
| `wireshark` with `usbmon` source | Decode and search USB traffic |
| `USBPcap` | Windows-side capture (free, MIT licensed) |
| `lsusb -v` | Endpoint enumeration |
| `sane-backends` source on GitHub | Reference for avision quirks tables |

### 3.4 Test environment

The reference scanner currently lives on `hhplex01` in the bridge
maintainer's homelab (a Dell OptiPlex 3070 Micro). A Windows VM
or live Windows boot on the same physical host would simplify
identical-hardware capture comparison. Alternatively, a USB-MITM
device (e.g. one of the open-source USB sniffers) would let the
capture happen continuously without VM overhead.

The capture data and analysis will be added to this document as
sections 4 (raw findings) and 5 (interpretation) once Phase 2
completes.

## 4. Phase 2 raw findings — TBD

*This section will be filled in once Phase 2 captures are
completed. It will contain: capture file references, packet
counts per action, side-by-side comparisons of Linux-idle vs
Windows-active timing, decoded payloads.*

## 5. Phase 2 interpretation — TBD

*This section will conclude whether an upstream patch is
feasible, in what form, and what the next steps are.*

## 6. Status, decisions, and links

| Phase | Status | Date |
| --- | --- | --- |
| Phase 1: SANE-level baseline | Complete | 2026-04-30 |
| Phase 2: USB-level capture | Planned | TBD |
| Upstream-PR (avision backend) | Conditional on Phase 2 | TBD |

### Related documents

- `docs/superpowers/specs/2026-04-30-phase-1.2-webhook-architecture-design.md`
  — the Phase 1.2 design that directly relies on this Phase 1
  baseline
- `HARDWARE_COMPATIBILITY.md` § 4.1 — user-facing summary of i1120
  hardware-button status (the per-button reality table)
- `AGENTS.md` — overall project guidelines

### Reproducibility — Phase 1 captures

The Phase 1 captures were performed in disposable Docker containers
on `hhplex01`. The scripts and Dockerfiles used remain on the host
under `/tmp/` and are documented in the bridge maintainer's session
notes. To reproduce on a different host:

1. Attach a Kodak i1120 (or other avision-driven scanner) via USB
2. Build the test images:
   - `sane-smoke` — `debian:12-slim` + `sane-utils`
   - `scanbd-test` — `sane-smoke` + `scanbd`
3. Run `docker run --rm --device /dev/bus/usb sane-smoke -L` to
   confirm SANE detection
4. Run `docker run -d --rm --name scanbd-test --device /dev/bus/usb
   -v <config>:/etc/scanbd/scanbd.conf scanbd-test scanbd -f -d`
   with `debug-level=7` and a `^message.*` catch-all action
5. Press buttons; observe `docker logs` for option values

A production-quality replication procedure (with reproducible
Dockerfiles checked into the repo) will accompany Phase 2.
