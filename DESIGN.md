# Design

<!-- impeccable:design-record 1 -->

Durable visual-system record for the scan-bridge homepage
(`docs/en/index.md` + `overrides/home.html` + `docs/en/assets/home/`).
Written from the built, verified page — not from intentions written
before the build. Everything below reflects what actually ships.

## Scope

This file documents the **homepage only**. Every other page on
`scan-bridge.strausmann.de` keeps Zensical's default `main.html` theme,
completely unmodified. `custom_dir = "overrides"` in `zensical.toml`
exists solely so the homepage can carry `template: home.html`; nothing
in `overrides/` or `docs/en/assets/home/` is loaded on any other page.

## Anchor and direction

Locked direction (operator decision, after a 7-variant comparison round
covering both a zensical.org-DNA family and a Dockhand-UI-DNA family):
**"Hardware Chain"** — the zensical.org anchor, light-primary with dark
available via the site's own existing palette toggle (not a separate
bespoke switch).

- **World:** zensical.org's real, extracted tokens — indigo primary
  `#4051B5` / accent `#526CFE` (identical in both color schemes, matching
  zensical.org's own actual behavior: only the ground and text invert,
  not the brand color), the coral-to-gold CTA gradient
  (`linear-gradient(35deg, #FF8A8C, #FFC120)`) reserved for exactly one
  primary action per view, tight display letter-spacing, self-hosted
  Inter + JetBrains Mono.
- **Register:** calm and editorial, not a marketing-hero spectacle. The
  hero is a line-diagram of the four physical objects in the system
  (scanner, Pi, NAS, Paperless-ngx), not an animated canvas or a
  dashboard shell — those were the DNA of the other variants in the
  comparison round (Signal Flow, Operations Console) and were
  deliberately not chosen.
- **Mode:** Persuade for the hero (the one screen that has to earn
  attention and action), Read for everything below it (the actual
  content is real prose from README.md/CONCEPT.md/ROADMAP.md, styled but
  not rewritten).

## Color tokens

Defined in `docs/en/assets/home/home.css`, light in bare `:root`
(default/primary), dark under `[data-md-color-scheme="slate"]` — the
exact attribute the site's own theme sets on `<body>` when a visitor
toggles dark mode or has `prefers-color-scheme: dark` with no stored
preference. No separate homepage-only theme switch exists.

| Token | Light | Dark | Used for |
|---|---|---|---|
| `--mdx-bg` | `#ffffff` | `#0b0c0f` | Page background |
| `--mdx-bg-elevated` | `#f6f6f8` | `#15171b` | Cards, code panels, table |
| `--mdx-bg-subtle` | `#ececef` | `#1d2026` | Table header row |
| `--mdx-fg` | `#0b0c0f` | `#ffffff` | Headings, strong text |
| `--mdx-fg-muted` | `rgba(0,0,0,.7)` | `rgba(255,255,255,.74)` | Body copy |
| `--mdx-fg-faint` | `rgba(0,0,0,.46)` | `rgba(255,255,255,.46)` | Card body text |
| `--mdx-hairline` | `rgba(0,0,0,.12)` | `rgba(255,255,255,.11)` | Borders |
| `--mdx-primary` / `--mdx-accent` | `#4051b5` / `#526cfe` | *(same)* | Links, diagram accents |
| `--mdx-headline-accent` | `#8a5600` | `#ffc120` | Admonition title (see WCAG note below — light value is NOT simply a darker version of the dark value picked by eye, it was solved for the actual composited background) |
| `--mdx-ok-green` | `#1b7f49` | `#8fe6b0` | Live/green diagram accents |
| `--mdx-code-fg` (`--code-fg` in the file) | `#3546c4` | `#8fa8ff` | Inline `<code>` |
| `--mdx-ink-rgb` | `0,0,0` | `255,255,255` | Feeds `rgba(var(--mdx-ink-rgb), X)` for every hardcoded-opacity SVG stroke/fill in the hardware-chain diagram, so the diagram inverts correctly without a second copy of the SVG |

