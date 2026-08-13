#!/usr/bin/env bash
set -euo pipefail

# Vendor ESP Web Tools so the published site loads it from our own
# domain, not from unpkg.com — the docs site makes no third-party
# requests (see docs/en/architecture/no-third-party-requests.md).
#
# Unlike Mermaid (a single self-contained bundle, see vendor-mermaid.sh),
# ESP Web Tools' `dist/web/install-button.js` dynamically `import()`s a
# handful of sibling chunk files relative to its own URL — a shared
# dialog/console UI bundle plus one flasher stub per chip family (only
# the one for the chip actually being flashed loads, at install time).
# All of those imports are relative paths within `dist/web/`, verified
# against the published package contents (`npm pack esp-web-tools`):
# no absolute unpkg/jsdelivr/CDN URL appears anywhere in the bundle
# (checked in the PR that introduced this script). Self-hosting the
# whole `dist/web/` directory therefore keeps every one of those
# dynamic imports same-origin — vendoring only install-button.js would
# leave the chunk imports pointing nowhere.
#
# The npm registry publishes a subresource-integrity-style `dist.integrity`
# hash for every published version; that is what gets verified here,
# against the tarball as a whole (not a hand-computed digest of one
# file, the way vendor-mermaid.sh does it for a single-file bundle).
#
# The vendored directory is ~540 KB and deliberately NOT committed (see
# .gitignore). It is fetched at build time, pinned by version and
# verified by the registry's own published integrity hash, then served
# from the site.

ESP_WEB_TOOLS_VERSION="10.4.0"
ESP_WEB_TOOLS_SHA512="3pwkeFFm5Fj7UQo8SJNYK5RXrtNCpq6X9QoI6bMT4GBZWgrJqjn0YvM9ihG74BtMoSFYXfmDtkehuxe50PTMPQ=="

URL="https://registry.npmjs.org/esp-web-tools/-/esp-web-tools-${ESP_WEB_TOOLS_VERSION}.tgz"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET_DIR="${REPO_ROOT}/docs/en/javascripts/esp-web-tools"

tmp_tgz="$(mktemp)"
tmp_extract="$(mktemp -d)"
trap 'rm -f "$tmp_tgz"; rm -rf "$tmp_extract"' EXIT

echo "Fetching esp-web-tools ${ESP_WEB_TOOLS_VERSION} from ${URL}"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp_tgz" "$URL"

actual="$(openssl dgst -sha512 -binary "$tmp_tgz" | openssl base64 -A)"
if [ "$actual" != "$ESP_WEB_TOOLS_SHA512" ]; then
  echo "ERROR: digest mismatch for esp-web-tools ${ESP_WEB_TOOLS_VERSION}" >&2
  echo "  expected sha512-${ESP_WEB_TOOLS_SHA512}" >&2
  echo "  actual   sha512-${actual}" >&2
  echo "Refusing to vendor an unverified package. If this is a deliberate" >&2
  echo "version bump, look up the new dist.integrity value on" >&2
  echo "https://registry.npmjs.org/esp-web-tools/<new-version> first." >&2
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
echo "Wrote ${count} files to docs/en/javascripts/esp-web-tools/ (${size})"
