#!/usr/bin/env bash
set -euo pipefail

# Write the language-picking landing page at the site root.
#
# Both languages live under a prefix now (/en/ and /de/), so nothing
# renders at "/" itself. This page fills that slot. It is deliberately
# NOT a bare <meta refresh> to /en/: that would strand German speakers on
# an English page with no visible way back, and it would make the German
# site unreachable for anyone who bookmarked the bare domain.
#
# Behaviour, in order:
#   1. If the browser has previously chosen a language, honour it.
#   2. Otherwise follow navigator.languages — German-speaking browsers go
#      to /de/, everyone else to /en/.
#   3. With JavaScript off, the two links below are the whole page, so it
#      still works — this is why the redirect is not a <meta refresh>
#      alone.
#
# No third-party requests, matching the rest of the site (CI asserts it).

target_dir="${1:?usage: write-root-redirect.sh SITE_DIR}"

cat > "${target_dir}/index.html" <<'HTML'
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <title>paperless-scan-bridge</title>
    <link rel="canonical" href="https://scan-bridge.strausmann.de/en/">
    <!-- Not indexed: this page has no content of its own, and letting a
         crawler rank it above the real homepages would bury both. -->
    <meta name="robots" content="noindex,follow">
    <style>
      :root { color-scheme: light dark; }
      body {
        margin: 0; min-height: 100vh;
        display: flex; align-items: center; justify-content: center;
        font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
        text-align: center; padding: 2rem;
      }
      main { max-width: 30rem; }
      h1 { font-size: 1.5rem; font-weight: 600; margin: 0 0 .5rem; }
      p { margin: 0 0 2rem; opacity: .75; }
      .langs { display: flex; gap: 1rem; justify-content: center; flex-wrap: wrap; }
      a {
        display: inline-block; padding: .75rem 1.75rem; border-radius: .25rem;
        text-decoration: none; font-weight: 600;
        border: 1px solid currentColor; color: inherit;
      }
      a:hover, a:focus-visible { background: currentColor; }
      a:hover span, a:focus-visible span { color: Canvas; }
    </style>
  </head>
  <body>
    <main>
      <h1>paperless-scan-bridge</h1>
      <p>Choose a language &middot; Sprache w&auml;hlen</p>
      <div class="langs">
        <a href="/en/"><span>English</span></a>
        <a href="/de/"><span>Deutsch</span></a>
      </div>
    </main>
    <script>
      (function () {
        try {
          var stored = localStorage.getItem("psb-lang");
          if (stored === "de" || stored === "en") {
            location.replace("/" + stored + "/");
            return;
          }
          var langs = navigator.languages || [navigator.language || "en"];
          for (var i = 0; i < langs.length; i++) {
            var tag = String(langs[i]).toLowerCase();
            if (tag === "de" || tag.indexOf("de-") === 0) {
              location.replace("/de/");
              return;
            }
            // Any other resolved language wins here; only keep scanning
            // past entries we cannot classify.
            if (tag) {
              location.replace("/en/");
              return;
            }
          }
          location.replace("/en/");
        } catch (e) {
          // Private mode can throw on localStorage. The links above are
          // the fallback, so failing silently is correct.
        }
      })();
    </script>
  </body>
</html>
HTML

echo "Wrote ${target_dir}/index.html (language picker)"
