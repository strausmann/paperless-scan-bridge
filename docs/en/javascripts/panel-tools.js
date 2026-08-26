/*
 * Loader for the two Web Component bundles the panel pages use:
 * ESP Web Tools (/install/) and the Improv Wi-Fi BLE SDK (/manage/).
 *
 * Why this file exists at all, rather than a <script type="module"> in
 * the Markdown:
 *
 *   1. `navigation.instant` is enabled. Internal links are fetched by
 *      XHR and the new content is injected, so the browser never
 *      re-parses the document — an inline <script> in the injected page
 *      DOES NOT RUN. Opening /install/ directly worked; clicking
 *      through to it from anywhere else left the custom element
 *      unupgraded, i.e. the install button rendered as plain text and
 *      did nothing. That is the exact path a visitor takes.
 *
 *   2. `extra_javascript` entries survive instant navigation, but
 *      Zensical emits them as plain <script src=...> with no
 *      type="module". Both bundles are ES modules, so loading them that
 *      way is a syntax error. Hence this classic script, which reaches
 *      the modules through dynamic import() instead.
 *
 * Loading is lazy and idempotent: a bundle is imported only once, and
 * only on a page that actually contains its element, so the other ~20
 * pages pay nothing. The heavy per-chip payloads inside ESP Web Tools
 * are themselves lazily imported by the bundle when someone clicks.
 *
 * Paths are absolute on purpose. The German site is served under /de/
 * on the same origin and has no vendored copy of its own; /javascripts/
 * resolves to the English site's bundles either way. Renaming those
 * directories breaks both languages at once — see zensical.de.toml.
 */
(function () {
  "use strict";

  var BUNDLES = [
    ["esp-web-install-button", "/javascripts/esp-web-tools/install-button.js"],
    ["improv-wifi-launch-button", "/javascripts/improv-wifi/launch-button.js"]
  ];

  // Guards against a second import() of the same module. customElements
  // .define() throws on a duplicate name, and instant navigation calls
  // us again on every page change.
  var requested = Object.create(null);

  function load() {
    BUNDLES.forEach(function (entry) {
      var tag = entry[0];
      var src = entry[1];
      if (requested[src] || !document.querySelector(tag)) {
        return;
      }
      requested[src] = true;
      import(src).catch(function (err) {
        // Surfacing this matters: a silent failure here looks exactly
        // like "the button does nothing", which is hard to tell apart
        // from an unsupported browser.
        console.error("panel-tools: failed to load " + src, err);
      });
    });
  }

  // Material/Zensical publishes a document observable that fires on the
  // initial load and again after every instant-navigation swap. When it
  // is missing (theme change, script order), fall back to the ordinary
  // lifecycle so the page still works on a direct load.
  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(load);
  } else if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", load);
  } else {
    load();
  }
})();
