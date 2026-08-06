# 0002 — Container-first, host-thin

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** deploy, docker

## Context
The scanning host (Pi or any always-on Linux box) should be disposable and easy to rebuild; all logic
should be portable and versioned.

## Decision
We will keep **all functionality in containers**. The only acceptable host modifications are: install
Docker + the compose plugin, mount the Synology NFS share via `/etc/fstab`, and install udev rules.
**No** SANE/scanbd/language runtimes installed on the host.

## Options considered
- **Option A — container-first, host-thin (chosen):** disposable host, reproducible, portable.
- **Option B — host-installed SANE/services:** ties the setup to a hand-configured host; rejected.

## Consequences
- A feature that seems to need a host install must be containerized instead.
- The host is an ingestion node, trivially re-flashable.

## References
- `ARCHITECTURE.md` (host-thin, bootstrap list); `AGENTS.md`; `CLAUDE.md` (non-negotiable principles);
  `CONTAINER_SUITE.md` §2.
