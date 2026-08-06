#!/usr/bin/env bash
set -euo pipefail

# Vendor Mermaid so the published site loads it from our own domain.
#
# Zensical lazy-loads Mermaid from unpkg.com when it encounters a
# `.mermaid` element, which would send every visitor's IP to a
# third-party CDN. Its loader is guarded:
#
#   typeof mermaid == "undefined" || mermaid instanceof Element
#     ? load("https://unpkg.com/mermaid@11/dist/mermaid.min.js")
#     : <nothing>
#
# The upstream `mermaid.min.js` is a single self-contained bundle with
# no dynamic imports that ends in `globalThis["mermaid"] = ...`. Loading
# it ourselves via `extra_javascript` therefore satisfies the guard and
# the CDN fetch never happens. No wrapper module is needed.
#
# The file is ~3.5 MB and deliberately NOT committed (see .gitignore).
# It is fetched at build time, pinned by version and verified by digest,
# then served from the site. Upstream tracking for a build-time renderer
# that would make this unnecessary: zensical/backlog#155.

MERMAID_VERSION="11.16.1"
MERMAID_SHA384="aBQXj4hK6Jm05i7aQAsUV3bLdSUrHX1BGYfMB0166TtWt/RRaw+h0Eelme9OCOvy"

URL="https://unpkg.com/mermaid@${MERMAID_VERSION}/dist/mermaid.min.js"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGETS=("docs/en/javascripts/mermaid.min.js" "docs/de/javascripts/mermaid.min.js")

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Fetching mermaid ${MERMAID_VERSION} from ${URL}"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp" "$URL"

actual="$(openssl dgst -sha384 -binary "$tmp" | openssl base64 -A)"
if [ "$actual" != "$MERMAID_SHA384" ]; then
  echo "ERROR: digest mismatch for mermaid ${MERMAID_VERSION}" >&2
  echo "  expected sha384-${MERMAID_SHA384}" >&2
  echo "  actual   sha384-${actual}" >&2
  echo "Refusing to vendor an unverified bundle." >&2
  exit 1
fi
echo "Digest verified: sha384-${actual}"

for target in "${TARGETS[@]}"; do
  dest="${REPO_ROOT}/${target}"
  mkdir -p "$(dirname "$dest")"
  cp "$tmp" "$dest"
  chmod 0644 "$dest"   # mktemp creates 0600; the site has to serve it
  echo "Wrote ${target} ($(wc -c <"$dest") bytes)"
done
