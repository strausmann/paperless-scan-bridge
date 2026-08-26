# Quickstart

!!! warning "Not yet runnable"

    The bootstrap script (`deploy/bootstrap/install.sh`) and the compose
    stacks (`deploy/compose/`) referenced on this page are Phase 1
    deliverables and are not in the repository yet. This page documents the
    intended flow so the shape of the setup is reviewable before the code
    lands.

## Prerequisites

- Raspberry Pi 4 or 5 running Ubuntu Server 24.04 LTS (arm64)
- A SANE-compatible USB scanner — see the
  [hardware list](../hardware/index.md)
- A Synology NAS with NFS enabled
- A Docker host for Paperless-ngx (can be the NAS)

The Pi only needs Docker, an NFS mount, and USB permissions. Everything
else runs in containers.

## 1. Prepare the Synology share

Create a shared folder for the scan pipeline and enable NFS access for
the Pi's IP address. The exact export options depend on which
[storage topology](../architecture/storage-topologies.md) you pick;
Topology B (NFS direct) is the simplest starting point.

## 2. Bootstrap the Pi

Download the script, read it, then run it. It modifies `/etc/fstab` and
`/etc/udev/rules.d/` as root, so piping it straight into a shell is not
worth the convenience — a truncated download would execute as a
half-script.

```bash title="Not yet — deploy/bootstrap/install.sh does not exist"
ssh pi@your-pi-host
curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
less install.sh          # read what it is about to do
sudo bash install.sh
```

    The URL above 404s today. It is shown so the shape of the step is
    reviewable, not so it can be run.

The script installs Docker and the compose plugin, adds the NFS mount to
`/etc/fstab`, installs the udev rule that gives the container stable
access to the scanner, and pulls the container images. It touches
nothing else on the host.

## 3. Configure

```bash
git clone https://github.com/strausmann/paperless-scan-bridge.git
cd paperless-scan-bridge

cp deploy/compose/.env.example deploy/compose/.env
$EDITOR deploy/compose/.env
```

At minimum you set the Paperless-ngx URL, the API token, and the NFS
mount point.

!!! danger "Do not commit secrets"

    The Paperless API token and the bridge's own tokens belong in Docker
    secrets, environment variables, or a SOPS-encrypted file — never in a
    profile YAML and never in git. Profile files reference secrets by name
    only; the daemon rejects cleartext tokens at parse time.

## 4. Bring up the bridge

```bash title="Not yet — deploy/compose/ does not exist"
docker compose -f deploy/compose/scan-bridge.yml up -d
```

Pin an explicit image version in your compose file. This project does
not publish or use `latest` tags.

## 5. Verify

```bash
curl -s http://your-pi-host:8080/health
curl -s http://your-pi-host:8080/profiles
```

`/health` reports process liveness. `/profiles` lists the configured
scan profiles. Both endpoints work today.

## 6. First scan

```bash
curl -X POST http://your-pi-host:8080/scan \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"profile": "default"}'
```

!!! note "This works today — the deployment tooling around it does not"

    `POST /scan` is a real, bearer-protected handler: it dispatches
    through `sane-runtime` to the scanner, has `scan-processor` assemble
    the pages, and delivers the result to the configured destinations.
    It was first driven end to end against the reference hardware on
    2026-08-26. The `/jobs*` endpoints still return
    `501 Not Implemented` — asynchronous job tracking is separate work.

    What is missing is everything *around* it on this page: the
    bootstrap script and the published compose stacks.

## Troubleshooting

If the scanner is not detected, start at
[Troubleshooting](../operations/troubleshooting.md).
