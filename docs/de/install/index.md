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
[CYD scan-control panel](/en/hardware/cyd-scan-panel/).

## Später aktualisieren

Nach dem Flashen brauchen Sie diese Seite eigentlich nicht mehr — lesen
Sie vorher aber den Kasten unten.

Das Upload-Formular auf dem panel-eigenen Dashboard unter
`http://<panel-ip>/` nimmt eine `.bin` entgegen und funktioniert heute.
Die Konfiguration bleibt dabei erhalten: WLAN, Bridge URL, Bridge Token
und die Rastergröße liegen in einer eigenen Flash-Partition, die ein
Update nicht anfasst. Nur ein USB-Flash von dieser Seite löscht sie,
weil der Installer den gesamten Chip leert.

### Firmware herunterladen

**[:material-download: cyd-scan-panel.ota.bin](/firmware/cyd-scan-panel.ota.bin)**
— genau diese Datei will das Upload-Formular des Dashboards.

Die andere daneben,
[`cyd-scan-panel.factory.bin`](/firmware/cyd-scan-panel.factory.bin),
ist das vollständige Flash-Image, das die Schaltfläche oben auf dieser
Seite über USB schreibt. Es **löscht die Konfiguration des Panels**.
Nicht ins Upload-Formular legen.

```bash
curl -fsSLO https://scan-bridge.strausmann.de/firmware/cyd-scan-panel.ota.bin
```

Gegen die MD5 prüfen, die das
[Manifest](/firmware/manifest.json) für genau diesen Build angibt:

```bash
md5sum cyd-scan-panel.ota.bin
curl -s https://scan-bridge.strausmann.de/firmware/manifest.json | grep md5
```

!!! warning "Dieser Pfad hält einen Build, und der wird überschrieben"

    Unter `/firmware/` liegt immer der zuletzt von `main` gebaute Stand,
    bezeichnet durch eine Commit-SHA statt durch eine Version. Der
    nächste Doku-Deploy ersetzt ihn. Es gibt hier kein Archiv und keinen
    Weg, über diese URL zu einem älteren Build zurückzukehren.

    Ab dem nächsten Release hängen dieselben Dateien zusätzlich an jedem
    [GitHub-Release][releases] — mit ihrem Versions-Tag benannt, dauerhaft
    aufbewahrt und mit einer `SHA256SUMS` daneben:

    ```bash
    sha256sum -c SHA256SUMS --ignore-missing
    ```

    Ältere Releases tragen keine Firmware-Assets; diese Builds existieren
    nirgends mehr.

| | [`/firmware/`](/firmware/manifest.json) | [GitHub-Release][releases] |
| --- | --- | --- |
| Enthält | den neuesten Build | den Build eines Versions-Tags |
| Benannt nach | Commit-SHA | `v1.2.3` |
| Aufbewahrt | bis zum nächsten Deploy | dauerhaft |
| Prüfen mit | MD5 aus dem Manifest | `SHA256SUMS` |
| Verfügbar | jetzt | ab dem nächsten Release |

  [releases]: https://github.com/strausmann/paperless-scan-bridge/releases/latest

