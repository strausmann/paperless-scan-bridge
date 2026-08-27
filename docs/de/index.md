---
template: home.html
title: "paperless-scan-bridge — die freihändige Scanner-Pipeline für Paperless-ngx"
hide:
  - navigation
  - toc
hero_title: "Ein Scanner, ein Pi und ein NAS, dem Sie längst vertrauen."
hero_lede: >-
  Vier Geräte, einmal verkabelt. Dokument einlegen, Knopf drücken, rund
  dreißig Sekunden später ist es in Paperless-ngx durchsuchbar.
hero_cta_primary: "Worum es geht"
hero_cta_primary_href: "#worum-es-geht"
hero_cta_start: "Erste Schritte"
hero_cta_install: "Panel installieren"
hero_diagram_alt: >-
  Der Scanner hängt per USB am Raspberry Pi; der Pi schreibt über NFS auf
  ein Synology-NAS; Paperless-ngx liest vom NAS.
hero_label_scanner: "USB-Scanner (ADF)"
hero_chip_consume: "Consume-Ordner"
---

## Worum es geht

Paperless-ngx bringt keine Scanner-Anbindung mit. Für Teile dieses Stacks
gibt es Dutzende bruchstückhafte Anleitungen — SANE auf dem Pi,
Paperless-ngx mit NFS, Scanner-Tasten über scanbd, Zigbee-Automatisierung
in Home Assistant. Was fehlt, ist ein einzelnes Repository, das vom
frischen Pi-Image bis zur produktionsreifen Scan-Pipeline mit Backup,
Monitoring und Härtung durchführt.

Diese Lücke füllt dieses Projekt. Nebenbei ist es die Chronik davon, wie
ein **Kodak ScanMate i1120** — ein sechzehn Jahre alter Tischscanner ohne
moderne Linux-Treiber — zum freihändigen Teil eines Homelabs wird. Was
hier funktioniert, funktioniert für die meisten SANE-fähigen
Einzugsscanner auch.

## Drei Container. Nichts auf dem Host

Die Aufgabe des Pi sind Docker, ein NFS-Mount und udev-Regeln — mehr
nicht. Jede echte Arbeit passiert in einem dieser drei Images, die an
Ihre vorhandene Paperless-ngx-Instanz übergeben.

<div class="mdx-grid" markdown="1">
- **`scan-bridge`** *(Go)* — REST-API, Profil-Dispatch,
  Prometheus-Metriken. Nimmt den Auslöser entgegen — Hardware-Taste,
  Zigbee oder Webhook — und startet den Auftrag.
- **`sane-runtime`** *(Bash + Go)* — SANE-Treiber und udev-Anbindung für
  stabile USB-Gerätepfade. Steuert den physischen Scanner.
- **`scan-processor`** *(Go)* — richtet gerade, filtert leere Seiten,
  montiert das PDF und schreibt es atomar über NFS in das
  Consume-Verzeichnis.
- **`paperless-ngx`** *(upstream)* — holt das PDF aus seinem
  Consume-Ordner, führt OCR aus und verschlagwortet nach Profil. Läuft
  dort, wo es bei Ihnen ohnehin schon läuft.
</div>

## Drei Wege, „scanne das" zu sagen

Der Auslösepfad ist vollständig von der räumlichen Nähe entkoppelt.
Derselbe Mechanismus bedient jemanden, der vor dem Scanner steht, und
jemanden, der zwei Etagen entfernt am Telefon sitzt.

<div class="mdx-grid" markdown="1">
- **Hardware-Taste** *(geplant)* — scanbd fragt die Tasten des Scanners
  selbst ab. Die README von `sane-runtime` führt scanbd bislang
  ausdrücklich als außerhalb des Moduls; dieser Weg ist entworfen, nicht
  gebaut.
- **Zigbee-Fernbedienung** *(geplant)* — ein STYRBAR-Taster über Home
  Assistant, ein Tastenereignis je Scan-Profil.
  `homeassistant/blueprints/` ist bislang nur Gerüst, es gibt noch keine
  Blueprint-Datei.
- **HTTP-Webhook** — heute ein echter, per Bearer-Token geschützter
  `POST /scan` auf dem `scan-bridge`-Daemon: reicht an `sane-runtime`
  weiter, dann an `scan-processor`, dann an die Zustellung — aus einer
  Telefon-Kurzbefehl-Aktion, einem Skript oder jedem anderen System im
  Netz.
</div>

## Die eine nicht verhandelbare Regel

**Container-First, dünner Host.** Am Pi sind ausschließlich drei
Eingriffe erlaubt: Docker samt Compose-Plugin installieren, die
Synology-NFS-Freigabe über `/etc/fstab` einhängen und udev-Regeln unter
`/etc/udev/rules.d/` ablegen. Kein SANE auf dem Host. Kein scanbd auf dem
Host. Keine Sprach-Runtimes auf dem Host.

Die Dokumente landen auf Ihrem eigenen Synology-NAS, Ihre vorhandene
Backup- und Snapshot-Strategie deckt sie also bereits ab. MIT-lizenziert,
keine Cloud-Abhängigkeit, keine Telemetrie.

## Wie der Stand wirklich ist

