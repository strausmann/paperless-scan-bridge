# Hardware

Jeder Scanner mit funktionierendem SANE-Backend sollte laufen. Das
„sollte" trägt in diesem Satz einiges: Die Qualität der SANE-Backends
schwankt erheblich, und die Unterstützung für Hardware-Tasten ist
besonders gerätespezifisch — oft fehlt sie ganz.

## Kompatibilitätsstufen

| Stufe | Bedeutung |
| --- | --- |
| **A — Referenz** | Der Maintainer besitzt das Gerät und testet dagegen |
| **B — Verifiziert** | Jemand hat die vollständige Verifikation durchlaufen und berichtet |
| **C — Läuft laut Bericht** | Jemand sagt, es funktioniert; kein strukturierter Bericht |
| **D — Vermutlich kompatibel** | Ungetestet, aber das SANE-Backend spricht dafür |
| **X — Bekannt inkompatibel** | Dokumentiert nicht funktionierend, mit Begründung |

## Verifizierte Scanner

| Modell | USB-ID | SANE-Backend | Stufe | Anmerkungen |
| --- | --- | --- | --- | --- |
| [Kodak ScanMate i1120](kodak-scanmate-i1120.md) | `040a:6013` | `avision` | A | Referenzgerät. ADF + Duplex, 75–600 DPI. Hardware-Tasten nur teilweise nutzbar. |

Die vollständige Liste — inklusive bekannt inkompatibler Geräte,
ungetesteter Kandidaten, Auslöse-Hardware und Speicher-Backends — steht
in
[`HARDWARE_COMPATIBILITY.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/HARDWARE_COMPATIBILITY.md).

## Auslöse-Hardware

Kein Scanner, sondern ein Begleitgerät: Das CYD-Scan-Panel ist ein
Touchscreen, der Scan-Profile auflistet und einen Scan über HTTP
auslöst — direkt aus dem Browser flashbar.

- [CYD-Scan-Panel](cyd-scan-panel.md) — Hardware-Referenz, Einrichtung
  Schritt für Schritt und bekannte Grenzen
- [Panel installieren](../install/index.md) — Firmware flashen
- [Panel verwalten](../manage/index.md) — WLAN über Bluetooth
  einrichten

## Einen eigenen Scanner testen

Die Verifikation hat vier Stufen:

1. **SANE-Erkennung** — sieht ein einmalig gestarteter SANE-Container
   das Gerät auf dem USB-Bus?
2. **Erster Scan** — erzeugt `scanimage` eine brauchbare Seite?
3. **Bridge-Integration** — fährt der Stack das Gerät Ende zu Ende an?
4. **Hardware-Tasten** (optional) — erreichen Tastendrücke den Host?

Die genauen Befehle je Stufe stehen in Abschnitt 10 von
`HARDWARE_COMPATIBILITY.md`.

## Einen Bericht beisteuern

Hardware-Kompatibilitätsberichte sind die willkommenste Art von Pull
Request. Ein Gerät ergänzen:

1. Zeile in `HARDWARE_COMPATIBILITY.md` hinzufügen
2. udev-Regel in `deploy/udev/99-paperless-scan-bridge.rules` ergänzen
3. Nötige SANE-Konfiguration unter `components/sane-runtime/config/`
   ablegen
4. Modellnotizen als `docs/en/hardware/<hersteller>-<modell>.md`
   schreiben

Negative Ergebnisse sind genauso wertvoll wie positive — ein
dokumentiertes „das geht nicht, und zwar deshalb" erspart der nächsten
Person dieselbe Untersuchung.
