# paperless-scan-bridge

Dokument einlegen. Knopf drücken. Dreißig Sekunden später ist es in
Paperless-ngx durchsuchbar.

`paperless-scan-bridge` ist ein Container-First-Stack, der einen per USB
an einen Raspberry Pi angeschlossenen Dokumentenscanner mit einer
Paperless-ngx-Instanz irgendwo im Netz verbindet. Die Dokumente landen
auf einer Synology-NAS — die vorhandene Backup-, Snapshot- und
Off-Site-Strategie gilt damit für alles, was das System produziert.

!!! warning "Projektstand: frühe Phase 1 — es wird noch nichts gescannt"

    Dies ist ein Home-Lab-Projekt in aktiver Entwicklung. Phase 0
    (Repository, Dokumentation, diese Site) ist abgeschlossen. Phase 1 hat
    begonnen: Der `scan-bridge`-Daemon liefert heute `/health`,
    `/version`, `/profiles` und `/profiles/{name}`; `/ready`, `/scan` und
    die `/jobs`-Endpunkte antworten mit `501 Not Implemented`. Die
    Container `sane-runtime` und `scan-processor`, die Compose-Stacks und
    das Bootstrap-Skript sind noch nicht geschrieben — einen
    funktionierenden Scan-Pfad gibt es also noch nicht.

!!! info "Deutsche Übersetzung im Aufbau"

    Diese Seite ist derzeit die einzige deutsche Seite. Die vollständige
    Dokumentation liegt auf Englisch vor und wird schrittweise übersetzt.
    Bis dahin: [zur englischen Dokumentation](/).

    Grund für den Zuschnitt: Zensical hat noch keine native
    Mehrsprachigkeit. Englische und deutsche Site sind zwei getrennte
    Builds, die im CI zusammengefügt werden.

    Upstream ist das
    [zensical/backlog#2](https://github.com/zensical/backlog/issues/2)
    (native i18n) beziehungsweise
    [zensical/backlog#1](https://github.com/zensical/backlog/issues/1)
    (Kompatibilität zu `mkdocs-static-i18n`); auf unserer Seite hält
    [Issue #13](https://github.com/strausmann/paperless-scan-bridge/issues/13)
    fest, wann der Workaround wieder verschwindet.

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

- [Englische Dokumentation](/) — vollständig
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
