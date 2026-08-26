# Erste Schritte

Dieser Abschnitt führt vom frischen Raspberry Pi bis zu einem Dokument,
das in Paperless-ngx landet.

!!! warning "Noch nicht schlüsselfertig"

    Phase 1 läuft noch. Der Kern der Pipeline funktioniert — am
    26.08.2026 hat `POST /scan` erstmals einen Duplex-Scan gegen die
    Referenz-Hardware ausgelöst, als PDF montiert und nach Paperless-ngx
    hochgeladen. Was fehlt, ist das Drumherum: Das Bootstrap-Skript und
    die veröffentlichten Compose-Stacks, auf die unten verwiesen wird,
    liegen noch nicht im Repository. Die Seiten hier beschreiben den
    vorgesehenen Ablauf und wachsen mit, sobald ein Teil fertig ist.

## Seiten

- [Schnellstart](quickstart.md) — Voraussetzungen, Bootstrap, erster
  Scan
- [Panel installieren](../install/index.md) — Firmware für das
  ESP32-Touch-Panel aus dem Browser flashen
- [Scan profiles](/getting-started/scan-profiles/) *(englisch)* — wie
  Profile definiert und ausgewählt werden
- [Profile schema reference](/getting-started/profile-schema/)
  *(englisch)* — vollständige Feldreferenz

## Was Sie vorher brauchen

Drei Dinge:

1. **Einen Raspberry Pi 4 oder 5** mit Ubuntu Server 24.04 LTS (arm64)
   und einem SANE-kompatiblen USB-Scanner. Referenz-Hardware ist ein
   Pi 5 mit 8 GB RAM, SSD über USB 3.0 und ein Kodak ScanMate i1120.
2. **Eine Synology-NAS** mit aktiviertem NFS. Die NAS ist die einzige
   Quelle der Wahrheit für Dokumente; der Pi ist Ingestion-Knoten, kein
   Speicherknoten.
3. **Einen Docker-Host für Paperless-ngx.** Das kann die NAS selbst
   sein, ein Mini-PC oder irgendeine dauerhaft laufende Linux-Maschine.
   Es muss nicht der Pi sein.

Bevor Sie etwas kaufen: ein Blick in die
[Hardware-Übersicht](../hardware/index.md).