!!! info "Updates kommen von Ihrer Bridge, nicht von dieser Seite"

    Sobald das Panel seine **Bridge URL** kennt, fragt es dort nach
    neuerer Firmware und meldet sie auf dem eigenen Dashboard als
    **Firmware Update**. Daneben gibt es eine Schaltfläche **Check for
    Update**, die sofort nachfragt.

    Wie oft es fragt, hängt davon ab, was es weiß. Solange noch keine
    Prüfung erfolgreich war — der Zustand, den eine falsche Bridge URL
    hinterlässt und den das Dashboard als **UNKNOWN** anzeigt — fragt es
    jede **Minute**, damit sich das Korrigieren der Einstellung fast
    sofort zeigt. Nach der ersten erfolgreichen Prüfung geht es auf alle
    **30 Minuten** herunter. Jede Prüfung ist eine kleine Anfrage an
    Ihre eigene Bridge und erreicht GitHub nie.

    Der Umweg über die Bridge ist keine Vorliebe. Das Panel erreicht diese
    Seite nicht — und GitHub ebenso wenig, und überhaupt nichts über HTTPS:

    ```text
    E esp-tls-mbedtls: mbedtls_ssl_setup returned -0x7F00
    E http_request.update: Failed to fetch manifest
    ```

    `-0x7F00` ist `MBEDTLS_ERR_SSL_ALLOC_FAILED`. Die TLS-Sitzung lässt
    sich nicht anlegen — das Panel trägt bereits WLAN, den
    Bluetooth-Stack, LVGL und sein eigenes Dashboard, und mbedTLS möchte
    obendrauf rund 32 KB. Das ist eine Speichergrenze, kein
    Zertifikatsproblem: Ein eingebettetes Root-Zertifikat verschlimmert
    es, weil der Fehler auftritt, bevor überhaupt ein Zertifikat geprüft
    wird.

    Diesen Teil übernimmt `scan-bridge`. Sie fragt alle fünf Stunden bei
    GitHub nach dem neuesten Release, lädt die Firmware, **prüft sie gegen
    die `SHA256SUMS` des Releases** und bietet sie erst danach unter
    `http://<ihre-bridge>:18080/firmware/manifest.json` an. Eine Datei,
    deren Prüfsumme nicht stimmt, wird verworfen; die Bridge liefert
    weiter das Release aus, das sie bereits hatte. Das Manifest nennt nie
    einen Build, den die Bridge nicht herausgeben kann.

    Danach müssen Sie nichts weiter tun: Der Spiegel ist standardmäßig
    aktiv. Soll Ihre Bridge nicht mit dem öffentlichen Internet sprechen,
    setzen Sie in `config.toml` unter `[firmware]` den Wert
    `enabled = false` und nutzen weiterhin das Upload-Formular.

!!! warning "Ein bereits eingerichtetes Panel braucht einmal ein Update von Hand"

    Das Ganze greift erst ab der Firmware, die es eingeführt hat. Ein
    Panel, das bereits im Einsatz ist, läuft noch mit einem Build, der
    das HTTPS-Manifest dieser Seite abfragt — genau der Abruf, der auf
    dieser Hardware noch nie funktioniert hat. Es wird die neue Version
    also **nie** von selbst bekommen.

    Einmal von Hand einspielen:

    1. [`cyd-scan-panel.ota.bin` herunterladen](#firmware-herunterladen).
    2. Das Dashboard des Panels unter `http://<panel-ip>/` öffnen und das
       Upload-Formular **OTA Update** benutzen. (Oder von dieser Seite
       aus per USB neu flashen — beides geht.)
    3. Die **Bridge URL** setzen, falls noch nicht geschehen.

    Danach findet das Panel Updates von allein.

!!! info "Was ein Update absichert"

    Die Integritätsgarantie ist die MD5-Summe aus dem Manifest, und das
    Panel prüft sie **während des Schreibens**. Ein abgeschnittener oder
    veränderter Download wird verworfen, die laufende Firmware bleibt
    bestehen — ein abgebrochenes Update kann das Panel nicht unbrauchbar
    machen. Zwischen GitHub und Ihrer Bridge kommen die
    SHA-256-Prüfsummen als zweite, stärkere Prüfung hinzu, die das Panel
    selbst nicht leisten kann.

    Zwischen Bridge und Panel läuft der Verkehr als reines HTTP in Ihrem
    eigenen Netz. Wer dort Verkehr umschreiben kann, kann ein gefälschtes
    Manifest und eine dazu passende Binärdatei ausliefern — die
    MD5-Prüfung ginge durch. Deshalb **meldet** das Panel Updates, es
    installiert aber nie eines von selbst: Das entscheidende Zeitfenster
    ist der Moment, in dem Sie auf Installieren drücken, nicht jede
    Prüfung. Die vollständige Begründung steht in ADR 0024 und ADR 0025
    im Repository.

## Voraussetzungen

| | |
| --- | --- |
| **Browser** | Chrome oder Edge, Desktop. Web Serial gibt es nicht in Firefox, Safari oder auf iOS. |
| **Kabel** | Ein USB-**Datenkabel** — reine Ladekabel liefern keinen seriellen Port. |
| **Board** | ESP32-2432S028R („Cheap Yellow Display") |
