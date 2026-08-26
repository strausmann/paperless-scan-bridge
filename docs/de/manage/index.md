# Panel verwalten

Ein Panel über **Bluetooth** ins WLAN bringen — für ein frisch
geflashtes Panel oder eines, dessen Netz verschwunden ist, ohne Kabel.

## Über Bluetooth verbinden

<improv-wifi-launch-button>
  <button slot="activate">Panel über Bluetooth verbinden</button>
  <span slot="unsupported">
    Dieser Browser kann kein Bluetooth. Web Bluetooth gibt es nur in
    Chrome oder Edge (Desktop oder Android) — Firefox und Safari
    implementieren es nicht, und auf iOS kann es kein Browser.
  </span>
  <span slot="not-allowed">
    Bluetooth braucht einen sicheren Kontext (HTTPS oder localhost).
    Diese Seite wird über HTTPS ausgeliefert — wenn Sie das hier sehen,
    stimmt etwas nicht.
  </span>
</improv-wifi-launch-button>

<script type="module" src="/javascripts/improv-wifi/launch-button.js"></script>

Das Panel aus der Geräteliste des Browsers auswählen, dann ein Netz
wählen und das Passwort eingeben.

!!! important "Das Panel ist nur auffindbar, solange es kein WLAN hat"

    ESPHome startet den Improv-BLE-Dienst erst `wifi_timeout` nach dem
    Wegfall der WLAN-Verbindung — standardmäßig 90 Sekunden — und
    beendet ihn wieder, sobald das Panel in einem Netz ist. Ein frisch
    geflashtes Panel oder eines ohne erreichbares Netz taucht also in
    der Geräteliste auf; ein normal verbundenes Panel **nicht**.

    Um ein verbundenes Panel neu einzurichten, nutzen Sie entweder sein
    eigenes Dashboard über das Netz (siehe unten), oder Sie nehmen ihm
    das aktuelle Netz weg und warten den Timeout ab.

!!! warning "Was diese Seite kann — und was nicht"

    Das hier ist **ausschließlich WLAN-Einrichtung**. Das
    [Improv-Protokoll](https://www.improv-wifi.com/) überträgt
    Netzwerk-Zugangsdaten und einen Statuscode — sonst nichts. Es kann
    den Zustand des Panels nicht auslesen und weder Bridge-URL noch
    Token oder Grid-Größe setzen.

    Diese Dinge liegen im Dashboard des Panels selbst (nächster
    Abschnitt), erreichbar sobald WLAN steht. Warum eine umfangreichere
    Bluetooth-Fläche noch nicht existiert, steht unten.

## Danach: das Dashboard des Panels

Sobald das Panel eine IP hat, `http://<panel-ip>/` öffnen. Dieses
Dashboard steckt in der Firmware selbst und braucht deshalb keinen
Internetzugang — nur einen Browser im selben Netz. Es kann alles, was
Improv nicht kann:

- **Status** — WLAN-Zustand, die dreistufige Bridge-Anzeige und die
  Profilliste, die live über `GET /profiles` geholt wird.
- **Konfiguration** — Bridge-URL, Bridge-Token, Grid-Zeilen und -Spalten.
- **WLAN** — Neueinrichtung, falls Sie Bluetooth nicht nutzen möchten.

## Kein Netz, kein Bluetooth?

Findet das Panel kein bekanntes WLAN, öffnet es einen eigenen Access
Point `Scan Panel Setup`, Passwort `panelsetup`. Verbinden Sie sich
damit, das Captive Portal führt zum selben Dashboard.

!!! note "Dieses Hotspot-Passwort ist öffentlich"

    Es steckt in einem Binary, das jede und jeder von dieser Site laden
    kann, ist also auf allen Geräten identisch. Es schützt nichts außer
    dem temporären Setup-Hotspot — siehe „Security model" in der
    Firmware-README.

## Warum es keine vollständige Bluetooth-Steuerung gibt

Status lesen und Konfiguration schreiben über BLE — so wie diese Seite
es mit WLAN tut — bräuchte einen eigenen GATT-Dienst in der Firmware,
nicht Improv. Das ist eine bewusste offene Entscheidung, kein
Versehen: Das Panel läuft mit `authorizer: none`, hat also keinen
Bestätigungsknopf. Eine unauthentifizierte BLE-Konfigurationsfläche
würde jeder Person in Funkreichweite erlauben, das Bearer-Token der
Bridge zu lesen oder zu ersetzen — also den Auslöser für Scans zu
übernehmen.

Die Firmware-Arbeit wartet deshalb auf ein Autorisierungskonzept.
Festgehalten ist das als Architekturentscheidung 0022 im Repository.

## Voraussetzungen

| | |
| --- | --- |
| **Browser** | Chrome oder Edge — Desktop oder Android. Web Bluetooth gibt es nicht in Firefox, Safari oder auf iOS. |
| **Reichweite** | Bluetooth LE, also derselbe Raum. Das Panel muss eingeschaltet sein. |
