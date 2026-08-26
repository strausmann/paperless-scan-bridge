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
      <!-- Each fragment carries its own lang: the document is en, so a
           screen reader would otherwise pronounce "Sprache wählen" and
           "Deutsch" with English phonetics. -->
      <p><span lang="en">Choose a language</span> &middot;
         <span lang="de">Sprache w&auml;hlen</span></p>
      <div class="langs">
        <a href="/en/" data-lang="en" lang="en"><span>English</span></a>
        <a href="/de/" data-lang="de" lang="de"><span>Deutsch</span></a>
      </div>
    </main>
    <script>
      (function () {
        var KEY = "psb-lang";

        function remember(lang) {
          try {
            localStorage.setItem(KEY, lang);
          } catch (e) {
            // Private mode and blocked storage throw here. Remembering
            // is a convenience, not a requirement — the redirect below
            // still happens.
            console.warn("language picker: could not persist choice", e);
          }
        }

        // Clicking a language is an explicit choice and outranks the
        // browser's preference on the next visit. Without this the
        // stored-choice branch below could never fire.
        var links = document.querySelectorAll("a[data-lang]");
        for (var j = 0; j < links.length; j++) {
          links[j].addEventListener("click", function () {
            remember(this.getAttribute("data-lang"));
          });
        }

        var stored = null;
        try {
          stored = localStorage.getItem(KEY);
        } catch (e) {
          console.warn("language picker: could not read stored choice", e);
        }
        if (stored === "de" || stored === "en") {
          location.replace("/" + stored + "/");
          return;
        }

        var langs = navigator.languages || [navigator.language || "en"];
        var target = "en";
        for (var i = 0; i < langs.length; i++) {
          var tag = String(langs[i]).toLowerCase();
          if (!tag) {
            continue;
          }
          target = (tag === "de" || tag.indexOf("de-") === 0) ? "de" : "en";
          break;
        }
        // Deliberately NOT remembered: this was the browser's guess, not
        // the visitor's decision. Persisting it would make the picker
        // unreachable for someone who wanted the other language.
        location.replace("/" + target + "/");
      })();
    </script>
  </body>
</html>
HTML

echo "Wrote ${target_dir}/index.html (language picker)"
