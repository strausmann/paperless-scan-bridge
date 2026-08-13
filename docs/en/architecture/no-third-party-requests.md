# No third-party requests

The project rule *"no cloud dependencies for core functionality"* applies
to the documentation site too. A visitor reading these pages should not
have their IP address handed to anyone we did not ask.

That is not the default. Zensical, like Material for MkDocs before it,
reaches out to two external services out of the box.

## Mermaid, from unpkg.com

Zensical renders Mermaid diagrams client-side and loads the renderer
lazily the first time it meets one. Its loader looks like this:

```js
typeof mermaid == "undefined" || mermaid instanceof Element
  ? load("https://unpkg.com/mermaid@11/dist/mermaid.min.js")
  : /* already there, do nothing */
```

The guard is the opening. The upstream `mermaid.min.js` is a single
self-contained bundle with no dynamic imports whose last statement is:

```js
globalThis["mermaid"] = globalThis.__esbuild_esm_mermaid_nm["mermaid"].default;
```

So if we load that same file ourselves before Zensical needs it, the
global is already defined, the guard short-circuits, and the CDN request
never happens. No wrapper module, no patched theme.

`.github/scripts/vendor-mermaid.sh` downloads a pinned version, verifies
it against a stored SHA-384 digest, and writes it into
`docs/en/javascripts/` and `docs/de/javascripts/`. Both configs then
reference it:

```toml
extra_javascript = ["javascripts/mermaid.min.js"]
```

The file is about 3.5 MB. It is **not** committed — it is a reproducible
build artifact, fetched during the build and served from our own origin.
Skipping the vendoring step would silently fall back to the CDN, so CI
asserts the file is present in both outputs.

Upstream, [zensical/backlog#155](https://github.com/zensical/backlog/issues/155)
proposes rendering diagrams at build time. That would remove the 3.5 MB
client-side bundle altogether and make this whole section obsolete.

## ESP Web Tools, from unpkg.com

The [CYD scan-control panel](../hardware/cyd-scan-panel.md) installer
page embeds [ESP Web Tools](https://esphome.github.io/esp-web-tools/), a
web component that flashes ESP32 firmware over Web Serial straight from
the browser. Every published usage example loads it from unpkg.com:

```html
<script type="module" src="https://unpkg.com/esp-web-tools@10/dist/web/install-button.js?module"></script>
```

Unlike Mermaid, this isn't a single self-contained bundle — the entry
point dynamically `import()`s a shared dialog/console chunk and one
flasher stub per chip family, all as paths relative to its own URL
(verified against the published package contents: no absolute CDN URL
appears anywhere in the bundle). Loading only `install-button.js`
ourselves would still leave those relative imports resolving against
*our* origin but pointing at files that don't exist there — the whole
`dist/web/` directory has to be vendored together, or the dynamic
imports 404.

`.github/scripts/vendor-esp-web-tools.sh` downloads a pinned npm
package, verifies the tarball against the SHA-512 integrity hash the
registry itself publishes for that version, and writes every file under
`dist/web/` into `docs/en/javascripts/esp-web-tools/`. The installer
page references it directly:

```html
<script type="module" src="/javascripts/esp-web-tools/install-button.js"></script>
```

Same trade-off as Mermaid: about 540 KB, not committed (a reproducible
build artifact, fetched during the build and served from our own
origin), and CI's external-asset check would catch a regression here
too — a root-relative `src` is same-origin by construction, so there is
nothing to allowlist.

## Google Fonts

The default theme loads Inter and JetBrains Mono from
`fonts.googleapis.com`, with a `preconnect` to `fonts.gstatic.com`.
Disabled:

```toml
[project.theme]
font = false
```

The site uses system fonts instead, and CI fails the build if a font
link reappears.

## The one that is still there

Setting `repo_url` makes the theme fetch
`https://api.github.com/repos/<owner>/<repo>` and `.../releases/latest`
to show the version and star count in the header. The result is cached
in `sessionStorage`, so it is one or two requests per visitor session
rather than one per page — measured with a fresh browser profile, it
fires on the first English page and again on the first German one. It
is still a third-party request, and Zensical exposes no switch for it:
the fetch is triggered purely by `repo_url` matching a GitHub URL.

Turning it off means dropping `repo_url`, which also removes the
repository link in the header and the "edit this page" button. That
trade-off has not been made yet.

## Verifying

Build the site, serve it locally, and watch the network log with an
empty cache and cleared storage. The only external requests should be
none:

```bash
make test-docs
python3 -m http.server 8765 --directory site
```

Then load `/`, `/architecture/` and `/de/` and check that nothing
outside `127.0.0.1` is requested — except the GitHub API call described
above.

`make test-docs` runs `.github/scripts/check_no_external_assets.py`,
which parses every generated page and fails on any `<script src>`,
`<img src>`, `<iframe src>` or fetching `<link>` pointing at a foreign
origin. It cannot see JavaScript-initiated requests, which is exactly
why the GitHub call needs the note above.
