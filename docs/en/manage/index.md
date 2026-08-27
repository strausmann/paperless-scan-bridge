# Manage the scan panel

Connect to a panel over **Bluetooth** to get it onto Wi-Fi — for a
freshly flashed panel, or one whose network has disappeared, without
plugging in a cable.

!!! important "The panel is only findable while it has no Wi-Fi"

    ESPHome starts the Improv BLE service `wifi_timeout` after the Wi-Fi
    connection drops — 90 seconds by default — and stops it again once
    the panel is back on a network. A freshly flashed panel, or one whose
    network is gone, will therefore appear in the browser's device list;
    a panel that is happily connected will **not**.

    To re-provision a connected panel, either use its own dashboard over
    the network (below), or take its current network away and wait out
    the timeout.

## Connect over Bluetooth

!!! important "The panel is only findable while it has no Wi-Fi"

    ESPHome starts the Improv BLE service `wifi_timeout` after the Wi-Fi
    connection drops — 90 seconds by default — and stops it again once
    the panel is back on a network. A freshly flashed panel, or one whose
    network is gone, will therefore appear in the browser's device list;
    a panel that is happily connected will **not**.

    To re-provision a connected panel, either use its own dashboard over
    the network (below), or take its current network away and wait out
    the timeout.

<improv-wifi-launch-button>
  <button class="md-button md-button--primary" slot="activate">
    Connect to a panel over Bluetooth
  </button>
  <span slot="unsupported">
    This browser can't do Bluetooth. Web Bluetooth needs Chrome or Edge
    (desktop or Android) — Firefox and Safari do not implement it, and
    neither does any browser on iOS.
  </span>
  <span slot="not-allowed">
    Bluetooth needs a secure context (HTTPS or localhost). This page is
    served over HTTPS, so if you see this, something is off.
  </span>
</improv-wifi-launch-button>

Pick the panel from the browser's device list, then choose a network and
enter its password.

!!! warning "What this page can and cannot do"

    This is **Wi-Fi provisioning only**. The
    [Improv protocol](https://www.improv-wifi.com/) carries network
    credentials and a status code — nothing else. It cannot read the
    panel's state, and it cannot set the Bridge URL, the bearer token or
    the grid size.

    Those live in the panel's own dashboard (next section), which is
    reachable once Wi-Fi is up. A richer Bluetooth surface — live status
    and full configuration without a network — is
    [tracked separately](#a-fuller-bluetooth-surface).

## Then: the panel's own dashboard

Once the panel has an IP, open `http://<panel-ip>/`. That dashboard is
bundled into the firmware itself, so it needs no internet access — only
a browser on the same network. It carries everything Improv cannot:

- **Status** — Wi-Fi state, the three-state bridge indicator, and the
  live profile list pulled from `GET /profiles`.
- **Configuration** — Bridge URL, Bridge Token, grid rows and columns.
- **Wi-Fi** — re-provisioning, if you'd rather not use Bluetooth.

## No network, no Bluetooth?

With no known Wi-Fi in range the panel opens its own access point,
`Scan Panel Setup`, password `panelsetup`. Join it and the captive
portal takes you to the same dashboard.

!!! note "That hotspot password is public"

    It is compiled into a binary anyone can download from this site, so
    it is identical on every unit. It protects nothing but the temporary
    setup hotspot — see the firmware README's "Security model".

## A fuller Bluetooth surface

Reading status and writing configuration over BLE — the way this page
handles Wi-Fi — needs a custom GATT service in the firmware, not Improv.
That is a deliberate open decision rather than an oversight: the panel
runs `esp32_improv` with `authorizer: none`, meaning it has no physical
confirm button. Exposing the bridge's bearer token over an
unauthenticated BLE service would let anyone within radio range read or
replace the credential that triggers scans.

So the firmware work waits on an authorization model. Progress is
tracked in the repository's issues.

## Requirements

| | |
| --- | --- |
| **Browser** | Chrome or Edge — desktop or Android. Web Bluetooth is not available in Firefox, Safari, or on iOS. |
| **Range** | Bluetooth LE, so the same room. The panel must be powered. |
