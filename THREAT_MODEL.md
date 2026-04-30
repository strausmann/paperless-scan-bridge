# Threat Model

> **Status:** Draft v1.0
> **Last updated:** 2026-04-30
> **Methodology:** STRIDE
> **Audience:** Security-conscious operators, contributors evaluating
> security implications of changes, and reviewers performing security
> audits.

## Purpose

This document analyzes the security posture of `paperless-scan-bridge`
using the STRIDE methodology. STRIDE is an acronym for the six
categories of threat we evaluate:

- **S**poofing
- **T**ampering
- **R**epudiation
- **I**nformation disclosure
- **D**enial of service
- **E**levation of privilege

For each threat, we identify the asset at risk, the attack vector,
the likelihood and impact, and the mitigations in place or planned.

This is not a guarantee of security. Threat models capture what we
have thought about; they cannot capture what we have not. New threats
emerge as the system evolves. This document is updated when:

- A new component is added or an existing component changes role
- A new attack class becomes practical (e.g. a class of supply-chain
  attacks we did not previously consider)
- A vulnerability is discovered that the model did not anticipate

---

## Table of contents

1. [Assets and what we protect](#1-assets-and-what-we-protect)
2. [Trust boundaries](#2-trust-boundaries)
3. [Attacker profiles](#3-attacker-profiles)
4. [Data flow diagram](#4-data-flow-diagram)
5. [STRIDE analysis](#5-stride-analysis)
6. [Mitigations summary](#6-mitigations-summary)
7. [Residual risks](#7-residual-risks)
8. [What we do not protect against](#8-what-we-do-not-protect-against)
9. [Compliance and regulatory considerations](#9-compliance-and-regulatory-considerations)
10. [Threat model maintenance](#10-threat-model-maintenance)

---

## 1. Assets and what we protect

In rough order of value:

### 1.1 The documents themselves

The scanned PDFs and the OCR'd archive are the primary asset. They
contain whatever the user scans — bills, contracts, medical records,
tax documents, identity documents, business correspondence. The
sensitivity is high by default.

**Protection objectives:**

- Confidentiality: only the user can read the documents
- Integrity: documents cannot be modified by an attacker without
  detection
- Availability: documents remain accessible to the legitimate user

### 1.2 The Paperless metadata

The PostgreSQL database with tags, correspondents, document types,
and full-text search index. Compromise of metadata reveals what
documents exist, even without revealing their contents.

**Protection objectives:** same as documents.

### 1.3 The trigger and dispatch infrastructure

Webhook endpoints, the bridge daemon, the dispatch socket. Compromise
allows an attacker to trigger scans, observe scan activity, and
potentially exfiltrate data through scan profiles.

**Protection objectives:**

- Authentication: only authorized callers can trigger scans
- Authorization: trigger calls cannot escalate privilege
- Auditability: every action is logged with provenance

### 1.4 The Synology NAS share

The shared filesystem that hosts the consume directory and the
restic backup repository. Compromise of the NAS account is
catastrophic.

**Protection objectives:** Confidentiality and integrity at the
storage layer, with backup encryption providing defense in depth.

### 1.5 The credentials and secrets

API tokens, restic encryption keys, the SOPS age key. These are
the keys to other doors.

**Protection objectives:** Confidentiality at all times, with
key rotation procedures documented for compromise scenarios.

### 1.6 The system configuration

Compose files, environment files, profile YAML, udev rules. While
not as sensitive as documents, configuration tampering can lead to
all other compromises.

**Protection objectives:** Integrity, with version control and
review processes serving as the primary defense.

---

## 2. Trust boundaries

The system has six trust zones, each with a defined level of trust
and defined boundaries to its neighbors.

### 2.1 The trusted zone (highest trust)

**What is here:**

- The Synology NAS, on its administrative interfaces
- The user's primary workstation, with full SSH access
- Maintainer-controlled secrets (SOPS age private key, restic
  password, NAS admin credentials)

**Why trusted:** This is the user. Compromise here means the
attacker is the user.

### 2.2 The infrastructure zone

**What is here:**

- The Pi running the bridge containers
- The Docker host running Paperless-ngx
- The LAN segment they share

**Why trusted:** These devices hold credentials to the storage
zone and execute the scan pipeline. They must be trusted with
that responsibility.

**Boundary to trusted zone:** SSH access is limited to the
user's workstation; no other source can SSH in. CrowdSec and
fail2ban-style protections monitor for brute force attempts.

### 2.3 The application zone

**What is here:**

- The three custom containers (`scan-bridge`, `sane-runtime`,
  `scan-processor`)
- Paperless-ngx and its supporting containers
- The communication paths between them

**Why semi-trusted:** Containers are isolated from each other and
from the host. A compromise of one container should not lead to
compromise of others or of the host.

**Boundary to infrastructure zone:** Containers run as non-root
users with dropped capabilities. Read-only root filesystems prevent
in-container persistence. Only explicitly mounted volumes and
devices cross the boundary.

### 2.4 The trigger source zone

**What is here:**

- Home Assistant
- n8n (if used)
- The Zigbee mesh and remote devices
- HTTP clients triggering scans (curl, custom scripts, mobile apps)

**Why semi-trusted:** These devices originate trigger events. They
have the API token. They are network-exposed and may be compromised
through other vectors.

**Boundary to application zone:** Authenticated API endpoint with
rate limiting. IP allowlist option for additional defense in depth.
Token can be rotated without container rebuild.

### 2.5 The peripheral zone

**What is here:**

- The USB scanner itself
- The udev rules that grant scanner access

**Why semi-trusted:** USB devices are notoriously insecure (BadUSB,
firmware attacks, etc.). The scanner has direct access to the host
USB stack.

**Boundary to infrastructure zone:** udev rules limit access to
specific known vendor/product IDs. Scanner is in an isolated USB
controller from the storage USB controller (where applicable). The
sane-runtime container is the only consumer of the scanner.

### 2.6 The untrusted zone (lowest trust)

**What is here:**

- The internet
- Any device not explicitly enumerated in another zone
- Third-party scanner manufacturer's assumed update servers
- Cloud services we do not directly control

**Why untrusted:** Default deny for any communication crossing this
boundary.

**Boundary to all other zones:** No inbound connections from this
zone are accepted. Outbound connections are limited to known-good
destinations: GHCR for image pulls, Renovate for dependency updates,
GitHub for repository sync. Documentation site is the only artifact
served outward.

### 2.7 Trust boundary diagram

```
+------------------+
| TRUSTED ZONE     |
| User workstation |
| NAS admin UI     |  <-- highest trust
| Maintainer keys  |
+--------+---------+
         | SSH, NAS admin protocol
         v
+------------------+
| INFRASTRUCTURE   |
| Pi host          |
| Docker host      |  <-- holds credentials, runs containers
| LAN segment      |
+--------+---------+
         | Docker, Compose, named pipes
         v
+------------------+        +-----------------+
| APPLICATION      |<------>| TRIGGER SOURCES |
| scan-bridge      |  HTTPS | Home Assistant  |
| sane-runtime     |  with  | n8n             |
| scan-processor   |  token | Zigbee/curl     |
| Paperless        |        +-----------------+
+--------+---------+
         | USB device cgroup
         v
+------------------+
| PERIPHERAL       |
| USB scanner      |  <-- semi-trusted hardware
+------------------+

           UNTRUSTED ZONE
       Internet, world, etc.   <-- default deny inbound
```

---

## 3. Attacker profiles

We model three classes of attacker. Defenses scale to the most
capable attacker we plausibly face.

### 3.1 The casual network intruder

**Profile:** An attacker on the same LAN segment, perhaps a guest
Wi-Fi user, perhaps a compromised IoT device, perhaps a malicious
browser extension performing internal port scans.

**Capabilities:** Can scan local ports, send unauthenticated requests
to LAN services, possibly perform ARP spoofing.

**Motivations:** Opportunistic. Looking for low-hanging fruit.

**Mitigations:** API authentication, IP allowlist, network
segmentation (IoT VLAN), firewall rules.

### 3.2 The targeted external attacker

**Profile:** An attacker who knows the user has this stack and
specifically wants their documents. May be a former domestic abuser,
a litigation adversary, or an industrial espionage actor.

**Capabilities:** Skilled, patient, willing to invest weeks. May
combine social engineering with technical attacks. Has internet
access only — no LAN presence.

**Motivations:** Specific data exfiltration.

**Mitigations:** No internet exposure of any service. All
maintainer accounts use 2FA. No phone-home telemetry. Hardware
keys for repository signing. Encrypted backups with offline copy.

### 3.3 The supply chain attacker

**Profile:** An attacker who compromises an upstream dependency
(a Go module, a Debian package, a base image) to push malicious
code into our containers.

**Capabilities:** Can compromise package registries, maintainer
accounts on upstream projects, build infrastructure.

**Motivations:** Broad, opportunistic. Wants access to many
downstream users, of which we are one.

**Mitigations:** Dependencies pinned by hash where possible.
Container images pinned by digest. Base images from major vendors
only (Debian, Google distroless). govulncheck and Trivy scans on
every build. SBOM published per release. Renovate updates
reviewed by a human, never auto-merged.

### 3.4 Out of scope

We do not defend against:

- **Nation-state attackers with hardware implant capability.** A
  Pi taken out of the supply chain and compromised at the silicon
  level is not detectable by anything in this project.
- **Determined attacker with physical access for extended periods.**
  An attacker with the Pi in their hands for hours has the
  filesystem, the keys, and the documents.
- **The user themselves.** If the user wants to leak their own
  documents, no defense in this project prevents that.

---

## 4. Data flow diagram

The high-level data flow with trust boundaries marked:

```
   [TRIGGER ZONE]                                      [APPLICATION ZONE]
   +--------------------+      HTTPS + Token         +-------------------+
   | Home Assistant     | -------------------------->|   scan-bridge     |
   | n8n                |                            |   (REST API)      |
   | curl, mobile app   |                            +---------+---------+
   +--------------------+                                      |
                                                               | Unix socket
                                                               | (named volume)
                                                               v
   [PERIPHERAL ZONE]                                  +-------------------+
   +--------------------+                             |   sane-runtime    |
   |   USB scanner      |<------ device cgroup ------>|   (SANE, scanbd)  |
   +--------------------+                             +---------+---------+
                                                               | shared volume
                                                               | (raw images)
                                                               v
                                                      +-------------------+
                                                      |  scan-processor   |
                                                      |  (PDF assembly)   |
                                                      +---------+---------+
                                                               | atomic NFS write
                                                               v
   [INFRASTRUCTURE ZONE]                              +-------------------+
   +--------------------+      NFS / iSCSI            |  consume dir      |
   |  Synology NAS      |<--------------------------->|  (Paperless picks)|
   |  (storage hub)     |                             +-------------------+
   +--------------------+                                      |
            |                                                  |
            | Hyper Backup                                     v
            v                                          +-------------------+
   [UNTRUSTED ZONE]                                    | Paperless-ngx     |
   +--------------------+                              | (OCR, indexing)   |
   |  Off-site target   |<-- restic + Hyper Backup --->| PostgreSQL        |
   |  (S3, second NAS)  |                              +-------------------+
   +--------------------+
```

Each arrow is a data flow with attendant threats analyzed below.

---

## 5. STRIDE analysis

For each STRIDE category, we walk through the relevant threats,
their likelihood, their impact, and the mitigations.

### 5.1 Spoofing

**S1 — Forged trigger event from unauthorized source**

| Aspect | Description |
| --- | --- |
| Asset | Bridge daemon, scan pipeline |
| Attacker | Casual or targeted network intruder |
| Vector | Unauthenticated HTTP request to `/scan` |
| Likelihood | Medium without auth; Low with token |
| Impact | Medium (unauthorized scans, possible exfiltration via crafted profiles) |

**Mitigations:**

- Token-based authentication on all non-health endpoints
- IP allowlist as an opt-in additional layer
- Rate limiting (100 requests per minute per source IP) caps blast
  radius even if a token is leaked
- Structured logs capture every authentication failure with source IP
- Tokens can be rotated without service interruption via `SIGHUP`
  signal

**S2 — Forged identity in webhook source field**

| Aspect | Description |
| --- | --- |
| Asset | Audit logs and observability |
| Attacker | Insider or compromised trigger source |
| Vector | Lying about which device or automation triggered the scan |
| Likelihood | Medium |
| Impact | Low (audit trail integrity only; no direct compromise) |

**Mitigations:**

- The `source` field in scan requests is treated as an annotation,
  not as authoritative identity
- The source IP from the TCP connection is the authoritative
  identifier in audit logs
- Both fields are logged so discrepancies are visible

**S3 — Spoofed scanner**

| Aspect | Description |
| --- | --- |
| Asset | sane-runtime container |
| Attacker | Physical attacker with USB access |
| Vector | Plug in a malicious USB device that emulates the scanner's vendor/product ID |
| Likelihood | Low (requires physical access) |
| Impact | High (BadUSB-class attacks possible) |

**Mitigations:**

- USB device serial number can be added to udev rules to lock to a
  specific physical scanner (documented as optional)
- Scan output is sanity-checked by scan-processor before PDF assembly
  (rejecting obviously malformed image data)
- Container runs unprivileged with dropped capabilities

### 5.2 Tampering

**T1 — In-flight tampering with raw scan data**

| Aspect | Description |
| --- | --- |
| Asset | Scan content between sane-runtime and scan-processor |
| Attacker | Compromised container in the same network namespace |
| Vector | Modify files in the shared scratch volume between write and read |
| Likelihood | Low (requires container compromise first) |
| Impact | High (silent corruption of documents) |

**Mitigations:**

- Scratch volume is tmpfs, not persistent; smaller window of
  exposure
- File ownership and permissions limit access to the runtime's
  service account
- scan-processor verifies file integrity (size, format magic bytes)
  before processing
- Future: SHA-256 hash passed via the dispatch metadata, verified by
  processor

**T2 — Tampering with consume directory contents**

| Aspect | Description |
| --- | --- |
| Asset | Files awaiting Paperless ingestion |
| Attacker | Anyone with write access to the consume mount |
| Vector | Modify or replace PDFs before Paperless picks them up |
| Likelihood | Low (requires Pi or NAS compromise) |
| Impact | High (documents replaced silently) |

**Mitigations:**

- Atomic writes via `O_TMPFILE` + `linkat` ensure files appear
  fully formed
- NFS export limited to specific source IPs (Pi and Docker host
  only)
- Consume directory is write-once-read-once: Paperless removes the
  file after ingestion
- Backup snapshots provide a forensic record if tampering is
  suspected later

**T3 — Tampering with container images**

| Aspect | Description |
| --- | --- |
| Asset | Production container images |
| Attacker | Supply chain or registry compromise |
| Vector | Push a modified image with the same tag |
| Likelihood | Low (GHCR is well-defended) |
| Impact | Critical (full pipeline compromise) |

**Mitigations:**

- Production compose files reference images by digest, not tag
- Cosign keyless signatures verifiable by anyone
- Renovate detects digest changes and requires manual review
- Watchtower has explicit allowlist; never auto-pulls untrusted tags

**T4 — Tampering with the bootstrap script**

| Aspect | Description |
| --- | --- |
| Asset | First-time installer script |
| Attacker | Repository compromise or man-in-the-middle on script download |
| Vector | Modify install.sh to install backdoor or steal credentials |
| Likelihood | Low |
| Impact | Critical (post-compromise persistence on every fresh install) |

**Mitigations:**

- HTTPS only for script download
- SHA-256 of the script published in each release notes; users
  encouraged to verify
- Script source is public and reviewable
- The script does only documented, auditable things

### 5.3 Repudiation

**R1 — User claims they did not trigger a scan**

| Aspect | Description |
| --- | --- |
| Asset | Audit trail integrity |
| Attacker | Insider or curious household member |
| Vector | Plausible deniability for triggered scans |
| Likelihood | Low |
| Impact | Low (privacy concern, not security) |

**Mitigations:**

- All scan triggers logged with timestamp, source IP, profile,
  authentication mode
- Logs aggregated to long-term storage (Loki or syslog) with
  retention beyond container lifetime
- Trigger source field captures the originating system

**R2 — Repudiation of failed scan attempts**

| Aspect | Description |
| --- | --- |
| Asset | Compliance and forensic visibility |
| Attacker | Adversary attempting unauthorized scans |
| Vector | Failed authentication attempts not logged |
| Likelihood | Medium (logging configuration error) |
| Impact | Medium (loss of attack visibility) |

**Mitigations:**

- All authentication failures logged regardless of outcome
- Failed scans logged with full request metadata
- Default log level captures all auth events; lowering it requires
  explicit configuration

### 5.4 Information disclosure

**I1 — Document leakage via insecure storage**

| Aspect | Description |
| --- | --- |
| Asset | Document content |
| Attacker | NAS account compromise or filesystem-level access |
| Vector | Direct file read on the consume directory or Paperless storage |
| Likelihood | Medium (NAS compromise is realistic) |
| Impact | Critical (data exfiltration) |

**Mitigations:**

- NFS mount restricted to specific source IPs
- Synology user account for the share has no other privileges
- Volume encryption optional at the Synology level
- restic backups encrypted with age key
- Network segmentation isolates NAS from internet

**I2 — Document leakage via misconfigured webhook**

| Aspect | Description |
| --- | --- |
| Asset | Document content |
| Attacker | Network intruder |
| Vector | Triggering scans and reading results via the API |
| Likelihood | Low (API does not expose document content) |
| Impact | Medium |

**Mitigations:**

- API exposes job status and metadata, never document content
- Document retrieval requires Paperless authentication, not bridge
  authentication
- Job records contain no document content, only references

**I3 — Logs revealing document metadata**

| Aspect | Description |
| --- | --- |
| Asset | Document metadata (filenames, profiles, page counts) |
| Attacker | Anyone with log access |
| Vector | Reading logs to infer scan patterns |
| Likelihood | Medium |
| Impact | Low to Medium |

**Mitigations:**

- Logs contain no document content
- Logs contain page count, profile, duration — minimum metadata
- Log retention policies documented
- Loki access controls separate from Paperless access controls

**I4 — Secrets disclosure via container inspection**

| Aspect | Description |
| --- | --- |
| Asset | API token, restic password, NAS credentials |
| Attacker | Anyone with Docker socket access on the host |
| Vector | `docker inspect` to read environment variables |
| Likelihood | Low (requires host root) |
| Impact | High |

**Mitigations:**

- Secrets passed via `_FILE` environment variables pointing to mounted
  files, never directly as environment values
- File-mounted secrets are world-unreadable
- SOPS encryption protects secrets at rest in the repository

**I5 — Side-channel disclosure via traffic analysis**

| Aspect | Description |
| --- | --- |
| Asset | Scan timing and frequency patterns |
| Attacker | LAN observer |
| Vector | Observing network traffic patterns even if encrypted |
| Likelihood | Low |
| Impact | Low (some metadata leakage about scan activity) |

**Mitigations:**

- Considered acceptable; padding traffic patterns is not in scope
- LAN observer must already be a capable attacker

### 5.5 Denial of service

**D1 — Scan flood from compromised trigger source**

| Aspect | Description |
| --- | --- |
| Asset | Bridge availability |
| Attacker | Casual intruder or compromised HA |
| Vector | Spamming `/scan` to exhaust resources |
| Likelihood | Medium |
| Impact | Medium (legitimate scans delayed) |

**Mitigations:**

- Rate limiting at the bridge (100 req/min per source IP)
- Job queue depth metric alerts at thresholds
- Bridge does not crash on queue overflow; rejects new requests
  with 503

**D2 — Scanner monopoly via long-running scans**

| Aspect | Description |
| --- | --- |
| Asset | Scanner availability |
| Attacker | Authenticated but malicious caller |
| Vector | Starting a scan that never completes |
| Likelihood | Low |
| Impact | Medium (other scans blocked) |

**Mitigations:**

- Per-job timeout in profile configuration (default 5 minutes)
- Cancel endpoint allows administrative override
- scan-bridge metric `scan_bridge_active_jobs` alerts above 1 for
  long durations

**D3 — Disk exhaustion via massive scans**

| Aspect | Description |
| --- | --- |
| Asset | Pi and Docker host disk space |
| Attacker | Authenticated but malicious caller |
| Vector | Scanning very high-resolution color batches repeatedly |
| Likelihood | Low |
| Impact | Medium |

**Mitigations:**

- tmpfs scratch volume with size limit prevents unbounded growth
- Disk space monitoring alerts at thresholds
- Profile validation rejects unreasonable resolution/page combinations

**D4 — Backup repository corruption**

| Aspect | Description |
| --- | --- |
| Asset | Restore capability |
| Attacker | Anyone with NAS write access |
| Vector | Deleting or corrupting restic snapshots |
| Likelihood | Low |
| Impact | High (recovery impossible) |

**Mitigations:**

- Off-site backup as a second copy (Hyper Backup)
- restic check verifies repository integrity weekly
- Synology snapshot retention provides additional rollback
- Append-only backup destinations supported (S3 Object Lock,
  immutable Synology shares)

### 5.6 Elevation of privilege

**E1 — Container escape**

| Aspect | Description |
| --- | --- |
| Asset | Host system |
| Attacker | Compromised container |
| Vector | Kernel exploit, Docker bug, misconfiguration |
| Likelihood | Low (modern Docker is well-isolated) |
| Impact | Critical (full host compromise) |

**Mitigations:**

- Containers run as non-root users
- All capabilities dropped by default; only specific ones added back
- `no-new-privileges: true` set on every container
- Read-only root filesystem prevents in-container persistence
- AppArmor or SELinux profiles enabled by default on supported hosts
- Regular base image updates via Renovate
- Minimal attack surface in distroless images

**E2 — USB device escalation**

| Aspect | Description |
| --- | --- |
| Asset | Host kernel |
| Attacker | Malicious USB device |
| Vector | Exploit in USB driver triggered by crafted device |
| Likelihood | Low (requires specific kernel CVE) |
| Impact | Critical |

**Mitigations:**

- Specific device cgroup permissions, not full USB access
- udev rule scoped to known scanner vendor/product ID
- Kernel updates via unattended-upgrades on Ubuntu Server
- USB devices on a separate USB controller from boot/storage
  devices when possible

**E3 — Privilege escalation via the bootstrap script**

| Aspect | Description |
| --- | --- |
| Asset | Pi root account |
| Attacker | Anyone who tampers with the script before execution |
| Vector | Modify the script during download or in the user's filesystem |
| Likelihood | Low |
| Impact | Critical |

**Mitigations:**

- HTTPS-only download
- SHA-256 verification step documented (and recommended)
- Script source is reviewable; no obfuscation
- Script does only documented operations

**E4 — Privilege escalation via Compose configuration injection**

| Aspect | Description |
| --- | --- |
| Asset | Container runtime privileges |
| Attacker | Anyone with write access to the compose directory |
| Vector | Modify compose to grant elevated capabilities |
| Likelihood | Low (requires Pi compromise first) |
| Impact | High |

**Mitigations:**

- Compose directory permissions restrict write access to the
  service account
- File integrity monitoring (optional, via AIDE or tripwire)
  detects unauthorized changes
- All compose files in version control; drift is visible

---

## 6. Mitigations summary

A consolidated list of the active and planned mitigations, organized
by which threat category they address most directly.

### 6.1 Authentication and access control

- Token-based authentication on the bridge API
- IP allowlist as opt-in defense in depth
- Rate limiting per source IP
- NFS share restricted to specific source IPs
- SSH key-based authentication only on the Pi
- Two-factor authentication on maintainer GitHub accounts

### 6.2 Encryption

- restic encrypts every snapshot with age key
- SOPS encrypts secrets at rest in the repository
- TLS at the reverse proxy for any internet-exposed service (out of
  scope for this project but documented)
- Optional NFSv4 with Kerberos for hostile-LAN scenarios

### 6.3 Container hardening

- Non-root users in every container
- All capabilities dropped, only specific ones added back
- `no-new-privileges: true` always
- Read-only root filesystem
- Distroless or slim base images
- Multi-stage builds
- Container images pinned by digest in production

### 6.4 Supply chain

- Cosign keyless signing on every release image
- SBOM published per release
- SLSA Level 3 provenance attestations
- Dependency pinning via `go.sum`, package digests
- Trivy and govulncheck in CI
- Renovate for dependency updates with human review

### 6.5 Detection and response

- Structured logs to stdout, easily aggregated
- Trace IDs propagating across containers
- Prometheus metrics with alerting
- Synthetic health checks every hour
- CrowdSec integration for active threat response (Phase 3)

### 6.6 Resilience

- Three storage topologies for different threat profiles
- Backup with restic, off-site replication via Hyper Backup
- Cold-standby procedures documented in DISASTER_RECOVERY.md
- Periodic restore tests in CI

---

## 7. Residual risks

After all mitigations, the following risks remain. We accept them
because mitigation cost outweighs the expected impact.

| Residual risk | Why we accept it |
| --- | --- |
| Side-channel timing analysis on the LAN | LAN attacker is already very capable; specific defense against traffic analysis is disproportionate |
| Quantum-computer attacks on age encryption | Not currently practical; will revisit if it becomes so |
| Supply chain compromise of the Linux kernel | Not actionable from project scope |
| Maintainer account compromise via undisclosed 2FA bypass | Hardware key mitigates most; absolute prevention impossible |
| Hardware implant in Pi or Synology supply chain | Not detectable by software; out of scope |

---

## 8. What we do not protect against

Honest scope statement; restated from CONCEPT.md and elaborated.

**Physical attacker with extended access to the Pi.** The Pi has
NFS mount credentials in plain form (limitation of the kernel NFS
client). The udev rules grant USB access. SSH keys for the user
account are on the Pi if SSH access is configured. An attacker with
the Pi in their hands for an hour has all of this.

**Compromised Synology administrator account.** The NAS holds the
encrypted restic repository, the consume directory, and (in
Topology B/C) the live document storage. A NAS admin can read or
delete all of it. The off-site backup mitigates only the deletion
side; reading is not preventable from this position.

**Insider with legitimate access.** Anyone the user has granted
access to Paperless can read the documents. We do not implement
intra-Paperless access controls beyond what Paperless itself
provides.

**Malicious upstream code.** A backdoor in Paperless-ngx, in SANE,
in Debian base images, in Go, or in any of the hundreds of
dependencies is not something we can detect on our own. We accept
this risk and rely on the broader open source security ecosystem.

**The user themselves.** If the user wants to leak their own
documents, they can. If they want to delete everything, they can.
This is by design.

**Coercion of the maintainer.** If someone forces the maintainer
to push a malicious commit, no automated process detects this.
Branch protection, review requirements, and signed commits raise
the bar but do not eliminate the threat.

---

## 9. Compliance and regulatory considerations

This project is a personal homelab tool. It is not designed for or
warranted as compliant with specific regulatory frameworks. However,
the following considerations may be relevant to users in regulated
contexts.

### 9.1 GDPR (EU General Data Protection Regulation)

Documents may contain personal data of the user or third parties.
Users handling third-party data are responsible for:

- Establishing a lawful basis for processing
- Implementing appropriate technical and organizational measures
  (this project's encryption and access controls may be one such
  measure, but the user must evaluate sufficiency)
- Honoring data subject rights (right to access, rectification,
  erasure)

This project does not transmit personal data to any third party.
The maintainer does not have access to user documents.

### 9.2 HIPAA (US Health Insurance Portability and Accountability Act)

This project is not designed for HIPAA-regulated workflows. Users
with PHI requirements should evaluate the full stack (Synology,
Paperless-ngx, this project) against HIPAA requirements
independently and likely require additional controls.

### 9.3 SOC 2, ISO 27001

These frameworks evaluate organizations, not software. Users
operating this stack within an SOC 2 / ISO 27001-certified
organization should ensure the deployment fits their existing
control framework.

### 9.4 What this project provides toward compliance

- Auditable logs with structured format
- Encryption at rest and in transit (with documented gaps)
- Documented threat model (this document)
- Documented backup and recovery procedures
- Open source code, fully reviewable
- Reproducible builds and signed releases

### 9.5 What this project does not provide

- Formal compliance attestations
- Compliance officer support
- Regulated-environment hardening profiles (CIS Benchmarks, STIG)
- Indemnification for compliance failures

Users with regulatory requirements should treat this project as a
component subject to their own compliance evaluation, not as a
compliant product.

---

## 10. Threat model maintenance

### 10.1 When to update

This document is reviewed and updated when:

- A new component is added or an existing component changes role
- A new attack class becomes practical (zero-day, novel research)
- A vulnerability is discovered that the model did not anticipate
- A major version release introduces architectural changes
- At minimum, annually

### 10.2 Review process

1. Maintainer reviews recent CVEs in dependencies
2. Maintainer walks through STRIDE categories against any new
   architecture
3. Outstanding items become issues in the repository
4. CHANGELOG entry calls out threat-model-relevant changes

### 10.3 External feedback

Security researchers and contributors are invited to suggest
additions to this document. Threat scenarios we have not
considered are exactly the gap this document is designed to close
over time. Submit suggestions through the channels in
[SECURITY.md](SECURITY.md) for sensitive observations, or via
GitHub Discussions for general additions.

---

*A threat model is a living artifact. The version that ships at v1.0
is a starting point, not a finished product. Every contributor and
operator helps it improve.*
