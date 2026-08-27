#!/usr/bin/env bash
set -euo pipefail

# paperless-scan-bridge host bootstrap.
#
# Prepares a fresh Raspberry Pi (or any Debian/Ubuntu host) for the
# container stack. Deliberately small: the container-first principle in
# CLAUDE.md permits exactly three host modifications, and this script
# does those three and nothing else.
#
#   1. Docker Engine + the compose plugin, from Docker's own repository
#   2. the Synology NFS share, mounted via /etc/fstab
#   3. the udev rule that makes the scanner readable without root
#
# No SANE on the host. No scanbd. No Python, no Go, no language runtime.
# If a future feature seems to need one, it belongs in a container.
#
# Every step is idempotent -- running this twice is a supported way to
# repair a half-finished setup, and each step says whether it changed
# anything. Nothing is removed: an existing fstab line or udev rule is
# reported and left alone rather than rewritten, because this script
# runs as root on a machine it did not set up.
#
# Usage:
#   curl -fsSLO https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/bootstrap/install.sh
#   less install.sh          # read it before running it as root
#   sudo bash install.sh --nfs-server 192.168.1.10 --nfs-export /volume1/scans
#
#   --dry-run   print every action without performing it
#
# Piping straight into a shell is deliberately not documented: a
# truncated download would execute as a half-script, as root.

readonly FSTAB=/etc/fstab
readonly UDEV_RULE=/etc/udev/rules.d/99-paperless-scan-bridge.rules
readonly RULE_URL=https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/main/deploy/udev/99-paperless-scan-bridge.rules

NFS_SERVER=""
NFS_EXPORT=""
MOUNT_POINT=/mnt/synology
DRY_RUN=0
CHANGED=0

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warn\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror\033[0m %s\n' "$*" >&2; exit 1; }
skip() { printf '      %s\n' "$*"; }
did()  { printf '\033[1;32m   ok\033[0m %s\n' "$*"; CHANGED=1; }

# run executes a command, or prints it under --dry-run. Every mutating
# action in this script goes through here, so --dry-run is exhaustive by
# construction rather than by remembering to add a branch each time.
run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '      would run: %s\n' "$*"
    return 0
  fi
  "$@"
}

usage() {
  sed -n '3,32p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --nfs-server) NFS_SERVER="${2:-}"; shift 2 ;;
      --nfs-export) NFS_EXPORT="${2:-}"; shift 2 ;;
      --mount-point) MOUNT_POINT="${2:-}"; shift 2 ;;
      --dry-run) DRY_RUN=1; shift ;;
      -h|--help) usage 0 ;;
      *) die "unknown argument: $1 (try --help)" ;;
    esac
  done
}

require_root() {
  [[ "$DRY_RUN" -eq 1 ]] && return 0
  [[ "$(id -u)" -eq 0 ]] || die "must run as root (sudo bash $0)"
}

require_debian_like() {
  command -v apt-get >/dev/null 2>&1 \
    || die "apt-get not found — this script targets Debian/Ubuntu (the reference host is Ubuntu Server 24.04 arm64)"
}

# ---------------------------------------------------------------------
# 1. Docker Engine + compose plugin
# ---------------------------------------------------------------------
install_docker() {
  log "Docker Engine and the compose plugin"

  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    skip "already present: $(docker --version), $(docker compose version | head -1)"
    return 0
  fi

  # Docker's convenience script rather than the distribution package:
  # Debian and Ubuntu ship docker.io, which lags well behind and has
  # historically not carried the compose *plugin* at all, only the
  # standalone docker-compose v1 that this project's compose files do
  # not target.
  run apt-get update -qq
  run apt-get install -y --no-install-recommends ca-certificates curl
  run install -m 0755 -d /etc/apt/keyrings

  if [[ ! -f /etc/apt/keyrings/docker.asc ]]; then
    run curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    run chmod a+r /etc/apt/keyrings/docker.asc
  fi

  local codename arch
  # shellcheck source=/dev/null  # /etc/os-release is generated per host
  codename="$(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")"
  arch="$(dpkg --print-architecture)"
  run tee /etc/apt/sources.list.d/docker.list >/dev/null <<EOF
deb [arch=${arch} signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${codename} stable
EOF

  run apt-get update -qq
  run apt-get install -y --no-install-recommends \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  did "Docker installed"
}

