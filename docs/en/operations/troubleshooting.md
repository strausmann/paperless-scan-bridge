# Troubleshooting

Symptom-driven. Start at the symptom that matches, work down the list.

!!! note "Growing document"

    Most of the pipeline is not implemented yet, so this page covers the
    parts that exist today plus the failure modes already understood from
    hardware testing. It grows as the stack does.

## The scanner is not detected

**Check the USB bus first, on the host:**

```bash
lsusb
```

If the device does not appear here, it is a cable, power, or hardware
problem — no amount of container configuration will fix it.

**Then check that the container can see it.** The `sane-runtime`
container needs the device passed through explicitly:

```text
--device=/dev/bus/usb
```

plus a udev rule granting access. The container must never need
`--privileged`; if it seems to, the udev rule is wrong.

**Then check SANE inside the container:**

```bash
scanimage -L
```

An empty list with a visible `lsusb` entry usually means the backend is
not enabled, or the device needs an entry in the backend's config file.

## The scanner is detected but scanning fails

```bash
scanimage -A          # list all options the backend exposes
```

If `-A` works but a scan errors out, check the source setting. On the
reference i1120 the duplex source is spelled exactly `ADF Duplex`.

`SANE_STATUS_NO_DOCS` means the ADF is empty. On the i1120 this is the
*only* reliable paper-presence signal — the paper sensor is not
otherwise visible to SANE.

## Pressing the Start button does nothing

Expected on the Kodak ScanMate i1120. The Start button generates no
SANE-visible event on this device. See
[the i1120 page](../hardware/kodak-scanmate-i1120.md) for the evidence
and [issue #7](https://github.com/strausmann/paperless-scan-bridge/issues/7)
for the USB-level investigation.

The indicator wheel (positions 1–9) *does* generate events and can be
used as a secondary trigger.

## `POST /scan` returns 501

Expected today. The scan dispatch path, job store, and SANE-net client
are Phase 1.2 work. `GET /health`, `GET /version`, `GET /profiles` and
`GET /profiles/{name}` are implemented; `GET /ready`, `POST /scan` and
the `/jobs` endpoints are not.

## Documents scan but never appear in Paperless

Check which [storage topology](../architecture/storage-topologies.md)
you are running.

On **Topology B (NFS direct)**, `inotify` does not work over NFS.
Paperless must be configured to poll:

```text
PAPERLESS_CONSUMER_POLLING=10
```

Without it, files land in the consume directory and sit there forever.
This is the single most common misconfiguration in this topology.

On **Topologies A and C**, inotify does work; if pickup stalls there,
check filesystem permissions on the consume directory and whether the
write was atomic — a partially written PDF picked up mid-write fails
OCR.

## Collecting information for a bug report

```bash
curl -s http://your-pi-host:8080/version
docker compose logs --no-color --tail=200 scan-bridge
lsusb
```

Include all three, plus the scanner model and the SANE backend, when
opening an issue.
