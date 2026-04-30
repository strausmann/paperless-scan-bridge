# Hardware Compatibility

> **Status:** Living document
> **Last updated:** 2026-04-30 (i1120 hardware-button findings re-verified)
> **Maintainer:** Björn Strausmann + community contributors

This document tracks scanner hardware verified to work with
`paperless-scan-bridge`, with what configuration, and at what level
of completeness.

If your scanner is not listed, contributions are welcome. The
[Adding an entry](#adding-an-entry) section explains how. Most of
this list comes from the community — only a small number of devices
are tested by the maintainer personally.

---

## Table of contents

1. [Reading this document](#1-reading-this-document)
2. [Compatibility levels](#2-compatibility-levels)
3. [Reference platform](#3-reference-platform)
4. [Verified scanners](#4-verified-scanners)
5. [Known incompatible scanners](#5-known-incompatible-scanners)
6. [Untested but likely-compatible scanners](#6-untested-but-likely-compatible-scanners)
7. [Trigger device compatibility](#7-trigger-device-compatibility)
8. [Storage backend compatibility](#8-storage-backend-compatibility)
9. [Adding an entry](#9-adding-an-entry)
10. [Testing your scanner](#10-testing-your-scanner)
11. [Reporting incompatibilities](#11-reporting-incompatibilities)

---

## 1. Reading this document

The compatibility tables include:

- **Vendor and model** — the printed name, not the marketing name
- **USB ID** — vendor and product as four hex digits each, useful
  for udev rules
- **SANE backend** — which backend handles the device
- **Compatibility level** — see next section
- **Verified by** — who tested, with rough date
- **Notes** — anything model-specific worth knowing

A scanner being absent from this list does not mean it does not
work; it just means nobody has reported either way. Many SANE-
compatible scanners likely work without anyone having tried.

---

## 2. Compatibility levels

We use four levels to describe how thoroughly a scanner has been
tested:

### Level A — Reference

Tested by the maintainer in production use. All features verified.
Documentation includes model-specific notes. Issues here are treated
as bugs in this project.

Currently: only the Kodak ScanMate i1120.

### Level B — Verified

Tested by a contributor who runs the stack with this scanner. Core
functionality (scanning, profiles, atomic writes to consume) verified.
Hardware buttons and advanced features may or may not be tested.

### Level C — Reported working

A contributor has reported success but the testing was not exhaustive.
Could include "I got my first scan to work" reports, which are
valuable but do not constitute thorough validation.

### Level D — Likely compatible (untested)

Based on the SANE compatibility database and similarity to verified
devices. Listed for guidance, but the user is the first tester.

### Level X — Known incompatible

Scanners that do not work, with documented reasons. Listed so
prospective buyers and users can avoid them.

---

## 3. Reference platform

The reference platform is what the maintainer uses daily and what
all Level A entries are tested on:

| Component | Specification |
| --- | --- |
| Scanner | Kodak ScanMate i1120 |
| Pi | Raspberry Pi 5 with 8 GB RAM |
| Pi storage | 256 GB SSD via USB 3.0 (no SD card boot) |
| Pi OS | Ubuntu Server 24.04 LTS arm64 |
| Pi kernel | 6.8 series (Ubuntu HWE) |
| Docker | 27.x |
| NAS | Synology DS920+, 4×4 TB SHR-1, DSM 7.2.x |
| Network | Gigabit wired, IoT VLAN segmented |
| Trigger | IKEA STYRBAR + Home Assistant 2026.4 |
| Zigbee | zigbee2mqtt 1.42.x with ConBee III |

Other platforms are expected to work. Differences from this
reference are noted per-entry in the tables below where relevant.

---

## 4. Verified scanners

### 4.1 Kodak (Alaris)

| Model | USB ID | SANE backend | Level | Verified by | Date | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| ScanMate i1120 | 040a:6013 | avision | A | Björn Strausmann | 2026-04 | Reference device. ADF + duplex via `ADF Duplex` source, 75–600 DPI. Hardware-button support is **asymmetric** — see notes below. |

**Model-specific notes for i1120 (re-verified 2026-04-30):**

- The `avision` backend has been marked unmaintained since 2020 but
  works reliably for this device on Linux kernel 5.15+ (verified
  2026-04 on Ubuntu Server 24.04 with the 6.8 kernel series).
- **Hardware-button support is partial.** The LCD function-indicator
  wheel (the 1–9 selector) generates SANE events on the read-only
  `--message` option as strings of the form `<n>:button1`. These
  events are reliably captured by scanbd's stock `function_knob`
  filter at 250 ms polling and were empirically verified across
  all nine indicator positions during the 2026-04-30 testing run.
- **The hardware "Start" (scan-trigger) button generates no
  SANE-visible event** — neither via the avision backend's options,
  nor via direct enumeration with `scanimage -A`, nor via scanbd
  polling. Multiple Start-button presses during a 60-second scanbd
  capture session produced zero events while indicator-wheel turns
  produced 21 events. See `docs/research/scanner-hardware-events.md`
  for the captured logs and the open question of whether a USB-level
  signal exists that the avision backend simply does not decode.
- **The ADF paper sensor is similarly opaque to SANE.** Paper
  insertion and removal change neither `scanimage -A` output nor
  the `--message` field — verified by snapshot diff and live
  scanbd polling on 2026-04-30. The scanner *does* report
  paper-empty as a `SANE_STATUS_NO_DOCS` error during a scan
  attempt; that is the only signal callers can rely on for
  "is there paper?".
- **Practical consequence for triggering on this device:** webhook
  triggering (HTTP) and Zigbee remotes via Home Assistant are the
  primary paths. The indicator wheel works as a *secondary*
  hardware trigger via scanbd's `function_knob` mapping. The Start
  button does not work today; that is upstream-tracked in the
  research doc above.
- **Useful diagnostic data via `--nvram-values`:** the avision
  backend exposes scanner model, firmware version, serial number,
  manufacturing date, first-scan date, and total pad/ADF scan
  counters as a read-only string. The bridge's monitoring stack
  reads this for ops dashboards.
- Maximum scanning speed (vendor spec): 20 ppm simplex, 10 ppm
  duplex on USB 2.0. USB 3.0 host port does not increase speed
  (the scanner itself is USB 2.0).
- Paper handling: deterministic with standard office paper weights
  (60–105 g/m²). Heavier stock (cardboard, glossy photographs)
  requires manual feed via the front slot.
- ADF capacity: 50 sheets nominal, 30 sheets reliable.
- Cleaning: rollers should be cleaned monthly with a lint-free
  cloth and isopropyl alcohol; documented in
  `docs/hardware/kodak-i1120/maintenance.md`.

### 4.2 Other verified scanners

This section is currently empty. As contributors verify their
scanners and submit entries via PR, they will appear here. The
Kodak i1120 row above is the template; each new entry follows the
same format.

Anticipated growth areas based on community demand and SANE
compatibility:

- Other Kodak ScanMate and i-series devices using the avision
  backend
- Brother ADS-series document scanners using the brother4 or
  pixma backends
- Fujitsu ScanSnap iX-series using the epjitsu backend
- Canon imageFORMULA DR-series using the canon_dr backend
- Plustek SmartOffice and PS-series using the plustek backend
- Epson WorkForce ES-series using the epson2 backend (USB only —
  network mode requires escl which is partly supported)

When you verify any of these, please contribute an entry.

---

## 5. Known incompatible scanners

Scanners documented to NOT work, and why. Listed to save other
people the same investigation.

| Model | USB ID | Reason | Reported by | Date |
| --- | --- | --- | --- | --- |
| (none yet) | | | | |

This section will populate as community reports come in. The
typical incompatibility patterns to expect:

- **Brother network-only scanners** (some models lack USB SANE
  support entirely; only the proprietary brscan driver works,
  which is x86-only)
- **WIA-only Windows scanners** (no Linux driver path at all)
- **Some HP all-in-one devices** (USB scan path exists but is
  poorly maintained in hplip)
- **Older Canon flatbeds** (CIS sensors with proprietary protocols
  that SANE does not implement)

If your scanner falls into one of these categories, please report
it so others can be warned.

---

## 6. Untested but likely-compatible scanners

These are scanners listed in the SANE compatibility database with
a "good" status, that share a backend with a verified device, but
nobody has confirmed they work in this specific stack.

This is your contribution opportunity. If you have one of these and
get it working, you upgrade the entry to Level C and earn a place in
the contributor list.

### 6.1 Kodak / Alaris (avision backend, similar to i1120)

| Model | USB ID | Notes |
| --- | --- | --- |
| Kodak ScanMate i940 | 040a:6035 | Smaller form factor, ADF, USB-only |
| Kodak ScanMate i1150 | 040a:6014 | Similar to i1120 with 25 ppm |
| Kodak ScanMate i1180 | 040a:6015 | Larger ADF capacity |
| Kodak ScanMate i1190 | 040a:6016 | Similar generation |
| Kodak ScanMate i2400 | 040a:6019 | Office-class A4 sheet-fed |
| Kodak ScanMate i2600 | 040a:601a | Higher-volume successor |

### 6.2 Brother (pixma or brother backends)

| Model | USB ID | Notes |
| --- | --- | --- |
| Brother ADS-1700W | 04f9:0445 | Document scanner, ADF, USB+network |
| Brother ADS-2200 | 04f9:033c | Higher-volume office scanner |
| Brother ADS-2700W | 04f9:0466 | Network-capable, often USB-fallback |

Brother caveats: the brother4 backend is community-maintained;
official support depends on Brother's proprietary brscan driver
which is x86-only and not packaged for ARM. We document the
community backend usage; mileage may vary.

### 6.3 Fujitsu / Ricoh ScanSnap

| Model | USB ID | Notes |
| --- | --- | --- |
| Fujitsu ScanSnap iX1500 | 04c5:132b | Popular document scanner, epjitsu backend |
| Fujitsu ScanSnap iX1600 | 04c5:1373 | Successor, same backend family |
| Fujitsu ScanSnap S1300i | 04c5:128d | Compact, portable |

ScanSnap caveats: official Fujitsu software (ScanSnap Home) is
Windows/macOS only. SANE epjitsu backend works for many models but
firmware updates can occasionally break compatibility. Pin your
firmware version once a working combination is found.

### 6.4 Canon imageFORMULA

| Model | USB ID | Notes |
| --- | --- | --- |
| Canon DR-C225 | 1083:165d | Compact document scanner |
| Canon DR-M160 | 1083:163d | Mid-volume, canon_dr backend |
| Canon DR-M260 | 1083:1740 | Higher-volume office scanner |

Canon caveats: the canon_dr backend supports many DR-series models
but quality varies. Some devices need firmware quirks documented in
the SANE source.

### 6.5 Plustek

| Model | USB ID | Notes |
| --- | --- | --- |
| Plustek SmartOffice PS283 | 07b3:0c30 | Compact ADF scanner |
| Plustek SmartOffice PS406U | 07b3:0c40 | A4 office scanner |

### 6.6 Epson

| Model | USB ID | Notes |
| --- | --- | --- |
| Epson WorkForce ES-50 | 04b8:014c | Portable, single-sheet |
| Epson WorkForce ES-300W | 04b8:014b | Compact ADF, USB+WiFi |
| Epson WorkForce ES-400 | 04b8:014a | Office ADF scanner |

Epson caveats: the epson2 backend covers many models well. Some
devices also support eSCL (network protocol) via the airscan
backend, which is increasingly the preferred path for newer hardware.

---

## 7. Trigger device compatibility

The bridge accepts triggers from any HTTP client. The Home Assistant
blueprints we ship are tested against specific Zigbee remote devices.

### 7.1 Zigbee remotes verified with shipped blueprints

| Device | Blueprint | Buttons mapped | Verified by | Notes |
| --- | --- | --- | --- | --- |
| IKEA STYRBAR | `homeassistant/blueprints/styrbar.yaml` | 4 directional + 2 corners | Björn | All button events covered. Long-press distinguishable. |
| IKEA SYMFONISK Sound Remote Gen 2 | `homeassistant/blueprints/symfonisk-gen2.yaml` | Play/pause + 4 directional + 2 dots | Pending | Blueprint drafted, awaiting hardware test |
| IKEA RODRET | `homeassistant/blueprints/rodret.yaml` | On + Off, with hold | Pending | Simpler 2-button device; covers most use cases |
| IKEA TRÅDFRI Round (5-button) | `homeassistant/blueprints/tradfri-round.yaml` | 5 buttons | Pending | Older but widely available |

### 7.2 Other triggers

| Source | Status | Notes |
| --- | --- | --- |
| HTTP webhook (curl, Python script, mobile app) | Universal | The canonical interface; if you can speak HTTP and bearer auth, you can trigger |
| Scanner hardware buttons via scanbd | Partial (i1120) | Per-scanner reality; the avision backend on the i1120 surfaces the LCD indicator wheel via `function_knob` but not the Start button or paper sensor (see i1120 notes in section 4.1). Other backends may differ — document per-button results when adding a scanner |
| n8n workflow | Documented | Example workflows under `n8n/`; n8n is itself a webhook caller |
| Node-RED | Untested but trivial | Node-RED HTTP request node configures in 30 seconds |

### 7.3 Mobile apps for ad-hoc triggering

The bridge does not ship a dedicated mobile app, but the API is
simple enough that any HTTP client works. Tested options:

- **Home Assistant companion app**: shortcuts on the home screen
  triggering automations
- **iOS Shortcuts**: a "Get contents of URL" action with bearer
  auth header
- **Android Tasker / HTTP Request Shortcuts**: same pattern as iOS
- **HTTP Shortcuts** (Android, open source): purpose-built for
  triggering webhooks

A future Phase 4 consideration is a thin PWA that does nothing but
trigger scans; tracked as a low-priority enhancement.

---

## 8. Storage backend compatibility

The bridge writes scans to a directory. What lives behind that
directory is a separate decision documented as a "topology" choice.

### 8.1 Verified storage backends

| Backend | Topology | Verified by | Notes |
| --- | --- | --- | --- |
| Synology DS920+ over NFSv4 | A, B, C | Björn | Reference platform |
| Local ext4 on Docker host | A | Björn | The "fast path" of Topology A |

### 8.2 Likely-compatible storage backends

| Backend | Topology | Notes |
| --- | --- | --- |
| Synology DS-series (other models) | A, B, C | DSM 7+ supported; older DSM versions may have NFSv4 limitations |
| QNAP NAS over NFSv4 | A, B, C | Compatible NFS implementation; specifics depend on QTS version |
| TrueNAS Core/SCALE over NFSv4 | A, B, C | Mature NFS implementation; widely tested in homelab community |
| Unraid over NFS | A | Common in homelab setups |
| Generic Linux NFS server | A, B, C | Anything with stock Linux kernel NFS will work |
| btrfs/zfs locally on Docker host | A | Atomic write via O_TMPFILE works on both |

### 8.3 Known-problematic storage backends

| Backend | Issue |
| --- | --- |
| SMB/CIFS as the consume target | Atomic writes via O_TMPFILE not supported on most CIFS configurations; falls back to rename-based atomic write which is less robust |
| FUSE filesystems generally | Performance and inotify reliability vary widely; not recommended for the consume directory |
| NFSv3 (versus NFSv4) | inotify not supported regardless of version; NFSv4 has slightly better lock behavior |

### 8.4 Cloud storage backends

Cloud storage as the live consume directory is not recommended. The
latency and inotify limitations make it impractical. However, cloud
storage as the *backup* destination (Topology A's restic target)
works well — see [DISASTER_RECOVERY.md](DISASTER_RECOVERY.md).

| Cloud target | Use case | Notes |
| --- | --- | --- |
| Backblaze B2 | Off-site restic backup | Cheap, restic-native support |
| Hetzner Storage Box | Off-site restic backup | EU jurisdiction, simple pricing |
| AWS S3 | Off-site restic backup | Most expensive but most flexible (Object Lock for immutability) |
| Wasabi | Off-site restic backup | Cheap, S3-compatible, no egress fees |

---

## 9. Adding an entry

We welcome contributions. The process:

### 9.1 Verify your scanner works

Run through the testing procedure in section 10 before submitting.
The goal is for the entry to genuinely help future readers, not
just to grow the list.

### 9.2 Open a pull request

1. Fork the repository
2. Edit this file (`HARDWARE_COMPATIBILITY.md`) to add your row
3. Use the row template from section 9.3
4. If your scanner needs configuration not already shipped, add
   it under `components/sane-runtime/config/<vendor>-<model>.conf`
5. If your scanner has an unusual udev requirement, add the rule
   to `deploy/udev/99-paperless-scan-bridge.rules`
6. If model-specific notes warrant a dedicated page, add a file at
   `docs/hardware/<vendor>-<model>.md`
7. Open a PR with the title `feat(hardware): add <vendor> <model>
   compatibility (level <X>)`

### 9.3 Row template

Copy this and adapt:

```markdown
| <Model name> | <vendor>:<product> | <SANE backend> | <Level> | <Your name or handle> | <YYYY-MM> | <One-line note> |
```

For example:

```markdown
| Kodak ScanMate i940 | 040a:6035 | avision | C | Anna Müller | 2026-05 | ADF works at 300 DPI; duplex untested. |
```

If your one-line note becomes more than two sentences, move the
detail to a separate section under "Model-specific notes" beneath
the table.

### 9.4 What we ask of contributors

- Test before reporting. "I plugged it in and it appeared in scanimage -L"
  is not enough; please complete at least the testing procedure in
  section 10.1.
- Be honest about scope. If you only tested simplex at 200 DPI,
  say so. Future contributors can extend the testing.
- Document the failure modes you encountered along the way. Future
  users with the same hardware benefit enormously from "I tried
  X, it didn't work because Y, Z fixed it."

---

## 10. Testing your scanner

A four-stage procedure for verifying a new scanner. Each stage is a
gate; if a stage fails, the scanner does not move to the next.

### 10.1 Stage 1: SANE detection

Confirm the scanner is recognized by SANE inside the container.

```bash
# On the Pi host
lsusb
# Note the vendor:product ID

# Run a one-off SANE detection container
docker run --rm \
    --device=/dev/bus/usb \
    ghcr.io/strausmann/paperless-scan-bridge/sane-runtime:latest \
    scanimage -L
```

**Expected output:** at least one device with `device 'avision:libusb...'`
or similar. If the device list is empty:

- Verify the USB device is enumerated: `lsusb -v -d <vid>:<pid>`
- Check the SANE backend is enabled: `grep -i <backend> /etc/sane.d/dll.conf`
  inside the container
- Try a different USB port (USB 2.0 vs 3.0 differences exist)
- Check `dmesg` for kernel-level errors during USB enumeration

If Stage 1 passes, you have basic SANE compatibility. Move on.

### 10.2 Stage 2: First scan

Perform a single scan to a file, completely outside the bridge
infrastructure.

```bash
docker run --rm \
    --device=/dev/bus/usb \
    -v /tmp/scans:/output \
    ghcr.io/strausmann/paperless-scan-bridge/sane-runtime:latest \
    scanimage --device <DEVICE_FROM_STAGE_1> \
              --format=tiff \
              --resolution=300 \
              --mode=Color \
              -o /output/test-scan.tiff
```

**Expected output:** A TIFF file at `/tmp/scans/test-scan.tiff`
that opens in any image viewer and shows your scanned page.

If Stage 2 passes, the scanner-to-file path works. Move on.

### 10.3 Stage 3: Bridge integration

Bring up the full bridge stack with your scanner attached.

1. Add a udev rule for your scanner (see section 10.4)
2. Add a profile entry for your scanner in your local
   `profiles.yaml`
3. Bring up the compose stack
4. Trigger a scan via curl:

```bash
curl --silent --fail \
     -H "Authorization: Bearer $(cat /etc/paperless-scan-bridge/secrets/api-token)" \
     -H "Content-Type: application/json" \
     -d '{"profile": "test-profile"}' \
     http://localhost:8080/scan
```

**Expected:** A 202 Accepted response with a job ID. Within 30
seconds, a PDF appears in the consume directory and gets ingested
by Paperless.

If Stage 3 passes, the integrated path works. Move on.

### 10.4 Stage 4: Hardware buttons (optional)

If your scanner has hardware buttons and you want to use them:

1. Configure scanbd in `etc/scanbd/scanner.d/<your-vendor>-<model>.conf`
2. Restart the sane-runtime container
3. Press *each* hardware button on the scanner in turn
4. Verify which presses produce events in the scanbd log and
   trigger configured actions

**Per-button reality check.** "Hardware buttons" is a fuzzy phrase.
A scanner may have several buttons (Scan, Cancel, +/-, function
wheels, profile presets) and the SANE backend may surface some,
all, or none of them. The reference Kodak i1120 surfaces the LCD
function wheel but not the Start/Scan button — see section 4.1.
When you contribute a scanner entry, document which specific
buttons produced SANE-visible events and which did not. A scanner
with a working function wheel and a non-working Scan button is
still a fine Level B candidate; the doc just needs to be honest
about the asymmetry.

This stage is optional in the sense that the bridge does not
require any hardware-button support — webhook callers (HA, Paperless,
n8n, curl) cover all use cases. Zigbee remotes via Home Assistant
are often a more reliable physical trigger than scanner buttons,
because the SANE button surface is hardware-dependent.

### 10.5 Reporting your results

After completing the relevant stages, document:

- Which stages you completed
- The exact firmware version of your scanner (some models behave
  differently across firmware revisions)
- Your Pi/host hardware and OS
- Anything unusual you encountered

Open the PR per section 9.

---

## 11. Reporting incompatibilities

Equally important: if your scanner does not work, please report it.
Saving someone from buying the same incompatible device is as
valuable as confirming a working one.

### 11.1 Format

Open an issue with the title `incompat: <vendor> <model>`. Include:

- USB vendor:product ID
- Output of `scanimage -L` from inside the sane-runtime container
- Output of `lsusb -v -d <vid>:<pid>`
- The SANE backend you tried (and any others you tested)
- The exact failure mode (no detection, scan starts but fails,
  output corrupted, etc.)
- Whether the scanner works under Linux outside this stack (e.g.
  with `simple-scan` on a desktop)

### 11.2 What we do with the report

- Confirm the issue (or note it as unconfirmed if we cannot
  reproduce)
- Add an entry to section 5 (Known incompatible scanners)
- Document any partial workarounds we identify
- Track the issue for re-testing after SANE updates

We do not promise to make any specific scanner work. The SANE
project is upstream of us, and many incompatibilities live there.
But documenting the situation helps everyone.

---

## 12. Acknowledgments

The hardware compatibility list is a community effort. Contributors
who have submitted verified entries are credited in the table rows
and listed in the project README.

Special thanks to the SANE project and its contributors. Without
their decades of work on Linux scanner support, this project would
not exist.
