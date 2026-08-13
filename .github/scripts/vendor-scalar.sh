#!/usr/bin/env bash
set -euo pipefail

# Vendor Scalar's standalone API-reference renderer so the published
# site loads it from our own domain, not from a CDN — the docs site
# makes no third-party requests (see
# docs/en/architecture/no-third-party-requests.md).
#
# @scalar/api-reference's dist/browser/standalone.js is a single
# self-contained bundle: no dynamic imports resolve against an
# absolute CDN URL by default (there is exactly one dynamic
# `import(e)` call, used only for a user-supplied optional plugin
# module — never invoked unless a caller configures one). Verified
# against the published package contents (`npm pack @scalar/api-reference`)
# when this script was introduced. That makes it the same shape as
# Mermaid (vendor-mermaid.sh) rather than ESP Web Tools
# (vendor-esp-web-tools.sh, whose entry point imports sibling chunk
# files and therefore needs its whole dist/web/ directory vendored).
#
# Unlike Mermaid, though, Scalar is only published as an npm package,
# not as a single raw file at a stable CDN URL — there is no
# "download https://unpkg.com/.../standalone.js and hash that exact
# byte stream" step, because the digest npm publishes
# (`dist.integrity` in the registry metadata) covers the whole
# package tarball, not one file inside it. So this script follows
# ESP Web Tools' approach instead: fetch the tarball from the npm
# registry, verify it whole against that published integrity hash,
# then extract only the one file this project actually serves.
#
# Two 3rd-party requests standalone.js makes by default, both
# neutralised in docs/en/api-reference/index.md's
# Scalar.createApiReference() call (verified against the bundle
# source when this script was introduced):
#   - the default theme loads font files from https://fonts.scalar.com
#     -> withDefaultFonts: false
#   - the "Try it" panel's CORS workaround posts through
#     https://proxy.scalar.com -> proxyUrl: ''
#
# The file is ~3.6 MB and deliberately NOT committed (see
# .gitignore). It is fetched at build time, pinned by version and
# verified by digest, then served from the site.

SCALAR_VERSION="1.65.0"
SCALAR_SHA512="5ZkyHlx2rK5kaoj7bdnftE1O2qdakJaYiYiRP1frhiSfuKr2hpCIJ1WMrw/c6qDVYTfeHrnG6SdKV79wqvq95Q=="

URL="https://registry.npmjs.org/@scalar/api-reference/-/api-reference-${SCALAR_VERSION}.tgz"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="${REPO_ROOT}/docs/en/javascripts/scalar/standalone.js"

tmp_tgz="$(mktemp)"
tmp_extract="$(mktemp -d)"
trap 'rm -f "$tmp_tgz"; rm -rf "$tmp_extract"' EXIT

echo "Fetching @scalar/api-reference ${SCALAR_VERSION} from ${URL}"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp_tgz" "$URL"

actual="$(openssl dgst -sha512 -binary "$tmp_tgz" | openssl base64 -A)"
if [ "$actual" != "$SCALAR_SHA512" ]; then
  echo "ERROR: digest mismatch for @scalar/api-reference ${SCALAR_VERSION}" >&2
  echo "  expected sha512-${SCALAR_SHA512}" >&2
  echo "  actual   sha512-${actual}" >&2
  echo "Refusing to vendor an unverified package. If this is a deliberate" >&2
  echo "version bump, look up the new dist.integrity value on" >&2
  echo "https://registry.npmjs.org/@scalar/api-reference/<new-version> first." >&2
  exit 1
fi
echo "Digest verified: sha512-${actual}"

tar -xzf "$tmp_tgz" -C "$tmp_extract"

src="${tmp_extract}/package/dist/browser/standalone.js"
if [ ! -f "$src" ]; then
  echo "ERROR: ${src} not found in the ${SCALAR_VERSION} tarball." >&2
  echo "Package layout changed upstream; update this script's extract path." >&2
  exit 1
fi

mkdir -p "$(dirname "$TARGET")"
cp "$src" "$TARGET"
chmod 0644 "$TARGET"   # tar preserves the source mode; the site has to serve it
echo "Wrote docs/en/javascripts/scalar/standalone.js ($(wc -c <"$TARGET") bytes)"