## Typography

- **Text:** `InterVariable` (variable font, weights 100–900 in one file),
  self-hosted at `docs/en/assets/home/fonts/InterVariable.woff2`
  (~352 KB). Fed into the theme via `--md-text-font`, which the compiled
  stylesheet already reads wherever Google Fonts would normally have
  been wired in — `font = false` in `zensical.toml` means that path is
  otherwise dormant, so this page is the only thing that ever pays for
  loading a real display font.
- **Mono:** `JetBrains Mono` Regular only (~92 KB), same self-hosting
  mechanism via `--md-code-font`. Used for inline `<code>`, the diagram's
  small labels, and the quickstart code block.
- **License:** both fonts are SIL Open Font License; the license files
  ship alongside the woff2s in the same directory
  (`Inter-LICENSE.txt`, `JetBrainsMono-OFL.txt`).
- No italic weights are shipped — nothing in the built copy uses
  emphasis-via-italic that would need one.

## Components

- **Hero (`overrides/home.html`, `hero` block).** Headline is a `<p>`,
  not an `<h1>` — see "Two accessibility fixes" below for why. SVG
  hardware-chain diagram (four nodes, dashed connecting rail, one
  soft warm glow via `radial-gradient`). Two CTAs reusing the theme's own
  `.md-button` / `.md-button--primary` class *names* for semantic
  consistency, but with their own complete box-model CSS — see the
  button note below.
- **Card grid (`.mdx-grid`).** Applied to a `<div class="mdx-grid"
  markdown="1"><ul>...</ul></div>` wrapper around ordinary Markdown
  bullet lists (`md_in_html`, already enabled in `zensical.toml`). Used
  twice: the three containers + Paperless-ngx, and the three trigger
  paths.
- **Status table (`.mdx-status`).** Same div-wrapping technique, around
  an ordinary Markdown table — the Phase 0–4 roadmap ledger.
- **Admonition.** The theme's own `warning` admonition, restyled to the
  landing palette instead of Material's default red/orange.
- **Quickstart code block.** The theme's normal fenced-code rendering,
  restyled (background, border-radius) to match the rest of the page.
- **Motion.** One authored moment only: the hardware-chain rail draws
  itself in via `stroke-dashoffset` when it first scrolls into view
  (`docs/en/assets/home/home.js`), then settles back to its resting
  dotted pattern. Respects `prefers-reduced-motion`. No other JavaScript
  on the page; everything else is static HTML/CSS.

## Three real bugs this build caught (and fixed) — not cosmetic notes

Kept here because they are the kind of thing a future edit to this page
could silently reintroduce:

