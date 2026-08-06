# Storage topologies

The relationship between Pi, Docker host, and Synology NAS is the most
consequential architectural decision in this project. Three topologies
are supported, and the trade-offs are documented explicitly rather than
hidden behind a default.

## At a glance

| | A — Local FS + restic | B — NFS direct | C — iSCSI LUN |
| --- | --- | --- | --- |
| Pickup mechanism | inotify | polling | inotify |
| Latency at the scanner | sub-second | ~10 s | sub-second |
| Backup | restic to NAS | Synology snapshots | LUN snapshots |
| Storage tiers | two (live + backup) | one | one |
| Multi-host capable | yes | yes | **no** |
| Setup effort | medium | low | high |

## Topology A — Local filesystem on the Docker host with restic backup

```text
Pi → NFS write → /mnt/synology/staging/   (Synology)
                          |
                       sync via inotify-watch container
                          v
                Docker host: /var/lib/paperless/consume   (local FS)
                          |
                       inotify pickup
                          v
                Paperless-ngx
                          |
                          +-- nightly restic to Synology /backup/restic/
                          +-- weekly restic check --read-data-subset=10%
```

**Pros.** Inotify works, so pickup is sub-second and Paperless reads are
as fast as the host's disk. Backup is a separate, well-defined process
with a dedicated tool.

**Cons.** Two storage tiers to reason about. Restore requires restic,
not a plain file copy.

**Choose this when** you want the best balance of performance, backup
integrity, and operational clarity. This is the recommended default.

## Topology B — NFS direct from Synology

```text
Pi → NFS write → /mnt/synology/consume/   (Synology)
                          |
                          v
                Docker host: NFS mount → Paperless container
                          |
                       polling (inotify does not work on NFS)
                          v
                Paperless-ngx
                          |
                          +-- relies on Synology Btrfs snapshots
                          +-- Hyper Backup to off-site
```

**Pros.** A single storage tier. Backup is implicit via snapshots and
the Synology infrastructure you already run. Simplest mental model.

**Cons.** `inotify` does not work over NFS, so Paperless has to poll
(`PAPERLESS_CONSUMER_POLLING=10`) — roughly ten seconds of latency at
the scanner. NFS lock contention is possible under load. Btrfs snapshots
taken against a live consume directory can race with writes and are not
always crash-consistent.

**Choose this when** simplicity beats latency: single-user homes, low
scan volume, and a first setup you want running today.

## Topology C — iSCSI LUN from Synology

```text
Pi → NFS write → /mnt/synology/staging/   (Synology)
                          |
                          v
                Docker host: iSCSI LUN mounted as ext4 → Paperless
                          |
                       inotify pickup
                          v
                Paperless-ngx
                          |
                          +-- LUN snapshots on Synology
                          +-- Hyper Backup of LUN snapshots
```

**Pros.** Inotify works, because from the host's perspective this is a
local block device. LUN snapshots are crash-consistent at the block
level. One storage location for backup purposes.

**Cons.** A LUN belongs to exactly one host — no active-active across
two Docker nodes. On 1 GbE, iSCSI becomes the bottleneck for very large
scans. More setup than NFS.

**Choose this when** you want inotify *and* Synology-native backup, and
can accept the single-host limitation.

## Picking one

`deploy/compose/` will contain a compose file per topology; you copy the
one you want to `docker-compose.yml`.

!!! note "Compose files not written yet"

    `deploy/compose/` is empty at the time of writing. The reference stack
    for Topology B is the first one planned, because it is the simplest
    starting point.
