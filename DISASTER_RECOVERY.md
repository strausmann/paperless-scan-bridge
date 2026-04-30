# Disaster Recovery

> **Status:** Draft v1.0
> **Last updated:** 2026-04-30
> **Audience:** Anyone operating a `paperless-scan-bridge` deployment
> who needs to recover from hardware failure, data loss, or
> configuration disasters.

## Purpose

This document is the operational runbook for backup and recovery. It
answers three questions:

1. **What is backed up, where, and how often?**
2. **What do I do when something fails?**
3. **How do I prove the backups work before I need them?**

A backup that has never been restored is a backup that might not
work. We treat untested backup procedures as the same risk as no
backup at all. Every recovery scenario in this document has been
walked through against real hardware at least once.

---

## Table of contents

1. [Recovery objectives](#1-recovery-objectives)
2. [What is backed up](#2-what-is-backed-up)
3. [Backup architecture](#3-backup-architecture)
4. [Backup schedule](#4-backup-schedule)
5. [Off-site replication](#5-off-site-replication)
6. [Restore procedures](#6-restore-procedures)
7. [Disaster scenarios and runbooks](#7-disaster-scenarios-and-runbooks)
8. [Periodic restore testing](#8-periodic-restore-testing)
9. [Backup integrity verification](#9-backup-integrity-verification)
10. [Key management](#10-key-management)
11. [Migration scenarios](#11-migration-scenarios)
12. [Lessons learned and post-incident review](#12-lessons-learned-and-post-incident-review)

---

## 1. Recovery objectives

Two metrics drive every decision in this document:

**RPO — Recovery Point Objective.** How much data are we willing to
lose? Our target: **24 hours** in the worst case, typically much
less. Hourly PostgreSQL dumps and nightly restic snapshots mean we
lose at most one day of new documents and at most one hour of new
metadata.

**RTO — Recovery Time Objective.** How long are we willing to be
down? Our targets:

- **Pi failure** with cold-standby Pi available: **15-30 minutes**
- **Docker host failure** with cold-standby host available:
  **60-90 minutes**
- **Synology NAS failure** with off-site backup: **4-8 hours**
- **Total disaster** (everything gone, rebuild from scratch with
  off-site backup): **1 working day**

These targets assume a calm, prepared operator following the
runbooks. First-time-ever execution under stress will be longer; we
recommend rehearsing at least once.

### 1.1 Why these targets

The system is a homelab document pipeline, not a hospital.
Sub-minute recovery is technically possible (active-active HA
cluster) but the operational cost is disproportionate. A scanner
that does not work for an hour is an inconvenience; a scanner that
loses a year of business records is a disaster. We optimize for
preventing the disaster, not for eliminating the inconvenience.

---

## 2. What is backed up

### 2.1 Mandatory backup contents

These items are backed up; loss is unacceptable:

- **Original documents** in Paperless-ngx `media/originals/`
- **OCR'd archive** in Paperless-ngx `media/archive/`
- **Document metadata** in PostgreSQL (tags, correspondents,
  document types, custom fields, workflow assignments)
- **Profile definitions** at `/etc/paperless-scan-bridge/bridge/profiles.yaml`
- **Compose configuration** at `/etc/paperless-scan-bridge/compose/`
- **Environment files** at `/etc/paperless-scan-bridge/compose/.env`
- **SOPS-encrypted secrets** at `/etc/paperless-scan-bridge/secrets/`
- **udev rules** at `/etc/udev/rules.d/99-paperless-scan-bridge.rules`
- **The age private key** for SOPS decryption (this is the most
  critical item — without it, the backup is useless)

### 2.2 Optional backup contents

These items are backed up where convenient but loss is recoverable:

- **scan-bridge job database** at `/var/lib/scan-bridge/jobs.db`
  (lost jobs older than the live state are not consequential)
- **Prometheus metrics history** (rebuildable; recent operational
  visibility is what matters)
- **Grafana dashboard customizations** (the shipped JSON dashboards
  are in the repository)

### 2.3 Explicitly NOT backed up

These items are intentionally excluded:

- **Container images** — pulled from GHCR on demand
- **Operating system on the Pi** — rebuilt with the bootstrap script
- **Redis state** — rebuilt on next start
- **Paperless search index** — rebuilt by Paperless from the archive
- **Container working directories** — ephemeral by design
- **Log files** — long-term retention is the user's responsibility,
  shipped to Loki or syslog rather than backed up

### 2.4 What is irreplaceable

The single most important question to answer before a disaster: if
everything else is lost, what *must* we have to recover?

**The age private key.** Without it, the restic repository is
unreadable. The restic password is encrypted with this key. All
other secrets are encrypted with this key.

**Treat the age private key with the seriousness it deserves.**
Store at least three offline copies in physically separate
locations. A printed copy in a safe deposit box is not paranoid;
it is appropriate.

---

## 3. Backup architecture

Three layers of defense, each independent of the others.

### 3.1 Layer 1: PostgreSQL hourly dumps

Every hour, on the Docker host running Paperless-ngx, a cron job
runs `pg_dump` against the Paperless database and writes the result
to a local file:

```
/var/backups/paperless/postgres/
├── paperless-2026-04-30T00.sql.gz
├── paperless-2026-04-30T01.sql.gz
├── paperless-2026-04-30T02.sql.gz
└── ...
```

Retention: last 24 hourly dumps locally. Older dumps are pruned by
the same cron job. Restic includes the directory in its nightly
snapshots, so older dumps are still available historically.

Why hourly: PostgreSQL dumps are cheap relative to the value of the
metadata. An hour of lost tag work is a minor annoyance; a day of
lost work is consequential.

### 3.2 Layer 2: Nightly restic snapshots

Every night at 02:30 local time, restic creates a snapshot
containing:

- The PostgreSQL dump directory from Layer 1
- The Paperless `media/originals/` and `media/archive/` directories
- The configuration tree under `/etc/paperless-scan-bridge/`
- The udev rules
- The scan-bridge job database

The repository lives on the Synology at
`/volume1/backup/restic-paperless-scan-bridge/`. Restic's content-
addressable, deduplicated storage means each nightly snapshot is
typically only 10-50 MB after the first full snapshot, even when the
underlying data has grown substantially.

Retention policy:

```bash
restic forget \
    --keep-daily 7 \
    --keep-weekly 4 \
    --keep-monthly 12 \
    --keep-yearly 5 \
    --prune
```

Storage growth: typical homelab usage produces a repository of
50-200 GB after a year, depending on document volume.

### 3.3 Layer 3: Off-site replication

Synology Hyper Backup copies the entire restic repository to a
remote location every Sunday night. Supported destinations:

- **Second Synology NAS** at a different physical location (friend's
  house, family home, office)
- **S3-compatible object storage** (Backblaze B2, Wasabi, Hetzner
  Object Storage, AWS S3)
- **Encrypted external USB disk** rotated manually (3-2-1 backup
  rule satisfaction without ongoing cost)

The repository is already encrypted by restic, so the off-site copy
does not add an encryption layer. It does add geographic redundancy.

### 3.4 The 3-2-1 rule

Together, the three layers satisfy the classic backup rule:

- **3 copies of data**: live, restic snapshot, off-site
- **2 different storage media**: NAS internal disks, remote
  destination (different vendor/medium)
- **1 off-site copy**: the Hyper Backup target

We exceed this with hourly PostgreSQL dumps, which add a fourth
copy for the most rapidly changing data.

---

## 4. Backup schedule

### 4.1 Standard schedule

| When | What | Tool | Duration |
| --- | --- | --- | --- |
| Every hour at :05 | PostgreSQL dump | `pg_dump` | <30 seconds |
| Daily at 02:30 | restic snapshot | `restic backup` | 2-15 minutes |
| Weekly Sunday 03:00 | restic forget + prune | `restic forget` | 5-20 minutes |
| Weekly Sunday 04:00 | Off-site replication | Hyper Backup | 30 minutes - 4 hours |
| Weekly Sunday 05:00 | restic check (10% subset) | `restic check` | 15-60 minutes |
| Monthly 1st at 02:00 | restic check (full) | `restic check --read-data` | 1-6 hours |
| Quarterly | Restore test | Manual or CI workflow | 60-90 minutes |

### 4.2 Configuration

The backup orchestration runs on the Docker host (where Paperless
lives) via systemd timers. Why systemd timers and not cron:

- Better observability via `systemctl status`
- Persistent timers run even after host reboots
- Logs go to journald, easily forwarded to Loki

`/etc/systemd/system/paperless-backup-postgres.timer`:

```ini
[Unit]
Description=Hourly PostgreSQL dump for Paperless

[Timer]
OnCalendar=*:05
Persistent=true
RandomizedDelaySec=60

[Install]
WantedBy=timers.target
```

`/etc/systemd/system/paperless-backup-postgres.service`:

```ini
[Unit]
Description=PostgreSQL dump for Paperless
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/paperless-backup-postgres.sh
StandardOutput=journal
StandardError=journal
```

Similar units exist for `paperless-backup-restic`, the prune jobs,
and the integrity checks. All shipped under `backup/systemd/` in the
repository.

### 4.3 Handling failures

Every backup job logs its result to journald. Failed jobs trigger
an alert via the standard alert chain (Apprise, ntfy, Home Assistant
notification). The alert includes:

- Which job failed
- Exit code and stderr tail
- Timestamp
- A link to the full log

Two consecutive failures escalate to a higher-priority alert.
Three consecutive failures page the operator if a paging channel is
configured.

---

## 5. Off-site replication

### 5.1 Hyper Backup configuration

Hyper Backup runs on the Synology, copying the restic repository
folder to the configured remote target. Configuration recommendations:

- **Schedule**: Sunday 04:00 (after restic prune completes)
- **Block-level incremental**: enabled (saves bandwidth)
- **Client-side encryption**: not needed since restic already
  encrypted the data; enabling it adds CPU cost without security
  benefit
- **Compression**: disabled (restic already compresses where useful)
- **Versioning**: 12 versions retained at the remote
- **Integrity check**: weekly check after each successful run

### 5.2 Off-site target choices

For a homelab, three choices balance cost, complexity, and trust:

**Choice 1: Second Synology at a friend's house.**

- Cost: hardware only (one-time)
- Trust: high (you know the friend)
- Bandwidth: depends on friend's connection
- Best for: established relationships, low total data volume

**Choice 2: Backblaze B2 or Hetzner Storage Box.**

- Cost: ~5-10 EUR/month for 100 GB
- Trust: contractual (SLA-backed)
- Bandwidth: limited by your upload speed only
- Best for: most homelab users; reliable, cheap, simple

**Choice 3: Encrypted USB disk in physical location.**

- Cost: hardware only
- Trust: full (you control everything)
- Bandwidth: USB 3.0 (very fast)
- Best for: paranoid setups; require manual rotation discipline

The repository documents all three with example configurations under
`backup/off-site/`.

### 5.3 Bandwidth considerations

The first off-site replication is a full copy of the restic
repository. For 100 GB of repository on a 50 Mbps upload, this takes
about 5 hours. Plan accordingly:

- Schedule the first run during a known low-bandwidth period (e.g.
  Saturday night)
- Subsequent incrementals are typically <1 GB and complete in
  minutes
- If your upload bandwidth is limited, consider the USB-disk option
  for the initial seed and switch to network sync afterwards

### 5.4 Verifying off-site backups

The off-site backup is only useful if it is restorable. Twice a
year, perform a partial restore from the off-site copy to a
dedicated test environment:

1. Pull a recent snapshot from the off-site target
2. Decrypt with the restic password
3. Mount as a virtual filesystem (`restic mount`)
4. Verify a sample of files are readable and intact
5. Document the test in `backup/restore-test-log.md`

---

## 6. Restore procedures

### 6.1 Restoring a single document

Scenario: A user accidentally deleted a document in Paperless and
empty trash bin.

```bash
# On the Docker host
mkdir -p /tmp/restore
restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
       --password-file /etc/paperless-scan-bridge/secrets/restic-password \
       restore latest \
       --target /tmp/restore \
       --include "/var/lib/paperless/media/originals/2026/04/15/*"

# Verify the file
ls -la /tmp/restore/var/lib/paperless/media/originals/2026/04/15/

# Re-import into Paperless via the consume directory
cp /tmp/restore/var/lib/paperless/media/originals/2026/04/15/<file> \
   /mnt/synology/consume/

# Cleanup
rm -rf /tmp/restore
```

Time required: 1-5 minutes.

### 6.2 Restoring the PostgreSQL database

Scenario: The Paperless database is corrupt or wiped, but documents
on disk are intact.

```bash
# On the Docker host
docker compose stop paperless-ngx paperless-webserver

# Get the most recent dump (from local Layer 1)
ls -lt /var/backups/paperless/postgres/ | head -5
LATEST=$(ls -t /var/backups/paperless/postgres/*.sql.gz | head -1)

# Restore
gunzip -c "$LATEST" | docker compose exec -T paperless-db \
    psql -U paperless paperless

# Restart
docker compose start paperless-ngx paperless-webserver

# Trigger search index rebuild (if needed)
docker compose exec paperless-ngx \
    /usr/src/paperless/src/manage.py document_index reindex
```

Time required: 10-20 minutes for the database; 30-60 minutes for the
index rebuild on a 10,000-document corpus.

### 6.3 Restoring a complete Paperless deployment

Scenario: The Docker host died, the database is gone, you have a
fresh host.

Prerequisites: A new host running Ubuntu Server 24.04 or compatible,
the SOPS age private key, network access to the Synology.

```bash
# Step 1: Mount the Synology share
sudo mkdir -p /mnt/synology
echo "synology.lan:/volume1/backup /mnt/synology nfs defaults,vers=4 0 0" \
    | sudo tee -a /etc/fstab
sudo mount -a

# Step 2: Decrypt the restic password
sudo mkdir -p /etc/paperless-scan-bridge/secrets
sops -d /mnt/synology/restic-password.age \
    > /etc/paperless-scan-bridge/secrets/restic-password
sudo chmod 600 /etc/paperless-scan-bridge/secrets/restic-password

# Step 3: Verify the restic repository is reachable
restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
       --password-file /etc/paperless-scan-bridge/secrets/restic-password \
       snapshots

# Step 4: Restore everything to a staging area
sudo mkdir -p /var/restore
sudo restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
       --password-file /etc/paperless-scan-bridge/secrets/restic-password \
       restore latest \
       --target /var/restore

# Step 5: Place files in their final locations
sudo cp -r /var/restore/etc/paperless-scan-bridge /etc/
sudo cp -r /var/restore/var/lib/paperless /var/lib/
sudo cp -r /var/restore/var/backups/paperless /var/backups/

# Step 6: Bring up the stack
cd /etc/paperless-scan-bridge/compose
docker compose pull
docker compose up -d paperless-db
sleep 30  # let postgres start

# Step 7: Restore the database from the most recent dump
LATEST=$(ls -t /var/backups/paperless/postgres/*.sql.gz | head -1)
gunzip -c "$LATEST" | docker compose exec -T paperless-db \
    psql -U paperless paperless

# Step 8: Bring up the rest of the stack
docker compose up -d

# Step 9: Verify
curl -sf http://localhost:8000/api/ui_settings/ | jq .
curl -sf http://localhost:8080/health
```

Time required: 60-90 minutes for a typical 50 GB Paperless dataset.
Most of the time is the restic restore; the actual configuration is
a few minutes.

### 6.4 Restoring just the bridge components

Scenario: The Docker host is fine, but the Pi failed and you have a
fresh Pi.

```bash
# Step 1: Flash a fresh Ubuntu Server 24.04 arm64 SD card
# (using rpi-imager or similar)

# Step 2: Boot the Pi, configure SSH access

# Step 3: Run the bootstrap script
ssh pi@new-pi-host
curl -sSL https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh \
    | sudo bash

# Step 4: Recover the bridge configuration from backup
sudo mkdir -p /etc/paperless-scan-bridge
sudo scp -r docker-host:/etc/paperless-scan-bridge/bridge \
    /etc/paperless-scan-bridge/
sudo scp -r docker-host:/etc/paperless-scan-bridge/runtime \
    /etc/paperless-scan-bridge/

# Step 5: Mount the Synology share
echo "synology.lan:/volume1/scans /mnt/synology nfs defaults,vers=4 0 0" \
    | sudo tee -a /etc/fstab
sudo mount -a

# Step 6: Bring up the bridge
cd /etc/paperless-scan-bridge/compose
docker compose up -d

# Step 7: Verify
curl -sf http://localhost:8080/health
docker exec sane-runtime scanimage -L
```

Time required: 15-30 minutes assuming fresh hardware ready.

---

## 7. Disaster scenarios and runbooks

### 7.1 Pi SD card failure

**Symptoms:**

- Pi unresponsive
- Cannot SSH
- LED indicators show no boot activity
- USB device list on the Docker host shows no Pi enumeration

**Recovery time:** 15-30 minutes if cold-standby Pi is available

**Procedure:** See section 6.4 above.

**Prevention:**

- Boot from SSD over USB 3.0 instead of SD card (10x more reliable)
- If using SD, prefer industrial-grade or high-endurance cards
- Configure log2ram to reduce SD writes
- Schedule the Pi for SD replacement every 18 months prophylactically

### 7.2 Docker host hard drive failure

**Symptoms:**

- Paperless not responding
- Docker reports volume errors
- `dmesg` shows I/O errors on the Paperless storage device
- `smartctl` shows reallocated sectors increasing

**Recovery time:** 60-90 minutes with cold-standby host

**Procedure:** See section 6.3 above.

**Prevention:**

- Use SSD with TBW headroom appropriate for write volume
- Run `smartctl` checks via Prometheus's smartctl_exporter
- Alert on reallocated sectors, not just outright failure
- Consider RAID-1 if downtime is unacceptable

### 7.3 Synology NAS failure

**Symptoms:**

- NFS mounts unresponsive
- Synology web UI inaccessible
- Network shows the NAS as unreachable
- LEDs indicate disk error or no boot

**Recovery time:** Variable (4-48 hours depending on cause)

**Procedure:**

The Synology is the storage hub. Its failure is significant. Steps:

1. **Diagnose**: power cycle, check disk LEDs, check the Synology
   web UI from another device on the same network
2. **If single disk failure in RAID**: replace the failed disk per
   Synology documentation, allow rebuild
3. **If multiple disk failure or controller failure**: this is a
   true disaster
4. **Procure replacement Synology** of the same DSM-supported series
5. **Migrate disks** if the controller failed and disks are intact
6. **Restore from off-site backup** if disks are lost

```bash
# On the new Synology, restore restic repository from off-site
# Then on the Docker host, follow section 6.3 to restore Paperless

# The bridge containers are unaffected by Synology failure if
# Topology A (local FS) is in use. They simply have nothing to write
# scans to during the outage.
```

**Prevention:**

- Use SHR-1 minimum (one disk redundancy)
- Use SHR-2 if data volume justifies (two disks redundancy)
- Replace disks at end-of-warranty preventatively
- Off-site backup as the ultimate insurance

### 7.4 Compromised secrets

**Symptoms:**

- Unauthorized scan jobs in the audit log
- Restic snapshots from unknown source IPs
- Login attempts on Synology with admin credentials
- Anomaly in metric exporters

**Recovery time:** 1-4 hours to rotate; assessment of what was
exposed takes longer.

**Procedure:**

1. **Stop the bleed**: change the Synology admin password, revoke
   the API token, rotate SSH keys
2. **Identify the scope**: review audit logs for unauthorized
   activity
3. **Rotate the API token**:
   ```bash
   # Generate a new token
   NEW_TOKEN=$(openssl rand -hex 32)

   # Update SOPS-encrypted secret
   echo "$NEW_TOKEN" > /tmp/api-token
   sops -e /tmp/api-token > /etc/paperless-scan-bridge/secrets/api-token.age
   shred -u /tmp/api-token

   # Trigger reload
   docker compose kill -s HUP scan-bridge

   # Update Home Assistant, n8n, scanbd hooks with new token
   ```
4. **Rotate the restic password** (heavier, requires re-encryption):
   ```bash
   # restic supports key rotation
   restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
          key add  # adds the new key
   restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
          key remove <OLD_KEY_ID>  # removes the old key
   ```
5. **Rotate the age key** (heaviest, requires re-encrypting all
   SOPS files):
   ```bash
   # Generate new age key
   age-keygen -o ~/.config/sops/age/keys-new.txt

   # Update .sops.yaml to include both old and new recipients
   # Re-encrypt all secrets
   for f in /etc/paperless-scan-bridge/secrets/*.age; do
       sops decrypt "$f" | sops encrypt --age <new-public-key> /dev/stdin > "${f}.new"
       mv "${f}.new" "$f"
   done

   # Remove old recipient from .sops.yaml
   # Securely destroy the old age key
   ```
6. **Investigate root cause**: how did the secret leak? Was a
   maintainer device compromised, was the .env file pushed by
   accident, was the container inspectable?
7. **Document the incident** in `backup/incident-log.md` with full
   timeline

**Prevention:**

- Pre-commit hooks scan for accidentally committed secrets
- gitleaks runs in CI on every push
- SOPS-encrypted secrets in repository (so accidental commit doesn't
  leak)
- Maintainer 2FA hardware keys
- Regular review of audit logs

### 7.5 Accidental deletion of restic repository

**Symptoms:**

- Restic snapshot list returns empty or errors
- Synology shows missing folder
- Backup jobs failing for multiple cycles

**Recovery time:** Depends on off-site backup freshness; typically
4-8 hours

**Procedure:**

1. **Stop all backup jobs immediately** to prevent overwriting
   off-site copy with empty state
2. **Verify deletion**: check Synology recycle bin (Btrfs snapshots
   may have a copy)
3. **If recoverable from Synology snapshot**: restore from snapshot,
   resume normal operations
4. **If not recoverable from Synology**: pull from off-site:
   ```bash
   # Restore the entire repository from Hyper Backup
   # (specifics depend on chosen off-site target)

   # Verify integrity
   restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
          --password-file /etc/paperless-scan-bridge/secrets/restic-password \
          check
   ```
5. **Resume backup jobs**

**Prevention:**

- Synology share permissions restrict who can delete the restic
  folder
- Repository immutability via S3 Object Lock (if using S3 off-site)
- Regular off-site backup verification

### 7.6 Total disaster: everything destroyed

**Symptoms:**

- House fire, flood, theft, electrical surge
- Pi, Docker host, and Synology all destroyed
- Off-site backup is the only remaining copy

**Recovery time:** 1 working day for a competent operator with
prepared resources

**Procedure:**

1. **Procure new hardware**: Pi, scanner, Synology (or rented cloud
   alternatives), Docker host (rented VPS works as bridge)
2. **Recover the age private key**: from the printed copy in the
   safe deposit box, or from the family member you entrusted with
   a copy, or from the encrypted USB stick in your bug-out bag
3. **Configure new Synology**: install DSM, create a user account
   matching the original
4. **Restore restic repository** from the off-site target to the new
   Synology
5. **Verify integrity**: `restic check`
6. **Bootstrap new Pi and Docker host** following sections 6.4 and
   6.3

**Prevention:**

- Off-site backup is non-negotiable
- Age key in physically separate location
- Documentation accessible without the running system (printed copy,
  family member, keybase)

### 7.7 Configuration drift

**Symptoms:**

- A change made in production but not committed to the repository
- Unable to reproduce the running config in development
- Containers behaving differently after a redeploy

**Recovery time:** 30-60 minutes

**Procedure:**

1. **Diff** the running config against the repository:
   ```bash
   diff -ru /etc/paperless-scan-bridge/ \
            /home/user/paperless-scan-bridge-repo/deploy/
   ```
2. **For each difference, decide**: should it be committed to the
   repo, or reverted to match the repo?
3. **Commit the deltas** that should persist
4. **Revert the deltas** that should not
5. **Establish a habit**: every config change goes through a PR

**Prevention:**

- Read-only mounts of config into containers (already standard)
- File integrity monitoring on the config directory
- Quarterly config drift audit

---

## 8. Periodic restore testing

A backup is a hypothesis. A successful restore is the experiment
that proves it.

### 8.1 Quarterly full restore test

Once per quarter, perform a full restore to a dedicated test
environment. Procedure:

1. **Provision a test VM** (any cloud or local hypervisor) with
   Ubuntu Server 24.04
2. **Mount the Synology share** read-only (so the test can't
   corrupt production)
3. **Run the disaster recovery procedure** from section 6.3 against
   the test VM
4. **Verify**:
   - Paperless web UI loads
   - A known document is searchable
   - Tags and correspondents are intact
   - The bridge `/health` returns 200
   - The synthetic test scan completes
5. **Time the recovery** and compare to RTO target
6. **Document** the test in `backup/restore-test-log.md`:

```markdown
## 2026-04-30 — Quarterly restore test

**Operator:** Björn
**Test environment:** Hetzner CX21 VM
**Snapshot used:** 2026-04-29T02:30 (1 day old)
**Total time:** 78 minutes

**Steps timed:**
- VM provisioning: 5 min
- Synology mount: 2 min
- Restic restore: 47 min (52 GB dataset)
- PostgreSQL restore: 8 min
- Compose stack startup: 5 min
- Verification: 11 min

**Issues encountered:**
- Initial restic restore failed with "no parent snapshot" — was a
  permissions issue on the test mount; fixed by using --no-cache
- Paperless took longer than expected to come up; needed to wait for
  full index rebuild

**Lessons learned:**
- Need to document the --no-cache workaround in the runbook (DONE,
  see commit a1b2c3d)
- Should pre-warm the Paperless search index before declaring
  recovery complete
```

### 8.2 Monthly automated check

The CI pipeline includes a monthly automated check that pulls a
recent snapshot, mounts it via `restic mount`, and verifies the
expected directory structure exists. This is not a full restore but
catches gross repository corruption.

### 8.3 Triggered tests

Whenever the backup infrastructure changes (restic version upgrade,
Synology DSM upgrade, change of off-site target), run a restore test
within one week. The change is not "done" until the test passes.

---

## 9. Backup integrity verification

### 9.1 Weekly subset check

Every Sunday at 05:00, the system runs:

```bash
restic -r /mnt/synology/backup/restic-paperless-scan-bridge \
       --password-file /etc/paperless-scan-bridge/secrets/restic-password \
       check --read-data-subset=10%
```

This reads 10% of the actual data blocks (different 10% each week,
randomized) and verifies their cryptographic hashes. Over ten weeks,
this covers the entire repository statistically.

### 9.2 Monthly full check

On the first of each month at 02:00:

```bash
restic check --read-data
```

This reads every block. Slow (1-6 hours) but thorough. Catches bit
rot, silent corruption, or any restic-internal issues.

### 9.3 Failure handling

A failed integrity check is a serious event. The procedure:

1. **Alert immediately** (paging-class)
2. **Do not run further backups** until investigated (avoid
   propagating corruption)
3. **Compare to off-site copy**: if off-site is intact, plan a
   re-seed from off-site
4. **If both are corrupt**: investigate the underlying storage
   (Synology disk health, S3 object integrity)
5. **Document** the incident

---

## 10. Key management

### 10.1 The age private key

The single most important secret in the system.

**Where it lives in normal operation:**

- On the maintainer's workstation at `~/.config/sops/age/keys.txt`
- On the Docker host at `/etc/paperless-scan-bridge/secrets/sops.txt`
  (so automated decryption can happen)

**Where backup copies live:**

- Printed on paper in a safe deposit box
- Printed on paper in a separate physical location (parents' house,
  trusted family member)
- On an encrypted USB stick in the bug-out bag
- Optionally: split via Shamir's Secret Sharing across trusted
  parties

**What to do if it is lost:**

You cannot recover anything encrypted with it. Period. Treat this
with appropriate gravity.

**What to do if it is compromised:**

See section 7.4 for the rotation procedure.

### 10.2 The restic password

A symmetric password used to encrypt the restic repository.

**Where it lives:**

- SOPS-encrypted at `/etc/paperless-scan-bridge/secrets/restic-password.age`
- Decrypted at runtime to `/etc/paperless-scan-bridge/secrets/restic-password`
  (file mode 600)

**Backup copies:** not separately backed up; recoverable as long as
the age key is intact

### 10.3 The API token

A bearer token for the bridge HTTP API.

**Where it lives:**

- SOPS-encrypted at `/etc/paperless-scan-bridge/secrets/api-token.age`
- Decrypted into the container as a `_FILE` mount

**Backup copies:** not separately backed up

### 10.4 NAS credentials

The Synology user account credentials for the NFS share.

**Where they live:**

- In `/etc/fstab` configuration via `credentials=/etc/.smbcredentials`
  format if SMB is used; not stored if NFSv4 with sec=sys is used
  (relies on UID matching)

**Backup copies:** documented in the Synology DSM admin UI, password
manager entry

---

## 11. Migration scenarios

### 11.1 Migrating to a different NAS

Scenario: Replacing Synology with TrueNAS, QNAP, or similar.

```bash
# Step 1: On the new NAS, create the equivalent share with NFS
# enabled and the appropriate UID/GID mapping

# Step 2: Sync existing data
rsync -av --progress \
    /mnt/synology-old/volume1/scans/ \
    /mnt/new-nas/scans/

rsync -av --progress \
    /mnt/synology-old/volume1/backup/ \
    /mnt/new-nas/backup/

# Step 3: Update /etc/fstab on Pi and Docker host to point at new NAS

# Step 4: Update Hyper Backup (or equivalent) configuration

# Step 5: Verify
restic -r /mnt/new-nas/backup/restic-paperless-scan-bridge \
       --password-file /etc/paperless-scan-bridge/secrets/restic-password \
       check
```

Time required: depends on data volume; primarily limited by network
or USB transfer speed.

### 11.2 Migrating to a different Docker host

Scenario: Moving Paperless to a more powerful host, or changing
hardware vendors.

This is a controlled version of the disaster recovery in section
6.3. Schedule a maintenance window, follow the procedure, verify,
and decommission the old host.

### 11.3 Migrating away from this project

Scenario: User decides Paperless email ingestion is enough, wants
to retire the bridge.

The data lives in Paperless. Retiring the bridge does not affect
the data. Procedure:

1. Stop the bridge containers
2. Document profile mappings somewhere (email subjects → tags)
3. Configure email ingestion in Paperless to replicate the
   workflow
4. Optionally: keep the bridge containers but stopped, for an
   easy revert
5. After 30 days of no regret, decommission

The reverse (returning to the bridge later) is also supported.
The configuration tree under `/etc/paperless-scan-bridge/` can sit
inactive indefinitely.

---

## 12. Lessons learned and post-incident review

### 12.1 The post-incident process

After any incident that triggers the procedures in this document
(planned or unplanned), conduct a post-incident review within one
week. Capture in `backup/incident-log.md`:

- What happened, in chronological order
- Root cause (with appropriate humility — first hypothesis is rarely
  the final answer)
- What worked well in the response
- What did not work well
- Action items, with owner and target date
- Whether the runbook in this document accurately predicted what to
  do; if not, where it was wrong

The action items become issues in the GitHub repository.

### 12.2 Ongoing improvements

This document evolves based on what we learn. Every incident is a
data point. Every restore test is a data point. The runbooks should
be living artifacts that reflect actual experience, not just
theoretical procedures.

If you operate this stack and encounter a scenario not covered here,
please open an issue or PR. Even a "this happened to me, here's what
I did" note adds value to future operators.

---

*Backup is the discipline you regret skipping the day you need it.
This document represents the discipline we apply ourselves; it is
shared in the hope that others find it useful for their own setups.*