!!! warning "Projektstand: Der Kern von Phase 1 läuft, das Deployment-Werkzeug fehlt"

    Dies ist ein Home-Lab-Projekt in aktiver Entwicklung. Phase 0
    (Repository, Dokumentation, diese Site) ist abgeschlossen. Der Kern
    der Phase-1.2-Pipeline ist weiter als ein Blick auf die Roadmap
    vermuten lässt: `scan-bridge` liefert heute `/health`, `/version`,
    `/ready`, `/profiles` und `/profiles/{name}`, und `POST /scan` ist
    ein echter, Bearer-geschützter Handler, der über `sane-runtime` und
    `scan-processor` bis zur Zustellung durchreicht — nur die
    `/jobs*`-Endpunkte antworten noch mit `501 Not Implemented`.
    `sane-runtime` und `scan-processor` existieren als echte
    Go-Implementierungen samt Dockerfiles, und die `compose.yaml` im
    Repository-Root verdrahtet alle drei Dienste.

    Am 26.08.2026 ist der erste vollständige Durchlauf gegen die
    Referenz-Hardware gelungen: `POST /scan` hat einen Duplex-Scan
    ausgelöst, das Ergebnis als zweiseitiges PDF montiert und in
    Paperless-ngx hochgeladen. Was fehlt, ist das Drumherum —
    Bootstrap-Skript, veröffentlichter Compose-Stack, scanbd, der
    Home-Assistant-Blueprint, der asynchrone Job-Store, Monitoring und
    Backup.

<div class="mdx-status" markdown="1">
| Phase | Umfang | Stand |
| ----- | ------ | ----- |
| **0** | Repository, MIT-Lizenz, Doku-Site, Hardware-Tabelle | abgeschlossen |
| **1** | Kern-Pipeline verdrahtet, gegen echte Hardware belegt | läuft |
| **2** | Hardware-Tasten, Zigbee-Blueprints, n8n-Exporte | nicht begonnen |
| **3** | restic-Backup, Prometheus/Grafana, Härtung | nicht begonnen |
| **4** | Reife des Ökosystems — community-getrieben | nicht begonnen |
</div>

Direkt gegen das Repository geprüft — die
[Roadmap](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md)
ist der Plan, nicht immer der Stand auf den Commit genau. Wo beide
auseinandergehen, gilt hier, was tatsächlich im Repository steht.

!!! info "Deutsche Übersetzung: der Einstieg und die stabile Referenz"

    Übersetzt sind diese Startseite,
    [Erste Schritte](getting-started/index.md), der
    [Schnellstart](getting-started/quickstart.md),
    [Panel installieren](install/index.md),
    [Panel verwalten](manage/index.md), die
    [Architektur](architecture/index.md) samt
    [Speichertopologien](architecture/storage-topologies.md) und
    [Keine Drittanbieter-Anfragen](architecture/no-third-party-requests.md)
    sowie die [Hardware](hardware/index.md) mit
    [Kodak ScanMate i1120](hardware/kodak-scanmate-i1120.md) und
    [CYD-Scan-Panel](hardware/cyd-scan-panel.md).

    **Englisch bleiben vorerst die Seiten, die sich am häufigsten
    ändern** — Scan-Profile, Profil-Schema, Troubleshooting, API-Referenz
    und der Blog. Das ist Absicht: Eine Übersetzung, die hinterherhinkt,
    ist schlechter als gar keine. Jede deutsche Seite verlinkt an den
    passenden Stellen ins Englische.

    Grund für den zweigeteilten Aufbau: Zensical hat noch keine native
    Mehrsprachigkeit. Englische und deutsche Site sind zwei getrennte
    Builds, die im CI zusammengefügt werden. Upstream sind das
    [zensical/backlog#2](https://github.com/zensical/backlog/issues/2)
    (native i18n) und
    [zensical/backlog#1](https://github.com/zensical/backlog/issues/1);
    auf unserer Seite hält
    [Issue #13](https://github.com/strausmann/paperless-scan-bridge/issues/13)
    fest, wann der Workaround verschwindet.

## Weiterführend

- [Erste Schritte](getting-started/index.md) — Voraussetzungen und der
  erste Scan
- [Architektur](architecture/index.md) — Komponenten, Datenfluss und die
  drei Speichertopologien
- [Hardware](hardware/index.md) — was funktioniert, was nicht, und wie
  Sie Ihr eigenes Gerät melden
- [Panel installieren](install/index.md) — Firmware direkt aus dem
  Browser auf das ESP32-Panel flashen
- [Englische Dokumentation](/en/) — vollständig, inklusive Referenz
- [Repository auf GitHub](https://github.com/strausmann/paperless-scan-bridge)

## Lizenz und Marken

MIT. Kodak® und ScanMate® sind Marken von Kodak Alaris Inc. Synology®
ist eine Marke von Synology Inc. IKEA®, TRÅDFRI®, STYRBAR® und
SYMFONISK® sind Marken von Inter IKEA Systems B.V. Raspberry Pi® ist
eine Marke der Raspberry Pi Ltd. Paperless-ngx ist ein
community-gepflegter Fork von Paperless-ng. Dieses Projekt steht in
keiner Verbindung zu diesen Unternehmen und wird von ihnen weder
unterstützt noch gesponsert. Produktnamen dienen ausschließlich der
Identifikation.