1. **`.md-button` has almost no styling outside `.md-typeset`.** Verified
   against the actual compiled stylesheet
   (`site/assets/stylesheets/modern/main.*.css`): `.md-button{display:
   inline}` is the *only* rule that applies unconditionally; every real
   button property (background, radius, padding, font-weight) is scoped
   `.md-typeset .md-button`. The hero buttons sit outside `.md-typeset`
   (they're in the `hero` block, a sibling of the content area), so they
   need their **complete** box model supplied in `home.css` rather than
   relying on inherited theme styling.
2. **The theme auto-injects `<h1 id="__skip">`** in `partials/content.html`
   whenever the Markdown has no H1 — using it as both the page's one real
   heading *and* the skip-navigation link's target. Because `index.md`
   deliberately starts at `##` (its H1 would have duplicated the hero's
   own visual headline), this auto h1 exists in the DOM with the SEO
   title text and is visually hidden with a standard clip-based
   visually-hidden rule — never `display:none`, which would break the
   skip link's focus target.
3. **`[data-md-color-scheme="slate"] body:has(...)` never matches
   anything.** `data-md-color-scheme` is set *on* `<body>` itself
   (`<body data-md-color-scheme="default" ...>`), not on an ancestor of
   it — a selector prefixing it as a descendant combinator is
   syntactically valid but can never match. Two dark-mode overrides (the
   `<a>` link color and the inline `<code>` color inside `.md-typeset`)
   silently fell back to their light-mode values in dark mode until the
   WCAG audit below caught it. Fixed to `body[data-md-color-scheme=
   "slate"]:has(.mdx-hero) ...` — the attribute selector applies to
   `body` directly.
4. **`{: .class }` (attr_list trailing-line syntax) does not reliably
   attach to a `<ul>` or a `<table>`** in this markdown pipeline. Verified
   against the actual built HTML: for a list, it attaches to the *last
   list item* instead of the list (turning one `<li>` into an accidental
   grid container, visibly breaking its layout); for a table, it gets
   parsed as a literal extra table row (`{: .mdx-status }` rendered as
   real cell content). The reliable mechanism, used throughout
   `index.md`, is `md_in_html`: wrap the whole list or table in
   `<div class="…" markdown="1"> … </div>`.
5. **The Jinja `| url` filter does not do the Markdown link resolver's
   `.md` → directory-URL rewrite.** `'getting-started/index.md' | url`
   in a template produces a link to a source file that is never present
   in the built site (only `getting-started/index.html` is) — a silent
   broken link that only shows up by checking the actual generated
   `href` values, not by looking at the rendered page. Fixed to the
   directory-style path (`'getting-started/' | url`), matching every
   other correctly-resolved link on the page.

## WCAG contrast — verified against the real built page, both themes

Not computed from the token table by hand: a small Playwright script
navigated the actual built site with the browser's `color-scheme`
emulated to `light` and `dark`, read the real computed `color` and
alpha-composited `background-color` (walking the full ancestor chain,
not just the first non-transparent layer) for every distinct text
role on the page, and checked each against its correct WCAG threshold
(3:1 for large text, 4.5:1 otherwise). Ran clean in both themes after
three real fixes it surfaced:

- Light-mode admonition title/body was 3.34:1 (`#b87400` on the
  composited ~`rgb(247,240,222)` warning-box background) — corrected to
  `#8a5600`, now 5.41:1.
- Dark-mode inline `<code>` and link colors were silently using their
  light-mode values (root cause: bug 3 above) — 2.41:1 before the
  selector fix, 7.42:1 after.
- The primary gradient button can't be read by `backgroundColor` at all
  (it's a `background-image: linear-gradient(...)`, not a solid color);
  verified by hand instead, against both gradient stops: `#1a0e05` text
  on `#ff8a8c` = 8.36:1, on `#ffc120` = 11.63:1. Both comfortably clear
  4.5:1.

## Responsive

Verified at 1440×900 (desktop) and 390×844 (mobile), both color
schemes, full page (not just first viewport). The card grids collapse
to a single column, the hardware-chain SVG scales down via its own
viewBox, the header's nav links hide below the theme's own existing
breakpoint (unchanged theme behavior, not something this page touches).

## Known open items (not fixed in this build — flagged, not silently skipped)

- **Pre-existing, unrelated:** `zensical build --strict` currently
  aborts on 3 broken-anchor warnings in `getting-started/profile-schema.md`
  pointing into `components/scan-processor/README.md` — a file that
  exists but whose anchors Zensical can't validate because it lives
  outside `docs_dir`. Present on this branch before any homepage work
  started; not touched here (out of scope for a landing-page change,
  and not homepage-related). Worth a follow-up issue: as of this
  writing, `main` likely fails the same `--strict` CI step for the
  same reason.
- **German site (`zensical.de.toml`) does not get the custom homepage.**
  Only the English `docs/en/index.md` was given `template: home.html`;
  the German build still uses the default template. No German landing
  copy was requested or written — extending this to `docs/de/index.md`
  is a follow-up, not a partial job done here.
- **`overrides/home.html`'s hero content is hardcoded HTML**, not driven
  by page front matter. This is intentional (the hardware-chain SVG and
  the specific two CTAs are this page's whole identity, not reusable
  boilerplate for other pages), but it does mean a future copy change to
  the headline/lede requires editing the template, not just the
  Markdown front matter.
