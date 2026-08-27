# Schnellstart

!!! info "Das Werkzeug auf dieser Seite existiert jetzt"

    `deploy/bootstrap/install.sh` und `deploy/compose/scan-bridge.yml`
    liegen im Repository. Was **noch nicht** passiert ist: ein
    Durchlauf dieser Seite von vorn bis hinten auf einem frischen Pi.
    Die Pipeline selbst ist gegen die Referenz-Hardware belegt, und das
    Bootstrap-Skript durch seinen eigenen `--dry-run` und durch
    `docker compose config` — aber niemand hat bislang eine
    unvorbereitete Maschine anhand dieser sechs Schritte bis zum Scan
    gebracht. Rechnen Sie damit, irgendwo anzuecken. Bitte melden.

## Voraussetzungen

- **Ein Linux-Host mit Docker, in USB-Reichweite des Scanners.** Jede
  amd64- oder arm64-Maschine. Ein Raspberry Pi 4 oder 5 mit Ubuntu
  Server 24.04 ist die Referenz und der günstige Weg, einen Host genau
  dorthin zu stellen, wo der Scanner stehen soll — aber er ist **eine
  Möglichkeit, keine Voraussetzung**. Wer ohnehin einen Docker-Host in
  Kabelreichweite betreibt, nimmt den und lässt den Pi weg.
- Ein SANE-kompatibler USB-Scanner — siehe
  [Hardware-Übersicht](../hardware/index.md)
- Eine Synology-NAS mit aktiviertem NFS
- Ein Docker-Host für Paperless-ngx (darf die NAS selbst sein)

Der Host braucht Docker, einen NFS-Mount und USB-Berechtigungen. Alles
andere läuft in Containern.

!!! info "Gescannte Seiten landen nie auf der Platte des Hosts"

    Jeder Scan schreibt rohe TIFF-Seiten, lässt sie von
    `scan-processor` zurücklesen und löscht sie wieder, bevor die
    HTTP-Antwort rausgeht — sie existieren für die Dauer einer Anfrage
    und keine Sekunde länger. Der Referenz-Stack legt diesen
    Zwischenspeicher auf **tmpfs**, er liegt also im RAM und erreicht
    dauerhaften Speicher nie.

    Am wichtigsten ist das auf einem Pi, der von SD-Karte bootet: ein
    Schreib-Lösch-Zyklus pro Scan ist genau das Zugriffsmuster, das
    diese Karten am schlechtesten vertragen. Es ist aber überall die
    richtige Vorgabe, denn die Daten haben keinen Grund, auf eine Platte
    geschrieben zu werden, von der sie gleich wieder verschwinden.
    Größen: `SCAN_BRIDGE_SCRATCH_SIZE` und `SCAN_PROCESSOR_TMPFS_SIZE`
    in der `.env`; beide fassen je einen Auftrag, und ein Überlauf
    scheitert laut, statt ein zu kurzes Dokument zu erzeugen.

    Fertige Dokumente gehen auf die NFS-Freigabe, nicht auf den Host.

## 1. Synology-Freigabe vorbereiten

Legen Sie einen freigegebenen Ordner für die Scan-Pipeline an und
aktivieren Sie NFS-Zugriff für die IP des Pi. Welche Export-Optionen
nötig sind, hängt von der gewählten
[Speichertopologie](../architecture/storage-topologies.md) ab;
Topologie B (NFS direkt) ist der einfachste Einstieg.

## 2. Pi bootstrappen

Skript herunterladen, lesen, dann ausführen. Es verändert `/etc/fstab`
und `/etc/udev/rules.d/` als root — es direkt in eine Shell zu pipen ist
die Bequemlichkeit nicht wert: Ein abgebrochener Download würde als
halbes Skript ausgeführt.

```bash
ssh pi@ihr-pi-host
curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
less install.sh          # lesen, was gleich passiert
sudo bash install.sh
```

    Die URL liefert heute 404. Sie steht hier, damit die Form des
    Schritts überprüfbar ist — nicht zum Ausführen.

Das Skript installiert Docker samt Compose-Plugin, trägt den NFS-Mount
in `/etc/fstab` ein, legt die udev-Regel an, die dem Container stabilen
Zugriff auf den Scanner gibt, und lädt die Container-Images. Sonst
fasst es nichts am Host an.

## 3. Konfigurieren

```bash
git clone https://github.com/strausmann/paperless-scan-bridge.git
cd paperless-scan-bridge

cp deploy/compose/.env.example deploy/compose/.env
$EDITOR deploy/compose/.env
```

Mindestens zu setzen: die Paperless-ngx-URL, das API-Token und den
NFS-Mountpunkt.

!!! danger "Keine Geheimnisse committen"

    Das Paperless-API-Token und die Tokens der Bridge gehören in
    Docker-Secrets, Umgebungsvariablen oder eine SOPS-verschlüsselte
    Datei — niemals in ein Profil-YAML und niemals nach git. Profildateien
    referenzieren Geheimnisse ausschließlich über ihren Namen; der Daemon
    lehnt Klartext-Tokens schon beim Parsen ab.

## 4. Bridge starten

```bash
docker compose -f deploy/compose/scan-bridge.yml up -d
```

Pinnen Sie eine konkrete Image-Version in Ihrer Compose-Datei. Dieses
Projekt veröffentlicht und verwendet keine `latest`-Tags.

## 5. Prüfen

```bash
curl -s http://ihr-pi-host:8080/health
curl -s http://ihr-pi-host:8080/ready
curl -s http://ihr-pi-host:8080/profiles
```

`/health` meldet, dass der Prozess lebt. `/ready` liefert `200`, sobald
Profile geladen sind und `sane-runtime` erreichbar ist. `/profiles`
listet die konfigurierten Scan-Profile.

## 6. Erster Scan

```bash
curl -X POST http://ihr-pi-host:8080/scan \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"profile": "default"}'
```

!!! note "Das funktioniert bereits — das Drumherum noch nicht"

    `POST /scan` ist ein echter, Bearer-geschützter Handler: Er reicht
    über `sane-runtime` an den Scanner durch, lässt `scan-processor` die
    Seiten montieren und stellt das Ergebnis den konfigurierten Zielen
    zu. Am 26.08.2026 lief das erstmals vollständig gegen die
    Referenz-Hardware. Die `/jobs*`-Endpunkte antworten weiterhin mit
    `501 Not Implemented` — asynchrone Job-Verfolgung ist eigene Arbeit.

    Was fehlt, ist alles *drumherum* auf dieser Seite: Bootstrap-Skript
    und veröffentlichte Compose-Stacks.

## Wenn etwas klemmt

Wird der Scanner nicht erkannt, beginnen Sie bei
[Troubleshooting](/en/operations/troubleshooting/) *(englisch)*.
