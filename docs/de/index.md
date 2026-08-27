# paperless-scan-bridge

Dokument einlegen. Knopf drücken. Dreißig Sekunden später ist es in
Paperless-ngx durchsuchbar.

`paperless-scan-bridge` ist ein Container-First-Stack, der einen per USB
an einen Raspberry Pi angeschlossenen Dokumentenscanner mit einer
Paperless-ngx-Instanz irgendwo im Netz verbindet. Die Dokumente landen
auf einer Synology-NAS — die vorhandene Backup-, Snapshot- und
Off-Site-Strategie gilt damit für alles, was das System produziert.

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
    Bootstrap-Skript, veröffentlichter Compose-Stack, Monitoring,
    Backup.

!!! info "Deutsche Übersetzung: der Einstieg, nicht die Referenz"

    Übersetzt sind die Seiten, die man zum Anfangen braucht: diese
    Startseite, [Erste Schritte](getting-started/index.md), der
    [Schnellstart](getting-started/quickstart.md), die
    [Hardware-Übersicht](hardware/index.md) sowie
    [Panel installieren](install/index.md) und
    [Panel verwalten](manage/index.md).

    **Die Referenzdokumentation bleibt vorerst englisch** — Profil-Schema,
    Architektur, Troubleshooting und API-Referenz. Das ist Absicht: Diese
    Seiten ändern sich am häufigsten, und eine Übersetzung, die
    hinterherhinkt, ist schlechter als gar keine. Jede deutsche Seite
    verlinkt an den passenden Stellen ins Englische.

    Grund für den zweigeteilten Aufbau: Zensical hat noch keine native
    Mehrsprachigkeit. Englische und deutsche Site sind zwei getrennte
    Builds, die im CI zusammengefügt werden. Upstream sind das
    [zensical/backlog#2](https://github.com/zensical/backlog/issues/2)
    (native i18n) und
    [zensical/backlog#1](https://github.com/zensical/backlog/issues/1);
    auf unserer Seite hält
    [Issue #13](https://github.com/strausmann/paperless-scan-bridge/issues/13)
    fest, wann der Workaround verschwindet.

## Worum es geht

Für Teile dieses Stacks gibt es Dutzende bruchstückhafte Anleitungen —
SANE auf dem Pi, Paperless-ngx mit NFS, Scanner-Tasten über scanbd,
Zigbee-Automatisierung in Home Assistant. Was fehlt, ist ein einzelnes
Repository, das vom frischen Pi-Image bis zur produktionsreifen
Scan-Pipeline mit Backup, Monitoring und Härtung durchführt.

Diese Lücke füllt dieses Projekt. Nebenbei ist es die Chronik davon, wie
ein Kodak ScanMate i1120 — ein sechzehn Jahre alter Tischscanner ohne
moderne Linux-Treiber — zum freihändigen Teil eines Homelabs wird.

## Die eine nicht verhandelbare Regel

**Container-First, dünner Host.** Am Pi sind ausschließlich drei
Eingriffe erlaubt: Docker samt Compose-Plugin installieren, die
Synology-NFS-Freigabe über `/etc/fstab` einhängen und udev-Regeln unter
`/etc/udev/rules.d/` ablegen. Kein SANE auf dem Host. Kein scanbd auf
dem Host. Keine Sprach-Runtimes auf dem Host.

## Weiterführend

- [Panel installieren](install/index.md) — Firmware direkt aus dem
  Browser auf das ESP32-Panel flashen
- [Erste Schritte](getting-started/index.md) — was der Stack tut und was
  man dafür braucht
- [Englische Dokumentation](/en/) — vollständig, inklusive Referenz
- [Repository auf GitHub](https://github.com/strausmann/paperless-scan-bridge)
- [Roadmap](https://github.com/strausmann/paperless-scan-bridge/blob/main/ROADMAP.md)

## Lizenz und Marken

MIT. Kodak® und ScanMate® sind Marken von Kodak Alaris Inc. Synology®
ist eine Marke von Synology Inc. IKEA®, TRÅDFRI®, STYRBAR® und
SYMFONISK® sind Marken von Inter IKEA Systems B.V. Raspberry Pi® ist
eine Marke der Raspberry Pi Ltd. Paperless-ngx ist ein
community-gepflegter Fork von Paperless-ng. Dieses Projekt steht in
keiner Verbindung zu diesen Unternehmen und wird von ihnen weder
unterstützt noch gesponsert. Produktnamen dienen ausschließlich der
Identifikation.
