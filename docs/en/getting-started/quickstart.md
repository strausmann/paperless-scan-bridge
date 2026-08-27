# Quickstart

!!! info "The tooling on this page exists now"

    `deploy/bootstrap/install.sh` and `deploy/compose/scan-bridge.yml`
    are in the repository. What has **not** been done is a run of this
    page end to end on a fresh Pi: the pipeline itself is proven against
    the reference hardware, and the bootstrap script is proven by its own
    `--dry-run` and by `docker compose config`, but nobody has yet taken
    an unprepared machine from nothing to a scan by following these six
    steps. Expect to hit something. Please report it.

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

```bash
ssh pi@your-pi-host
curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
less install.sh          # read what it is about to do
sudo bash install.sh
```

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

# the two secrets, as files -- an env var shows up in `docker inspect`
printf '%s' 'YOUR_PAPERLESS_TOKEN' > deploy/secrets/paperless_api_token
openssl rand -hex 32               > deploy/secrets/bridge_token
chmod 0640 deploy/secrets/*
```

At minimum you set the Paperless-ngx URL, the API token, and the NFS
mount point.

!!! danger "Do not commit secrets"

    The Paperless API token and the bridge's own tokens belong in Docker
    secrets, environment variables, or a SOPS-encrypted file — never in a
    profile YAML and never in git. Profile files reference secrets by name
    only; the daemon rejects cleartext tokens at parse time.

## 4. Bring up the bridge

```bash
docker compose -f deploy/compose/scan-bridge.yml up -d
```

`PSB_VERSION` in `.env` has no default: compose refuses to start
without it rather than reaching for `latest` (ADR 0011), so an unpinned
deployment cannot happen by forgetting.

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
