#!/usr/bin/env bash
set -euo pipefail

# Vendor the Improv Wi-Fi BLE SDK into docs/en/javascripts/improv-wifi/.
#
# The /manage/ page uses <improv-wifi-launch-button> to provision the
# panel's Wi-Fi over Bluetooth. Upstream tells you to load it from
# https://www.improv-wifi.com/sdk-js/launch-button.js — this project
# does not: the docs site makes no third-party requests
# (docs/en/architecture/no-third-party-requests.md), and CI asserts it.
#
# Same shape as vendor-esp-web-tools.sh: launch-button.js lazily imports
# a couple of sibling chunks relative to its own URL, so the whole
# dist/web directory has to be copied, not just the entry point, or the
# button renders and then fails the moment someone clicks it.
#
# The vendored directory is fetched at build time, pinned by version and
# verified against the npm registry's published integrity hash, and is
# deliberately NOT committed (see .gitignore).

IMPROV_VERSION="1.4.1"
IMPROV_SHA512="SjL0uAC4W0XTcFwDpvNc1h5m9kqf7z3jtBdj9UMsJZ1SsOfeMOUokXC8Y4C2QS7pJ/DZeUBzjVNuQ0QxJCCK1Q=="

URL="https://registry.npmjs.org/improv-wifi-sdk/-/improv-wifi-sdk-${IMPROV_VERSION}.tgz"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET_DIR="${REPO_ROOT}/docs/en/javascripts/improv-wifi"

tmp_tgz="$(mktemp)"
tmp_extract="$(mktemp -d)"
trap 'rm -f "$tmp_tgz"; rm -rf "$tmp_extract"' EXIT

echo "Fetching improv-wifi-sdk ${IMPROV_VERSION} from ${URL}"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp_tgz" "$URL"

actual="$(openssl dgst -sha512 -binary "$tmp_tgz" | openssl base64 -A)"
if [ "$actual" != "$IMPROV_SHA512" ]; then
  echo "ERROR: digest mismatch for improv-wifi-sdk ${IMPROV_VERSION}" >&2
  echo "  expected sha512-${IMPROV_SHA512}" >&2
  echo "  actual   sha512-${actual}" >&2
  echo "Refusing to vendor an unverified package. If this is a deliberate" >&2
  echo "version bump, look up the new dist.integrity value on" >&2
  echo "https://registry.npmjs.org/improv-wifi-sdk/<new-version> first." >&2
  exit 1
fi
echo "Digest verified: sha512-${actual}"

tar -xzf "$tmp_tgz" -C "$tmp_extract"

rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"
cp "$tmp_extract"/package/dist/web/*.js "$TARGET_DIR"/
chmod 0644 "$TARGET_DIR"/*.js   # the site has to serve them

count=$(find "$TARGET_DIR" -name '*.js' | wc -l)
size=$(du -sh "$TARGET_DIR" | cut -f1)
echo "Wrote ${count} files to docs/en/javascripts/improv-wifi/ (${size})"
