#!/usr/bin/env python3
"""Fail if the built site loads any asset from a foreign origin.

The project rule "no cloud dependencies" covers the documentation site:
a visitor must not have their IP handed to a third party. Zensical pulls
Mermaid from unpkg.com and fonts from fonts.googleapis.com by default.
Both are neutralised — a vendored Mermaid bundle and `font = false` —
and this script is the guard that keeps them that way.

Only asset-loading markup counts: <script src>, <img src>, <iframe src>,
and <link> with a rel that actually fetches something. Plain <a href>
links and <link rel="canonical"/"alternate"> are metadata or navigation,
not loads, and are ignored.

Known gap: with `repo_url` set, the theme fetches api.github.com for
repository stats from JavaScript. That is invisible to any markup check.
See docs/en/architecture/no-third-party-requests.md.

Usage: check_no_external_assets.py [SITE_DIR] [--allow HOST ...]
"""

from __future__ import annotations

import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlparse

# <link rel=...> values that cause a fetch. Anything else (canonical,
# alternate, prev, next, ...) is metadata.
FETCHING_REL = {
    "stylesheet",
    "preload",
    "modulepreload",
    "preconnect",
    "dns-prefetch",
    "prefetch",
    "icon",
    "shortcut icon",
    "apple-touch-icon",
    "manifest",
}


class AssetCollector(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.assets: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        a = {k: (v or "") for k, v in attrs}
        if tag in ("script", "img", "iframe", "audio", "video", "source"):
            url = a.get("src", "")
        elif tag == "link":
            rels = {r.strip().lower() for r in a.get("rel", "").split()}
            if not rels & FETCHING_REL:
                return
            url = a.get("href", "")
        else:
            return
        if url.startswith(("http://", "https://", "//")):
            self.assets.append(url)


def main(argv: list[str]) -> int:
    args = [a for a in argv[1:] if not a.startswith("--")]
    allowed = set()
    it = iter(argv[1:])
    for arg in it:
        if arg == "--allow":
            allowed.add(next(it, "").lower())

    site_dir = Path(args[0] if args else "site")
    if not site_dir.is_dir():
        print(f"ERROR: {site_dir} does not exist - build the site first.", file=sys.stderr)
        return 1

    # The site's own canonical host is not a third party.
    allowed.add("strausmann.github.io")

    offenders: dict[str, set[str]] = {}
    pages = 0
    for html in sorted(site_dir.rglob("*.html")):
        pages += 1
        parser = AssetCollector()
        parser.feed(html.read_text(encoding="utf-8", errors="ignore"))
        for url in parser.assets:
            host = (urlparse(url if "//" in url[:8] else "https:" + url).hostname or "").lower()
            if host and host not in allowed:
                offenders.setdefault(url, set()).add(str(html.relative_to(site_dir)))

    if offenders:
        print("ERROR: the built site loads assets from external origins:", file=sys.stderr)
        for url in sorted(offenders):
            where = sorted(offenders[url])
            shown = ", ".join(where[:3]) + (" ..." if len(where) > 3 else "")
            print(f"  {url}\n      in {shown}", file=sys.stderr)
        print("", file=sys.stderr)
        print("Self-host them. See docs/en/architecture/no-third-party-requests.md.",
              file=sys.stderr)
        return 1

    # The vendored Mermaid bundle must be in the output, or Zensical
    # falls back to the unpkg CDN the first time a page gains a diagram.
    # That URL lives inside the theme bundle, not in the markup, so the
    # check above cannot see it.
    missing = [
        d for d in (site_dir, site_dir / "de")
        if not (d / "javascripts" / "mermaid.min.js").is_file()
    ]
    if missing:
        for d in missing:
            print(f"ERROR: {d}/javascripts/mermaid.min.js missing.", file=sys.stderr)
        print("Run .github/scripts/vendor-mermaid.sh before building.", file=sys.stderr)
        return 1

    print(f"OK: {pages} pages, no external assets, vendored Mermaid present.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
