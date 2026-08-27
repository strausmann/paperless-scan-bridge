# CYD scan-control panel firmware (v2, secret-free)

ESPHome firmware for a wall-/desk-mountable touch panel that lists the
scan-bridge's configured scan profiles and triggers a scan over HTTP. See
[Issue #9](https://github.com/strausmann/paperless-scan-bridge/issues/9)
for the full design — this firmware implements phase D ("Firmware") and
phase E ("Build & distribution") against the API shape currently live on
the bridge; see "Scope and known limitations" below for what is deferred.

**This build carries no secrets.** Wi-Fi credentials, the bridge URL and
the bearer token are never build-time values — see "Secret-free firmware"
below. That is what makes it possible to distribute one public binary via
the browser-based [ESP Web Tools](https://esphome.github.io/esp-web-tools/)
installer (published on the project's docs site), instead of everyone
having to compile their own firmware with `esphome`.

## Board

**ESP32-2432S028R** — the widely-cloned "Cheap Yellow Display" (CYD).
WROOM-32 module, **no PSRAM**, 2.8" 320x240 ILI9341 TFT with a resistive
XPT2046 touch controller, an active-low RGB status LED, and a
GPIO21-driven backlight. This firmware targets that specific board
revision; the pin map in `cyd-scan-panel.yaml` will not match other CYD
variants (e.g. the ESP32-2432S024 or the capacitive-touch ESP32-8048
boards) or generic ESP32 dev boards with a bolted-on display.

## Secret-free firmware

Nothing in `cyd-scan-panel.yaml` is a `!secret` and there is no
`secrets.yaml` in this directory — `esphome config`/`esphome compile` run
against it as-is. What used to be build-time values are now one of two
things:

- **Wi-Fi station credentials** — provisioned entirely at runtime via
  [Improv Wi-Fi](https://www.improv-wifi.com/) (`esp32_improv` over BLE,
  `improv_serial` over the same USB-serial connection used for flashing).
  ESP Web Tools drives this in the browser right after flashing. Nothing
  Wi-Fi-related is ever compiled into the binary.
- **Bridge URL and bearer token** — two `text` entities (`Bridge URL`,
  `Bridge Token`) that persist to flash (`restore_value: true`) and are
  set from the panel's own **on-device web dashboard** (`web_server:`,
  reachable at the panel's IP once it has Wi-Fi) — no re-flash needed to
  change either. `Bridge URL` starts **empty** on a freshly flashed
  panel — set it to your own bridge's address (for example
  `http://<your-bridge-host>:18080`). `Bridge Token` starts **empty**
  on a freshly flashed panel —
  see "Configuration" below.

This is why a single compiled `.factory.bin` can be safely published for
anyone to flash: it identifies no network, no bridge, and no credential
until someone sets them on their own device, after their own flash.

## Prerequisites

**Browser flashing (recommended for most people):** Chrome or Edge
(Web Serial support), a USB-A-to-USB-C (or micro-USB, depending on the
board revision) cable, and the installer page on the project's docs site
— see Issue #9 for the link once phase E lands. No local toolchain.

**Building from source (for firmware development):**

- [ESPHome](https://esphome.io/) **≥ 2024.5.0** (the `http_request`
  action syntax used here — `http_request.get`/`http_request.post` with
  `on_response`/`on_error` — needs that version or newer; verified here
  against 2026.7.4). Install with `pip install esphome` or use the
  ESPHome Docker image; see the
  [ESPHome installation docs](https://esphome.io/guides/installing_esphome).
- A USB-A-to-USB-C (or micro-USB) cable for the first flash.
- Network access from wherever you run `esphome` to the board over USB,
  and later over Wi-Fi for OTA updates.

## Flashing

**Browser (ESP Web Tools):** open the installer page, click Install,
pick the serial port — the browser flashes the CI-built
`cyd-scan-panel.factory.bin` directly, no local toolchain. The installer
flow continues straight into Improv Wi-Fi provisioning in the same
browser tab.

**From source, over USB, from this directory:**

```bash
esphome run cyd-scan-panel.yaml
```

After the first flash, subsequent updates can go over the air (see
"Security model" below for why OTA has no password):

```bash
esphome upload cyd-scan-panel.yaml
```

## Configuration: runtime settings

Nothing to copy or fill in before flashing — everything below happens
**after** the flash, on the running device:

1. **Wi-Fi:** ESP Web Tools drives Improv Wi-Fi provisioning right after
   flashing (pick your network, enter the password, in the browser). If
   the panel can't join any configured network — first boot before
   provisioning, or a Wi-Fi outage — it starts a fallback SoftAP named
   `<friendly name> Setup` (password `panelsetup`, the same on every
   unit — see "Security model"); connect to that and use the captive
   portal to configure Wi-Fi manually.
2. **Bridge URL and Bridge Token:** once the panel has an IP, open
   `http://<panel-ip>/` in a browser — that's the on-device `web_server`
   dashboard. Set **Bridge URL** to your scan-bridge's address (the
   host and port you publish `scan-bridge` on, e.g.
   `http://<your-bridge-host>:18080`) and **Bridge Token** to the
   bridge's bearer token — the plaintext whose SHA-256 digest is in
   your `auth.token_hash`. Keep that token in your password manager;
   it is not in this repository.
   Both persist across reboots. Until **both** are set, the profile grid
   stays empty and tapping a (non-existent) button is a no-op — see
   `do_scan`'s guard in `cyd-scan-panel.yaml`.
3. **Grid Rows / Grid Cols:** on the same dashboard, two more entities
   let you resize the button grid at runtime, no re-flash needed —
   **Grid Rows** (1–3) and **Grid Cols** (1–3), default 2x3 (today's
   fixed 6-button layout, unchanged unless you opt in). Both persist
   across reboots. Nine button slots exist in the firmware; the grid
   size controls how many of them show at once (up to 3x3 = 9). If
   the bridge has more profiles than fit on one page, use the **`<`**
   / **`>`** buttons in the footer to page through them — see "Scope
   and known limitations" below.

## Touch calibration

**This is per-unit and mandatory.** The values shipped in
`substitutions:` are a starting point from one board; resistive panels
vary enough between units that a tap can land tens of pixels away, or in
a different quadrant entirely.

### Telling a calibration problem from a dropped tap

Two different faults look identical from the outside ("I tapped and
nothing happened"), so check which one you have before changing numbers.

Open the panel's dashboard, watch the **Debug Log**, and tap a button.
Every touch prints its raw value:

```text
[D][xpt2046:062] Touchscreen Update [2790, 3240], z = 2719
```

Convert it by hand. With `swap_xy: true` the driver swaps the pair
first, then maps each axis onto the screen:

```text
screen_x = (raw_y - x_min) / (x_max - x_min) * screen_width
screen_y = (raw_x - y_min) / (y_max - y_min) * screen_height
```

Then apply `mirror_x` / `mirror_y` if set: a mirrored axis becomes
`screen - value`.

This is how the reference unit's inversion was found. A tap on the
physical **top-left** corner read `[3820, 3820]` — near the ADC maximum
on both axes. Unmirrored that maps to **(316, 237)**: on a 320x240
screen that is 4 pixels from the bottom-right corner, i.e.
diagonally opposite the finger. Every press on the profile button landed
on empty screen and nothing happened. Both axes are mirrored in
`substitutions:` for that reason; the same tap now maps to **(4, 3)**.

If your computed point does not match where you touched, the calibration
is wrong for your unit.

If the computed point *does* match where you tapped and the button still
did not fire, the tap was dropped rather than mislocated — see
"Responsiveness" below.

### Correcting it

Tap each corner of the screen in turn and note the raw pairs. The
smallest and largest `raw_y` you see become `x_min`/`x_max`; the
smallest and largest `raw_x` become `y_min`/`y_max`.

If a corner reads near the **maximum** where you expected the minimum,
that axis is inverted — set `touch_mirror_x` / `touch_mirror_y`. Do not
try to express it by putting `x_min` above `x_max`: ESPHome rejects a
descending range with *"x_min must be smaller than x_max. To mirror the
direction use the 'transform' options"*.

Two corners on the **same diagonal** (top-left and bottom-right) cannot
tell you whether `swap_xy` is right — both axes move together. Use an
off-diagonal pair (top-right, bottom-left) to check that.

Put the values in the `substitutions:` block and re-flash — over USB or
OTA, both work.

## Responsiveness

`http_request` is synchronous. While a poll is in flight the main loop is
blocked and LVGL processes no input, so a tap in that window is lost, not
queued. A panel on a slow link logged

```text
[W][component:473] interval took a long time for an operation (1091 ms),
max is 190 ms
```

which is over a second of dead touchscreen per poll. The polling
intervals are set with that in mind (`check_bridge_health` every 60s,
`refresh_profiles` every 300s) rather than as fast as the data could
theoretically change. Lowering them makes the panel feel worse, not
fresher.

The same property affects the display, not just input. `lvgl.label.update`
writes into LVGL's object tree; the pixels are pushed in the component
loop. So a label set immediately before a blocking request is never
drawn — the panel showed `idle` for the whole of a twenty-second scan
even though `do_scan` had already written "Scanning: ...". `do_scan`
therefore yields with a short `delay` after updating the label and
spinner, before it starts the request. Any future code that updates the
UI and then blocks needs the same yield.

## Display orientation

**A build-time choice, not a runtime setting** (unlike Grid Rows/Cols
above) — orientation picks which physical dimensions and rotation get
compiled in, so it can only change with a re-flash. The published CI
binary is always **landscape** (320x240, the default below); building a
**portrait** variant needs a local `esphome` install and your own
compile.

Nine substitutions in `cyd-scan-panel.yaml` drive it:

| Substitution | Landscape (default, published) | Portrait |
| --- | --- | --- |
| `orientation` | `landscape` | `portrait` |
| `screen_width` | `320` | `240` |
| `screen_height` | `240` | `320` |
| `panel_rotation` | `0` | `90` |
| `touch_swap_xy` | `true` | `false` |
| `touch_x_min` | `280` | `340` |
| `touch_x_max` | `3860` | `3860` |
| `touch_y_min` | `340` | `280` |
| `touch_y_max` | `3860` | `3860` |

`screen_width`/`screen_height` feed `display.dimensions` **and** the
grid geometry lambdas that lay out the 9 button slots (`relayout_grid`
in `cyd-scan-panel.yaml`) — both stay in lockstep automatically.
`panel_rotation` feeds `lvgl.rotation`, which ESPHome's LVGL component
uses to rotate both the rendered content and the touch input together;
`display.rotation` itself stays hardcoded at `0` in both orientations —
setting it to anything else is rejected once `lvgl:` is configured
("set rotation in the LVGL config instead"). Per ESPHome's ili9xxx
docs, `display.dimensions` must already be the **rotated** (post-`lvgl.
rotation`) width/height, not the panel's physical/native ones — that is
why portrait swaps `screen_width`/`screen_height` rather than leaving
them at 320x240 and rotating on top.

To build a portrait variant, override the nine substitutions above with
a small `packages:` wrapper next to `cyd-scan-panel.yaml` (not
committed — this is a local/one-off build, not something CI publishes):

```yaml
# cyd-scan-panel-portrait.yaml
substitutions:
  orientation: portrait
  screen_width: "240"
  screen_height: "320"
  panel_rotation: "90"
  touch_swap_xy: "false"
  touch_x_min: "340"
  touch_x_max: "3860"
  touch_y_min: "280"
  touch_y_max: "3860"

packages:
  base: !include cyd-scan-panel.yaml
```

```bash
esphome run cyd-scan-panel-portrait.yaml
```

The `touch_x_min`/`touch_x_max`/`touch_y_min`/`touch_y_max` values above
are the landscape placeholders with the axes swapped — a reasonable
starting point given the calibration is a placeholder either way (see
"Touch calibration" above), **not** a verified portrait calibration.
Redo the calibration procedure after flashing a portrait build, same as
for a landscape one.

**What B4 does not do:** the header row (WiFi/Bridge status) and the
footer row (paging buttons, status label) are still laid out for a
320-wide screen regardless of orientation — only the button grid
between them resizes with `screen_width`/`screen_height`. A portrait
build's header/footer will not fill a 240px-wide screen correctly; a
dedicated portrait page layout is later work (see "Scope and known
limitations" below). **Not hardware-tested** — `esphome config` and
`esphome compile` pass for both orientations (see "Hardware
verification status" below), which proves the config is valid and the
firmware builds; it does not prove a portrait panel actually renders
right-side-up or that the touch mapping above is correct.

## Security model

This is a home-lab panel on a trusted LAN, not a hardened public-facing
device — every decision below trades convenience/distributability for a
security posture that would be unacceptable outside that context. If you
deploy this on a network you don't fully trust, treat all of the
following as things to change first:

- **Plain HTTP, not TLS.** `cyd-scan-panel.yaml` sets `verify_ssl: false`
  and the Bridge URL default is plain `http://`. The bearer token
  therefore travels in the clear, but only over the LAN. **If the bridge
  is ever put behind a TLS-terminating reverse proxy** (Traefik, a
  Pangolin resource, etc.), set the Bridge URL text entity to
  `https://...` and remove `verify_ssl: false` (or point it at the right
  CA) so the token is not sent in the clear over an untrusted path.
- **No `api:` block (no ESPHome native API).** A public binary cannot
  embed a per-device encryption key, and the panel doesn't need Home
  Assistant discovery — it's purely an HTTP client of the bridge. If you
  want HA integration, add your own `api: encryption: key: !secret ...`
  locally (that reintroduces the need for a local `secrets.yaml`, which
  is fine for a private build, just not for the published binary).
- **OTA has no password.** Same reasoning as `api:` — no fixed shared
  secret in a public binary, and no per-device way to set one before the
  first flash. Anyone who can reach the panel's IP on the LAN can push
  new firmware over the air. Re-flashing over USB via the installer page
  is always available as an alternative that doesn't depend on this.
- **The SoftAP setup password (`panelsetup`) is the same on every
  unit.** It only protects the temporary fallback hotspot that appears
  while the panel has no working Wi-Fi — not the bridge and not the
  panel's own dashboard. It cannot be anything else in a build that
  ships as one binary for everyone.
- **The on-device web dashboard has no login.** `web_server:` is
  unauthenticated — anyone on the LAN who can reach the panel's IP can
  read/change Bridge URL, Bridge Token, and Wi-Fi. `local: true` keeps
  its JS/CSS self-contained in the firmware image (no fetch to
  `esphome.io` at runtime), consistent with this project's "no cloud
  dependencies for core functionality" principle — that is a
  self-hosting choice, not an auth mechanism.
- **Improv provisioning has no physical confirmation step**
  (`authorizer: none`). Anyone with local BLE or serial access to the
  panel during a Wi-Fi provisioning window can set its network.

None of this is new risk introduced by going secret-free — v1 already
accepted the plain-HTTP/LAN-only trust model and unauthenticated Improv;
v2 extends the same posture to the two components (`api:`, OTA) that
previously depended on a build-time secret only the original builder
had, which was fine (a self-built, self-flashed device) but incompatible
with a publicly downloadable binary.

## Scope and known limitations

What it does:

- Reads the scan profiles from `GET /profiles` on boot and every 30s
  (once Bridge URL and Bridge Token are both set) into an internal list
  of up to 100 profiles, and fills as many of the **9** button slots as
  the current grid size (**Grid Rows** x **Grid Cols**, 1–3 each,
  default 2x3 = 6, see "Configuration: runtime settings" above) allows
  per page, hiding any slots beyond that page's profile count. The
  bridge currently exposes exactly one profile ("default"); more
  profiles show up automatically, paged via the footer's `<`/`>`
  buttons once they no longer fit on one page.
- Shows a three-state, color-coded Bridge indicator in the top bar
  (Issue #9 item B6), polled every 15s — only Bridge URL is required
  for this, not the token — plus a "WiFi: OK/--" indicator next to it.
  The indicator now calls `GET /ready` instead of the old plain
  `GET /health`, so it reflects scan-bridge's actual readiness contract
  (`components/scan-bridge/internal/api/ready.go`) rather than a bare
  process-up/-down check:
  - **green "Bridge: OK"** — `200`: profiles are loaded and
    sane-runtime answered its own health check. The panel is actually
    ready to serve a scan.
  - **blue "Scanner: offline"** — `503`
    `{"error":"sane_runtime_unreachable"}`: the bridge process itself
    answered, but the scanner backend specifically did not. Its own
    color on purpose — it points at "restart sane-runtime", not "the
    bridge is down".
  - **red "Bridge: ERR"** — any other `503` (e.g.
    `no_profiles_loaded`) or an unexpected status: the bridge answered
    but is not ready for another reason.
  - **red "Bridge: --"** — a network-level failure (timeout, DNS,
    connection refused): the bridge did not answer at all, same
    network-error case the old `/health` check had.
  - **red "Bridge: not set"** — no Bridge URL configured yet, so no
    request was even attempted.
- Tapping a profile button sends `POST /scan {"profile": "<name>"}` with
  the bearer token, disables all 9 button slots plus the `<`/`>` paging
  buttons and turns the status LED amber while the request is in
  flight, then shows the result and
  **resets the LED and status label back to idle after a delay in every
  case** — green flash + "Done: `<profile>`" on `200` (2s), "No paper in
  feeder" (amber-red) on `422` (4s), "Unauthorized" on `401`/`403` (4s),
  "Unknown profile" on `404` (4s), a generic "Error `<code>`" otherwise
  (4s), and "Bridge unreachable" on a network-level failure (timeout,
  DNS, connection refused; 4s). Earlier revisions only reset after
  success and left error states on screen indefinitely.
- Shows a centered spinner while a scan is in flight (Issue #9 item
  B5), on top of the button grid, from the moment `POST /scan` is fired
  until the result is known. The "Scanning: `<profile>`..." status
  label and amber LED already persisted for the whole (synchronous,
  blocking) request duration before this — the spinner does not fix a
  missing state, it makes an already-correct in-flight state visually
  louder than a 20px footer label. Hidden again in every terminal
  branch, the same discipline as the LED/label reset above.

What it deliberately does **not** do yet (see Issue #9 for phases beyond
D/E):

- **No dedicated portrait UI.** The button grid itself (1x1 up to 3x3,
  configurable at runtime, default 2x3 as described in Issue #9's
  mockups) resizes correctly for either orientation as of B4 — see
  "Display orientation" below — but the header and footer rows are
  still fixed at the 320-wide landscape layout regardless of
  `screen_width`/`screen_height`, so a portrait build's header/footer
  will not fill (or may overflow) a 240px-wide screen. A dedicated
  portrait page layout is a later option.
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
- **Per-unit touch calibration still needs a re-flash** (see "Touch
  calibration" above) — there is no on-device calibration wizard. This
  is the one setup step the browser installer alone cannot finish.
- **LVGL buffer sizing has not been hardware-verified.** The board has
  no PSRAM, and the LVGL/display defaults used here (no explicit
  `lvgl: buffer_size:` override) have not been confirmed against real
  RAM headroom on physical hardware — if flashing reports a memory
  allocation failure, tuning the buffer size is the first thing to try.
- **Grid size and paging (B1/B2) are not hardware-verified either.**
  The templatable `x`/`y`/`width`/`height` on the 9 button slots and
  the paging buttons compile and pass `esphome config`/`esphome
  compile` against this exact ESPHome version, but nothing beyond
  that — see "Hardware verification status" below.
- **Display orientation (B4) is config-verified only, not
  hardware-verified.** Both the published landscape default and the
  portrait `packages:` override (see "Display orientation" above) pass
  `esphome config`/`esphome compile`, proving the substitution wiring
  and the `lvgl.rotation`/swapped-`dimensions` pairing are schema-valid
  ESPHome — not that a physical portrait panel renders right-side-up or
  that the guessed-at swapped touch calibration is correct.
- **Scan spinner (B5) is config-verified only, not hardware-verified.**
  `esphome config`/`esphome compile` confirm the widget is schema-valid
  and pass at both orientations, but not that the LVGL spinner animation
  actually renders/spins smoothly on real hardware within this board's
  no-PSRAM LVGL buffer budget.
- **Chain-status indicator (B6) is config-verified only, not
  hardware-verified.** `esphome config`/`esphome compile` confirm the
  three `text_color` values resolve as intended (green `0x00A000`, blue
  `0x0080FF`, red `0xE00000`) and that the `GET /ready` body-parsing
  lambda is schema-valid, but nothing beyond that — a stub or emulator
  hitting the real bridge is what would confirm the panel actually
  turns blue when `sane-runtime` is stopped and green again once it
  comes back, not just that the YAML compiles.

## Hardware verification status

**Not yet flashed to real hardware.** The pin map, SPI bus split,
display/touch options, and backlight/LED wiring in `cyd-scan-panel.yaml`
are cross-verified against multiple cited working ESP32-2432S028R
ESPHome configurations, and (where `esphome` is available) the config
passes `esphome config`/`esphome compile` — see the PR that introduced
the secret-free build for the exact command and output. None of that is
a substitute for flashing it to an actual board, and the browser
installer path in particular (Improv-over-Web-Serial handoff, the
on-device dashboard actually being reachable and usable from a phone or
laptop on the LAN) has **no** verification beyond "the firmware compiles
and the manifest is well-formed" — it needs a real flash to confirm.
Please report back (open a follow-up issue referencing #9) once you've
flashed it — in particular the touch calibration values, whether the
LVGL memory budget is fine as-is, whether all 9 button slots render and
respond correctly at every grid size from 1x1 to 3x3, whether the `<`/
`>` paging buttons correctly page through more profiles than fit on one
page, whether the ESP Web Tools + Improv + dashboard flow works
end-to-end from a browser, and — if you build the portrait override
(see "Display orientation" above) — whether `lvgl.rotation: 90` and the
swapped `display.dimensions` actually render right-side-up and whether
the flipped touch calibration needs the sign convention this firmware
guesses at, or the opposite one. Also worth confirming for the
chain-status indicator (B6): that the Bridge label actually renders
green/blue/red distinguishably on the physical panel (not just that the
merged config resolves the right hex values), and that stopping/
restarting `sane-runtime` on the bridge host flips the indicator between
green and blue within one 15s poll cycle.
