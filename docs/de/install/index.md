# Panel installieren

Die Firmware des CYD-Scan-Panels lässt sich direkt aus dieser Seite
flashen — ohne Toolchain, ohne Download, ohne Kommandozeile. Das Binary
baut die CI aus
[`firmware/esp32-panel/cyd-scan-panel.yaml`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/cyd-scan-panel.yaml)
und veröffentlicht es zusammen mit dieser Site — es passt also immer zu
der Seite, die Sie gerade lesen. Welchen Commit Sie flashen, zeigt der
Versions-String im Installer.

## 1. Flashen

Panel per USB an diesen Rechner anschließen, dann:

<esp-web-install-button manifest="/firmware/manifest.json">
  <button class="md-button md-button--primary" slot="activate">
    CYD-Scan-Panel-Firmware installieren
  </button>
  <span slot="unsupported">
    Dieser Browser kann nicht flashen. Web Serial gibt es nur in Chrome
    oder Edge auf dem Desktop — Firefox und Safari implementieren es
    nicht, und auf iOS kann es kein Browser.
  </span>
  <span slot="not-allowed">
    Flashen braucht einen sicheren Kontext (HTTPS oder localhost). Diese
    Seite wird über HTTPS ausgeliefert — wenn Sie das hier sehen, stimmt
    etwas nicht.
  </span>
</esp-web-install-button>

Beim Nachfragen den seriellen Port auswählen. Der Installer löscht den
Chip, schreibt das Factory-Image und bietet danach im selben Tab die
WLAN-Einrichtung an.

!!! info "Warum ein einziges öffentliches Binary hier unbedenklich ist"

    Die Firmware enthält keine WLAN-Zugangsdaten, keine Bridge-URL und
    kein Token. Alles Deployment-Spezifische wird zur Laufzeit gesetzt
    und im Flash des Panels abgelegt. Genau das macht einen
    Browser-Installer überhaupt möglich — siehe „Secret-free firmware"
    in der Firmware-README.

## 2. Ins WLAN bringen

Direkt nach dem Flashen führt der Installer durch
[Improv Wi-Fi](https://www.improv-wifi.com/). Wenn Sie das überspringen
oder das Panel später sein Netz verliert, gibt es zwei weitere Wege:

- **[Bluetooth](../manage/index.md)** — die Verwalten-Seite richtet
  WLAN über BLE ein, ganz ohne Kabel.
- **Setup-Hotspot** — findet das Panel kein bekanntes Netz, öffnet es
  einen Access Point `Scan Panel Setup` (Passwort `panelsetup`) mit
  Captive Portal.

## 3. Auf die Bridge zeigen lassen

`http://<panel-ip>/` öffnen — das Panel liefert sein eigenes Dashboard
aus, das in der Firmware steckt und deshalb ohne Internetzugang
funktioniert. Dort einstellen:

| Einstellung | Wert |
| --- | --- |
| **Bridge URL** | Wo `scan-bridge` erreichbar ist, z. B. `http://<bridge-host>:18080` |
| **Bridge Token** | Der Klartext, dessen SHA-256-Digest in `auth.token_hash` steht |
| **Grid Rows / Cols** | Optional, je 1–3 (Standard 2x3) |

Beides übersteht einen Neustart, nichts davon braucht ein erneutes
Flashen. Sobald es gesetzt ist, wird die obere Leiste grün
**„Bridge: OK"**, sobald die Bridge auf `GET /ready` mit `200` antwortet.

Die vollständige Anleitung, die Zustandstabelle der Anzeige und die
bekannten Grenzen stehen in der englischen Doku:
[CYD scan-control panel](/hardware/cyd-scan-panel/).

## Später aktualisieren

Nach dem Flashen brauchen Sie diese Seite nicht mehr. Das Panel prüft
`manifest.json` — dieselbe Datei, die der Button oben nutzt — alle sechs
Stunden und meldet einen neueren Build als **Firmware Update** auf seinem
eigenen Dashboard unter `http://<panel-ip>/`. Installieren ist dort ein
Klick; kein Dateidialog, kein Kabel.

Das Upload-Formular weiter unten auf diesem Dashboard funktioniert
weiterhin und ist der Rettungsweg, wenn das Panel das Internet nicht
erreicht.

!!! warning "Was ein Over-the-Air-Update schützt — und was nicht"

    Das Manifest trägt die MD5-Summe der Firmware, und das Panel prüft
    sie **beim Schreiben**. Ein abgebrochener oder veränderter Download
    wird verworfen, die laufende Firmware bleibt — ein unterbrochenes
    Update kann das Panel also nicht unbrauchbar machen.

    Was fehlt, ist die Prüfung des TLS-Zertifikats: Die Firmware läuft
    mit abgeschaltetem `verify_ssl`. Wer sich aktiv in den Netzwerkpfad
    schaltet, könnte deshalb ein gefälschtes Manifest **und** eine dazu
    passende Binärdatei ausliefern, und die MD5-Prüfung ginge durch.

    Deshalb **meldet** das Panel Updates, installiert aber nie eines von
    selbst: Das entscheidende Zeitfenster ist der Moment, in dem Sie auf
    Installieren klicken — nicht alle sechs Stunden. Ob sich die
    Zertifikatsprüfung für diesen Build einschalten lässt, ist eine offene
    Frage; sie ist in ADR 0023 im Repository festgehalten und muss an
    echter Hardware getestet werden, nicht an einer Konfiguration, die
    bloß validiert.

## Voraussetzungen

| | |
| --- | --- |
| **Browser** | Chrome oder Edge, Desktop. Web Serial gibt es nicht in Firefox, Safari oder auf iOS. |
| **Kabel** | Ein USB-**Datenkabel** — reine Ladekabel liefern keinen seriellen Port. |
| **Board** | ESP32-2432S028R („Cheap Yellow Display") |
