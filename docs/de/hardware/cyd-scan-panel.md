# CYD-Scan-Panel

Ein Touch-Panel für Wand oder Schreibtisch, das die im Bridge
hinterlegten Scan-Profile auflistet und einen Scan per Tipp auslöst — das
hardware-unabhängige Gegenstück zur nur teilweisen Tastenunterstützung
des [Kodak ScanMate i1120](kodak-scanmate-i1120.md). Der vollständige
Entwurf steht in
[Issue #9](https://github.com/strausmann/paperless-scan-bridge/issues/9),
und alles Folgende ausführlicher in
[`firmware/esp32-panel/README.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/firmware/esp32-panel/README.md)
— einschließlich der Sicherheitsabwägungen, die diese Firmware eingeht,
um als ein einziges öffentliches Binary verteilbar zu sein.

| | |
| --- | --- |
| Board | ESP32-2432S028R („Cheap Yellow Display" / CYD) |
| Anzeige | 2,8" 320x240 ILI9341 TFT, resistiver XPT2046-Touch |
| Firmware | ESPHome, ohne eingebettete Geheimnisse — siehe unten |
| Hardware-Verifikation | Gegen echte Hardware geprüft; offene Punkte siehe README |

## Installieren und verwalten

Flashen und Bluetooth-Einrichtung haben eigene Seiten, verlinkt oben auf
der Site:

- **[Panel installieren](../install/index.md)** — Firmware direkt aus dem
  Browser über USB flashen (Chrome/Edge, Web Serial).
- **[Panel verwalten](../manage/index.md)** — ein Panel über Bluetooth
  ins WLAN bringen (Chrome/Edge, Web Bluetooth).
- **[Firmware herunterladen](../install/index.md#firmware-herunterladen)**
  — die `.bin` fürs Upload-Formular des Dashboards, der Rückfallweg. Der
  normale Weg läuft inzwischen automatisch: Das Panel holt seine
  Firmware von der Bridge (siehe unten).

Diese Seite ist die Hardware-Referenz dahinter: was das Panel ist, wie es
sich nach der Einrichtung verhält und was es weiterhin nicht kann.

## Einrichtung, Schritt für Schritt

1. **Installieren** — CYD per USB anschließen und von der Seite
   [Panel installieren](../install/index.md) flashen; den seriellen Port
   auswählen, wenn Chrome oder Edge danach fragt.
2. **WLAN (Improv)** — direkt nach dem Flashen führt der Installer im
   selben Browser-Tab durch die Einrichtung per
   [Improv Wi-Fi](https://www.improv-wifi.com/): Netz auswählen,
   Passwort eingeben. Das geht auch später über Bluetooth von der Seite
   [Panel verwalten](../manage/index.md). Findet das Panel kein
   bekanntes Netz, öffnet es ersatzweise einen Hotspot
   `Scan Panel Setup` (Passwort `panelsetup`) mit Captive Portal.
3. **Das panel-eigene Dashboard öffnen** — sobald es eine IP hat (im
   Router nachsehen oder im Protokoll der ESP Web Tools),
   `http://<panel-ip>/` in einem Browser im selben Netz aufrufen.
4. **Bridge URL und Bridge Token setzen** — auf diesem Dashboard die
   Adresse der scan-bridge eintragen (Host und Port, unter dem Sie
   `scan-bridge` veröffentlichen, etwa
   `http://<ihr-bridge-host>:18080`) sowie ihren Bearer-Token — den
   Klartext, dessen SHA-256-Summe in Ihrem `auth.token_hash` steht und
   der in Ihren Passwortspeicher gehört, nicht in dieses Repository.
   Beides übersteht einen Neustart, nichts davon braucht ein erneutes
   Flashen. Sobald es gesetzt ist, wird die Bridge-Anzeige in der oberen
   Leiste **grün „Bridge: OK"**, sobald die Bridge auf `GET /ready` mit
   `200` antwortet — Profile geladen und Scanner-Backend erreichbar.
   **Blau „Scanner: offline"** heißt, die Bridge selbst hat geantwortet,
   nur das Scanner-Backend nicht; **rot** deckt jeden anderen Fall ab
   (falsche URL, Bridge aus, fehlkonfiguriert). Die vollständige
   Zustandstabelle steht im Abschnitt „Scope and known limitations" der
   Firmware-README.
5. **Rastergröße (optional)** — dasselbe Dashboard hat **Grid Rows** und
   **Grid Cols** (je 1–3, Standard 2x3 — das heutige feste
   Sechs-Tasten-Layout, unverändert, solange Sie nichts ändern). Erhöhen
   Sie eines von beiden, um mehr Profile gleichzeitig zu sehen, bis zu
   3x3 = 9. Hat die Bridge mehr Profile, als auf eine Seite passen,
   blättern die Schaltflächen `<` und `>` in der Fußzeile durch den
   Rest.
6. **Touch-Kalibrierung** — der eine Schritt, den der Browser-Installer
   nicht übernehmen kann. Jedes Panel ist anders; siehe den Abschnitt
   „Touch calibration" der README (braucht ein lokales `esphome` und ein
   erneutes Flashen, über USB oder OTA).

## Bekannte Grenzen

!!! warning "Scans über 55 Sekunden lassen sich vom Panel aus nicht starten"

    `http_request` ist synchron: `POST /scan` hält die Hauptschleife des
    Panels über den gesamten Scan, und eine Schleife, die nicht
    innerhalb des Task-Watchdog-Fensters zurückkehrt, startet das Gerät
    neu. ESPHome deckelt diesen Watchdog bei 60 Sekunden, deshalb liegt
    der Client-Timeout des Panels bei 55 — bewusst darunter, damit der
    Client zuerst aufgibt und das Panel überlebt, um einen Fehler zu
    melden, statt mitten in der Anfrage getötet zu werden.

    Ein längerer Scan meldet auf dem Panel **Bridge unreachable**. Der
    Scan selbst läuft trotzdem durch: Die Bridge hat die Anfrage bereits
    und stört sich nicht daran, dass der Aufrufer weg ist. Drei der vier
    ausgelieferten Profile erlauben 180, 300 und 600 Sekunden und sind
    vom Panel aus vorerst nicht erreichbar.

    Die Lösung sind die `/jobs`-Endpunkte (Phase 1.4) — Scan auslösen,
    Ergebnis pollen, Schleife nie halten. ESPHomes `http_request` hat
    keinen asynchronen Modus als Alternative.

Kein eigenes Hochformat-Layout (das Tastenraster passt sich zwar beiden
Ausrichtungen an, Kopf- und Fußzeile sind aber weiterhin auf Querformat
festgelegt — siehe „Display orientation" in der Firmware-README), keine
Abfrage laufender Aufträge, kein Kalibrierungsassistent auf dem Gerät und
kein LVGL-Speicherbudget gegen Hardware belegt.

!!! warning "Zwei Punkte oben sind nur gegen die Konfiguration geprüft"

    **Rastergröße über 2x3 hinaus und das Blättern** (Schritt 5) bestehen
    `esphome config` und `esphome compile` — mehr nicht. Dass ein 3x3-Raster
    auf dem Glas lesbar bleibt und die `<`/`>`-Schaltflächen sauber
    umblättern, ist am Gerät nicht nachgewiesen.

    **Die dreifarbige Bridge-Anzeige** (Schritt 4) ebenso: Die Farbwerte
    lösen nachweislich wie beabsichtigt auf (grün `0x00A000`, blau
    `0x0080FF`, rot `0xE00000`) und die Lambda, die den `/ready`-Körper
    liest, ist schema-gültig. Ob die Anzeige tatsächlich blau wird, wenn
    `sane-runtime` steht, und wieder grün, wenn es zurückkommt, hat
    niemand ausprobiert.

    Die Firmware-README führt beides unter „Scope and known limitations"
    mit derselben Einschränkung.

## Updates kommen von der Bridge

Sobald das Panel seine **Bridge URL** kennt, fragt es dort nach neuerer
Firmware und meldet sie auf dem eigenen Dashboard als **Firmware
Update**; die Schaltfläche **Check for Update** fragt sofort nach.

Der Takt richtet sich danach, was das Panel weiß: **jede Minute**,
solange noch keine Prüfung erfolgreich war (der Zustand **UNKNOWN**, den
etwa eine falsche Bridge URL hinterlässt), danach **alle 30 Minuten**.
Jede Prüfung ist eine kleine Anfrage an die eigene Bridge, nie an
GitHub.

Das Panel spricht nie mit GitHub. Es kann es nicht: Neben WLAN,
Bluetooth-Stack, LVGL und dem eigenen Dashboard bleibt kein Speicher
mehr, um eine TLS-Sitzung anzulegen (`MBEDTLS_ERR_SSL_ALLOC_FAILED`) —
eine Speichergrenze, kein Zertifikatsproblem. Also fragt `scan-bridge`
alle fünf Stunden bei GitHub nach, lädt das Release, **prüft es gegen
die `SHA256SUMS` des Releases** und veröffentlicht es erst danach unter
`http://<ihre-bridge>:18080/firmware/manifest.json`. Eine Datei mit
falscher Prüfsumme wird verworfen, die Bridge liefert weiter aus, was
sie hatte — das Manifest kündigt also nie einen Build an, den die Bridge
nicht herausgeben kann.

Geprüft wird automatisch, **installiert nur auf ausdrücklichen Klick**.
Die Begründung steht in ADR 0024 und ADR 0025. Das Upload-Formular im
Dashboard bleibt als Rückfallweg —
[die Datei dafür gibt es hier](../install/index.md#firmware-herunterladen).

Die vollständige, aktuelle Liste führt die Firmware-README unter „Scope
and known limitations".