# ---------------------------------------------------------------------
# 2. NFS mount
# ---------------------------------------------------------------------
install_nfs_mount() {
  log "Synology NFS mount at ${MOUNT_POINT}"

  if [[ -z "$NFS_SERVER" || -z "$NFS_EXPORT" ]]; then
    skip "skipped: --nfs-server and --nfs-export not given"
    warn "the stack needs a share for documents; re-run with both flags when you have one"
    return 0
  fi

  run apt-get install -y --no-install-recommends nfs-common

  # grep -F: an export path contains slashes and, on Synology,
  # frequently a '+' in the volume name. Treating it as a regex would
  # match the wrong line or none at all.
  if grep -qF "${NFS_SERVER}:${NFS_EXPORT}" "$FSTAB"; then
    skip "already in ${FSTAB}: ${NFS_SERVER}:${NFS_EXPORT}"
    return 0
  fi

  run mkdir -p "$MOUNT_POINT"

  # nofail is what keeps a Pi bootable when the NAS is down or slow to
  # come up: without it systemd holds the boot at local-fs.target and
  # the machine needs a keyboard. x-systemd.automount defers the mount
  # to first access, which is also what makes nofail cheap.
  #
  # No `soft`: a soft mount returns EIO to a half-written document
  # instead of blocking, and this share receives PDFs.
  local line="${NFS_SERVER}:${NFS_EXPORT} ${MOUNT_POINT} nfs4 rw,nofail,x-systemd.automount,x-systemd.idle-timeout=600,_netdev 0 0"
  run cp -a "$FSTAB" "${FSTAB}.psb-backup.$(date +%Y%m%d%H%M%S)"
  # tee -a rather than a redirect: `run` executes an argv, and a shell
  # redirect inside it would need an eval, which is how a path with a
  # space in it turns into two fstab fields.
  printf '%s\n' "$line" | run tee -a "$FSTAB" >/dev/null
  run systemctl daemon-reload
  did "added to ${FSTAB} (a timestamped backup sits next to it)"

  if ! run mount "$MOUNT_POINT"; then
    warn "mount ${MOUNT_POINT} failed — check the export, the NAS firewall, and squash settings"
  fi
}

# ---------------------------------------------------------------------
# 3. udev rule
# ---------------------------------------------------------------------
install_udev_rule() {
  log "udev rule for the scanner"

  if [[ -f "$UDEV_RULE" ]]; then
    skip "already present: ${UDEV_RULE}"
    return 0
  fi

  # The scanner group has to exist before the rule references it, or
  # udev silently falls back to root and the container cannot claim the
  # device -- a failure that looks like "the scanner is not detected".
  if ! getent group scanner >/dev/null; then
    run groupadd --system scanner
    did "created group 'scanner'"
  fi

  # Prefer the copy next to this script (git clone), fall back to
  # fetching it (curl'd single file). Both paths are supported by the
  # documented quickstart.
  local local_rule
  local_rule="$(dirname "$(readlink -f "$0")")/../udev/99-paperless-scan-bridge.rules"
  if [[ -f "$local_rule" ]]; then
    run install -m 0644 "$local_rule" "$UDEV_RULE"
  else
    run curl -fsSL "$RULE_URL" -o "$UDEV_RULE"
    run chmod 0644 "$UDEV_RULE"
  fi

  run udevadm control --reload-rules
  run udevadm trigger --subsystem-match=usb
  did "installed ${UDEV_RULE}"
}

# ---------------------------------------------------------------------
# 4. Pull the images
# ---------------------------------------------------------------------
pull_images() {
  log "container images"

  if [[ "$DRY_RUN" -eq 0 ]] && ! command -v docker >/dev/null 2>&1; then
    warn "docker not on PATH — skipping the pull"
    return 0
  fi

  # Deliberately not :latest. ADR 0011 keeps it out of every compose
  # file, and pre-pulling a moving tag here would defeat that the first
  # time someone ran this twice a week apart.
  skip "compose pulls the pinned versions from deploy/compose/scan-bridge.yml"
  skip "run: docker compose -f deploy/compose/scan-bridge.yml pull"
}

main() {
  parse_args "$@"
  require_root
  require_debian_like

  [[ "$DRY_RUN" -eq 1 ]] && log "dry run — nothing will be changed"

  install_docker
  install_nfs_mount
  install_udev_rule
  pull_images

  echo
  if [[ "$CHANGED" -eq 0 ]]; then
    log "nothing to do — this host was already prepared"
  else
    log "done"
  fi
  cat <<'NEXT'

Next:
  cp deploy/compose/.env.example deploy/compose/.env
  $EDITOR deploy/compose/.env
  docker compose -f deploy/compose/scan-bridge.yml up -d
  curl -s http://localhost:18080/ready

If the scanner was plugged in before the udev rule was installed,
unplug and replug it — udevadm trigger does not re-apply permissions to
an already-claimed device on every kernel.
NEXT
}

main "$@"
