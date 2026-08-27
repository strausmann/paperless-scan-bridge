# Schnellstart

!!! warning "Noch nicht schlüsselfertig"

    Das Bootstrap-Skript (`deploy/bootstrap/install.sh`) und die
    Compose-Stacks (`deploy/compose/`), auf die diese Seite verweist,
    sind Phase-1-Lieferungen und liegen noch nicht im Repository. Die
    Seite dokumentiert den vorgesehenen Ablauf, damit die Form des
    Setups überprüfbar ist, bevor der Code da ist.

## Voraussetzungen

- Raspberry Pi 4 oder 5 mit Ubuntu Server 24.04 LTS (arm64)
- Ein SANE-kompatibler USB-Scanner — siehe
  [Hardware-Übersicht](../hardware/index.md)
- Eine Synology-NAS mit aktiviertem NFS
- Ein Docker-Host für Paperless-ngx (darf die NAS selbst sein)

Der Pi braucht nur Docker, einen NFS-Mount und USB-Berechtigungen. Alles
andere läuft in Containern.

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

```bash title="Noch nicht — deploy/bootstrap/install.sh existiert nicht"
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

```bash title="Noch nicht — deploy/compose/ existiert nicht"
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
