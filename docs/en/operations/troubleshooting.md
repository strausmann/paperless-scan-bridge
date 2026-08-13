# Troubleshooting

Symptom-driven. Start at the symptom that matches, work down the list.

!!! note "Growing document"

    This page covers the failure modes understood so far from hardware
    testing and from running the built pipeline (scan → OCR/assembly →
    Paperless delivery). Job-store/status-polling endpoints (`/jobs*`)
    are still not implemented — see the section below. It grows as the
    stack does.

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

## `/jobs*` returns 501

Expected today. `POST /scan` itself is fully implemented and
synchronous — it blocks through scan → `scan-processor` → every
configured destination's upload submission and returns the finished
result inline as `200 OK`. There is no job store to poll: `GET /jobs`,
`GET /jobs/{id}`, and `POST /jobs/{id}/cancel` all return `501` because
that store was never built (see [Profile schema
reference](../getting-started/profile-schema.md#response-shape) for
what the `200` response itself carries instead).

## Documents scan but never appear in Paperless

**First, check `scan-bridge`'s own `POST /scan` response** — the
scan-side pipeline (scanning, OCR, assembly) can succeed while the
Paperless *destination* still fails. Each document's `destinations[]`
entry reports either `{"status": "submitted", "task_id": "..."}` (the
normal case — see below) or `{"status": "failed", "error": "..."}`. A
`failed` entry's `error` string is the actual cause — usually one of:

- `base_url` unreachable from the container (DNS, firewall, wrong
  port)
- an invalid/expired/missing `paperless_api_token` Docker secret or
  `PAPERLESS_API_TOKEN` environment variable (see [Profile schema
  reference](../getting-started/profile-schema.md#the-paperless-targets-config-block)
  for the secret name and resolution order)
- Paperless itself rejecting the upload (4xx) — check Paperless's own
  logs for the specific validation error

**A `"submitted"` result is not proof the document exists in
Paperless yet.** `post_document/` is asynchronous on Paperless's own
side — `submitted` means "Paperless accepted the upload and queued its
own Celery consumption task", not "the document is indexed and
searchable". Poll `GET /api/tasks/?task_id=<the reported task_id>`
against Paperless itself (not `scan-bridge`) to see whether that task
reached `SUCCESS` or `FAILURE`; `scan-bridge` does not do this polling
on your behalf ([Profile schema
reference](../getting-started/profile-schema.md#not-in-the-schema-yet)).

The `paperless` destination calls Paperless's REST API directly
(`POST /api/documents/post_document/`) — it does not write into an
NFS/inotify consume directory. The
[storage topologies](../architecture/storage-topologies.md) page and
the `PAPERLESS_CONSUMER_POLLING` advice below only apply if you are
feeding Paperless through its own consume-directory mechanism
separately from this project (e.g. for documents scanned another way)
— they are unrelated to `scan-bridge`'s own delivery path, and no
destination that writes into a consume directory is built yet.

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

## A scan with several destinations or `page_grouping: per_page` times out partway through delivery

`timeout_seconds` bounds the **whole** `POST /scan` call, not just the
scan itself: scan, `scan-processor`, and every configured
destination's upload *submission* all share the same context deadline,
and destinations are delivered **serially**, one document at a time,
one destination at a time within each document (design doc §7, Option
A — deliberately, to avoid building a job queue for v1).

This means a profile with `assembly.page_grouping: per_page` (many
small documents instead of one combined one) and/or several
`destinations` entries needs a larger `timeout_seconds` than a
single-document, single-destination profile — the deadline has to
cover every one of those sequential uploads, not just the first. A
scan that produces 10 per-page documents fanned out to 2 destinations
each makes 20 sequential delivery calls before the response returns;
size `timeout_seconds` with that multiplication in mind, not just the
expected scan+OCR duration. If a scan is failing with a `504 timeout`
only on profiles with many pages/destinations, this is very likely why
— see
[issue #49](https://github.com/strausmann/paperless-scan-bridge/issues/49)
for the tracked follow-up (a per-destination or per-document timeout
budget instead of one shared one).

## Disk usage under `paths.output_dir` keeps growing

`scan-bridge` writes both a job's raw TIFF pages *and* the assembled
document(s) `scan-processor` returns under `output_dir`
(`/var/lib/scan-bridge/scans` by default), one subdirectory per
`scan_id` — this is deliberate (it's how `scan-bridge` re-reads pages
to hand them to `scan-processor`, and how it re-reads assembled
documents to hand them to each destination). **Nothing deletes these
subdirectories today** — there is no TTL, no cleanup-after-successful-
delivery, and no cleanup-after-failed-scan. This is a known,
[tracked](https://github.com/strausmann/paperless-scan-bridge/issues/49)
gap, not a bug you're hitting alone.

Because this pipeline processes receipts, invoices, and other personal
documents, unbounded local accumulation is a real operational and
privacy concern, not just a disk-space one — plan for it explicitly
rather than assuming the container cleans up after itself:

- Mount `output_dir` on storage you monitor for free space, same as
  any other unbounded-growth path.
- Until an in-daemon cleanup mechanism lands, prune old subdirectories
  yourself (e.g. a cron job removing `scan_id` directories older than
  a few days) rather than letting the volume fill silently.

## Collecting information for a bug report

```bash
curl -s http://your-pi-host:8080/version
docker compose logs --no-color --tail=200 scan-bridge
lsusb
```

Include all three, plus the scanner model and the SANE backend, when
opening an issue.
