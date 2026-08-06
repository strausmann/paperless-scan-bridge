# Getting started

This section takes you from a fresh Raspberry Pi to a document landing
in Paperless-ngx.

!!! warning "Not yet runnable"

    Phase 1 is still in progress. The bootstrap script and the compose
    stacks referenced below do not exist in the repository yet. The pages
    in this section describe the intended workflow and are updated as each
    piece lands.

## Pages

- [Quickstart](quickstart.md) — prerequisites, bootstrap, first scan
- [Scan profiles](scan-profiles.md) — how profiles are defined and
  selected

## Before you begin

You need three things:

1. **A Raspberry Pi 4 or 5** running Ubuntu Server 24.04 LTS (arm64)
   with a SANE-compatible USB scanner attached. Reference hardware is a
   Pi 5 with 8 GB RAM, an SSD over USB 3.0, and a Kodak ScanMate i1120.
2. **A Synology NAS** with NFS enabled. The NAS is the single source of
   truth for documents; the Pi is an ingestion node, not a storage node.
3. **A Docker host for Paperless-ngx.** This can be the NAS itself, a
   mini-PC, or any always-on Linux box. It does not have to be the Pi.

Check the [hardware compatibility list](../hardware/index.md) before
buying anything.
