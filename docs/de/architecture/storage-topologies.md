# Speichertopologien

Das Verhältnis zwischen Pi, Docker-Host und Synology-NAS ist die
folgenreichste Architekturentscheidung dieses Projekts. Unterstützt
werden drei Topologien, und ihre Abwägungen stehen ausdrücklich hier
statt hinter einer stillen Vorgabe.

## Auf einen Blick

| | A — Lokales FS + restic | B — NFS direkt | C — iSCSI-LUN |
| --- | --- | --- | --- |
| Abholmechanismus | inotify | Polling | inotify |
| Latenz am Scanner | unter einer Sekunde | ca. 10 s | unter einer Sekunde |
| Sicherung | restic aufs NAS | Synology-Snapshots | LUN-Snapshots |
| Speicherebenen | zwei (live + Backup) | eine | eine |
| Mehrere Hosts möglich | ja | ja | **nein** |
| Einrichtungsaufwand | mittel | gering | hoch |

## Topologie A — Lokales Dateisystem auf dem Docker-Host mit restic-Sicherung

```text
Pi → NFS-Schreibzugriff → /mnt/synology/staging/   (Synology)
                          |
                       Abgleich über einen inotify-Watch-Container
                          v
                Docker-Host: /var/lib/paperless/consume   (lokales FS)
                          |
                       Abholung per inotify
                          v
                Paperless-ngx
                          |
                          +-- nächtliches restic auf Synology /backup/restic/
                          +-- wöchentliches restic check --read-data-subset=10%
```

**Vorteile.** inotify funktioniert, die Abholung liegt also unter einer
Sekunde, und Paperless liest so schnell wie die Platte des Hosts. Die
Sicherung ist ein eigener, klar umrissener Vorgang mit einem dafür
gebauten Werkzeug.

**Nachteile.** Zwei Speicherebenen, über die man nachdenken muss. Ein
Restore braucht restic, nicht bloß eine Dateikopie.

**Wählen Sie das,** wenn Sie die beste Balance aus Geschwindigkeit,
Sicherungsintegrität und betrieblicher Klarheit wollen. Das ist die
empfohlene Voreinstellung.

## Topologie B — NFS direkt vom Synology

```text
Pi → NFS-Schreibzugriff → /mnt/synology/consume/   (Synology)
                          |
                          v
                Docker-Host: NFS-Mount → Paperless-Container
                          |
                       Polling (inotify funktioniert über NFS nicht)
                          v
                Paperless-ngx
                          |
                          +-- verlässt sich auf Btrfs-Snapshots des Synology
                          +-- Hyper Backup an einen anderen Standort
```

**Vorteile.** Eine einzige Speicherebene. Die Sicherung ergibt sich
implizit aus Snapshots und der Synology-Infrastruktur, die Sie ohnehin
betreiben. Das einfachste Denkmodell.

**Nachteile.** `inotify` funktioniert über NFS nicht, Paperless muss also
pollen (`PAPERLESS_CONSUMER_POLLING=10`) — das sind grob zehn Sekunden
Latenz am Scanner. Unter Last sind NFS-Sperrkonflikte möglich. Und
Btrfs-Snapshots, die gegen ein laufendes Consume-Verzeichnis gezogen
werden, können mit Schreibzugriffen kollidieren und sind nicht immer
absturzkonsistent.

**Wählen Sie das,** wenn Einfachheit vor Latenz geht: Einzelperson im
Haushalt, wenig Scanvolumen, und eine erste Einrichtung, die heute laufen
soll.

## Topologie C — iSCSI-LUN vom Synology

```text
Pi → NFS-Schreibzugriff → /mnt/synology/staging/   (Synology)
                          |
                          v
                Docker-Host: iSCSI-LUN als ext4 eingehängt → Paperless
                          |
                       Abholung per inotify
                          v
                Paperless-ngx
                          |
                          +-- LUN-Snapshots auf dem Synology
                          +-- Hyper Backup der LUN-Snapshots
```

**Vorteile.** inotify funktioniert, weil das aus Sicht des Hosts ein
lokales Blockgerät ist. LUN-Snapshots sind auf Blockebene
absturzkonsistent. Für die Sicherung gibt es genau einen Ort.

**Nachteile.** Ein LUN gehört genau einem Host — kein Aktiv-Aktiv über
zwei Docker-Knoten. Bei 1 GbE wird iSCSI bei sehr großen Scans zum
Engpass. Mehr Einrichtungsaufwand als NFS.

**Wählen Sie das,** wenn Sie inotify **und** Synology-eigene Sicherung
wollen und mit der Beschränkung auf einen Host leben können.

## Eine davon auswählen

`deploy/compose/` wird je Topologie eine Compose-Datei enthalten; Sie
kopieren die gewünschte nach `docker-compose.yml`.

!!! note "Die Compose-Dateien gibt es noch nicht"

    `deploy/compose/` ist zum Zeitpunkt dieses Textes leer. Der
    Referenz-Stack für Topologie B ist als erster geplant, weil er der
    einfachste Einstieg ist.
