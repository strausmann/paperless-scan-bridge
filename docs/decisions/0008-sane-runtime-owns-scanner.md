# 0008 — sane-runtime owns the scanner via device passthrough + udev, never `--privileged`

- **Status:** Proposed
- **Date:** 2026-06-28
- **Deciders:** strausmann
- **Tags:** sane-runtime, deploy

## Context
Only one container needs the USB scanner, and it must get access with least privilege.

## Decision
We will give **only `sane-runtime`** scanner access via specific **device-cgroup permissions + a
udev-managed stable symlink**. We will **not** use `--privileged`; containers `cap_drop: [ALL]` and add
back only what's required (e.g. `CAP_SYS_RAWIO`).

## Options considered
- **Option A — device passthrough + udev, least privilege (chosen):** safe, scoped to one container.
- **Option B — `--privileged`:** broad host access; rejected.

## Consequences
- **Open detail to reconcile:** the exact node differs across sources — `ARCHITECTURE.md`/issue #9 say
  `--device=/dev/bus/usb`, while `CONTAINER_SUITE.md` §9.4 maps the narrower symlink
  `/dev/scanner-i1120`. The *privilege* decision is firm; the device-path detail should be unified.

## References
- `ARCHITECTURE.md` ("does not require --privileged"); `AGENTS.md`; `CONTAINER_SUITE.md` §9 / §2.8.
