# Kodak ScanMate i1120

Das Referenzgerät dieses Projekts: ein Tischscanner von 2009 ohne
Linux-Unterstützung des Herstellers, angesteuert über das SANE-Backend
`avision`.

| | |
| --- | --- |
| USB-ID | `040a:6013` |
| SANE-Backend | `avision` |
| Kompatibilitätsstufe | A — Referenz |
| Verifiziert | 2026-04, erneut 2026-04-30 |
| Quellen | Flachbett, ADF simplex, `ADF Duplex` |
| Auflösung | 75–600 dpi |
| Geschwindigkeit (Herstellerangabe) | 20 Seiten/min simplex, 10 duplex über USB 2.0 |

## Zustand des Backends

Das Backend `avision` gilt upstream seit 2020 als nicht mehr gepflegt,
funktioniert für dieses Gerät aber zuverlässig auf jeder aktuellen
Linux-Distribution ab Kernel 5.15. Der Verifikationslauf vom 30.04.2026
lief unter Ubuntu Server 24.04 mit der 6.8er-Kernelreihe; das Ergebnis
hängt nicht am Kernel.

Ein USB-3.0-Anschluss macht den Scanner nicht schneller — das Gerät
selbst ist USB 2.0.

## Tasten am Gerät: nur teilweise unterstützt

Das ist das Wichtigste, was man über dieses Gerät wissen muss, und es hat
die Architektur des Projekts unmittelbar geprägt.

### Was funktioniert

Das **Funktionsrad mit LCD-Anzeige** (der Wähler 1–9) erzeugt
SANE-Ereignisse auf der nur lesbaren Option `--message`, als
Zeichenketten der Form `<n>:button1`. Der mitgelieferte
`function_knob`-Filter von scanbd fängt sie bei 250 ms Abtastrate
zuverlässig ab — über alle neun Positionen geprüft.

### Was nicht funktioniert

Die **Starttaste erzeugt kein für SANE sichtbares Ereignis.** Weder über
die Optionen des `avision`-Backends noch über direkte Auflistung mit
`scanimage -A` noch über scanbd-Abtastung. In einer 60-sekündigen
scanbd-Aufzeichnung erzeugten wiederholte Druckvorgänge auf die
Starttaste **null** Ereignisse, während Drehungen am Funktionsrad 21
erzeugten.

Der **Papiersensor des Einzugs ist ebenso undurchsichtig.** Papier
einzulegen oder zu entnehmen ändert weder die Ausgabe von
`scanimage -A` noch das Feld `--message` — geprüft per Momentaufnahmen-
Vergleich und laufender scanbd-Abtastung. Das einzige papierbezogene
Signal, auf das sich ein Aufrufer verlassen kann, ist
`SANE_STATUS_NO_DOCS`, zurückgegeben als Fehler während eines
tatsächlichen Scanversuchs.

!!! warning "Kein automatisches „Papier rein, Scan startet\""

    Der ursprünglich geplante Ablauf — Papier einlegen, Scanner startet
    von selbst — ist auf diesem Gerät über SANE nicht erreichbar.
    `scanbd` ist als direkte Folge aus dem Entwurf der Phase 1.2
    geflogen.

Die aufgezeichneten Protokolle, die Negativbefunde und die offene Frage,
ob es ein Signal auf USB-Ebene gibt, das das Backend schlicht nicht
auswertet, stehen in
[`docs/research/scanner-hardware-events.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/docs/research/scanner-hardware-events.md).
Eine Untersuchung per USB-Mitschnitt wird in
[Issue #7](https://github.com/strausmann/paperless-scan-bridge/issues/7)
verfolgt.

### Praktische Folge

Auslösung per HTTP-Webhook und Zigbee-Fernbedienungen über Home Assistant
sind auf diesem Gerät die Hauptwege. Das Funktionsrad funktioniert über
die `function_knob`-Zuordnung von scanbd als **zweiter**
Hardware-Auslöser. Die Starttaste funktioniert heute nicht.

## Diagnose über NVRAM

Das `avision`-Backend stellt eine nur lesbare Zeichenkette
`--nvram-values` bereit. Sie enthält Modell, Firmware-Version,
Seriennummer, Herstellungsdatum, Datum des ersten Scans sowie die
Gesamtzähler für Pad- und ADF-Scans. Der Monitoring-Stack liest das für
Betriebs-Dashboards aus — es ist der billigste Weg, die Frage „wie viele
Seiten hat diese Walze gesehen?" zu beantworten.

## Papierhandhabung und Wartung

- Zuverlässig bei üblichen Bürogrammaturen, 60–105 g/m².
- Schwereres Material — Karton, Hochglanzfotos — muss von Hand durch den
  vorderen Schlitz geführt werden.
- Einzugskapazität: 50 Blatt nominal, 30 Blatt zuverlässig.
- Walzen monatlich mit einem fusselfreien Tuch und Isopropylalkohol
  reinigen.
