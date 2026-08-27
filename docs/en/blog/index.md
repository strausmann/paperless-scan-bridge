# Blog

Notes from building this stack — hardware archaeology, container design
decisions, and the occasional dead end worth documenting.

!!! note "Hand-maintained index"

    Zensical does not ship a blog plugin yet, so there is no automatic
    archive, category, or tag view. Posts are listed here by hand until
    upstream support lands.

    Upstream this is
    [zensical/backlog#30](https://github.com/zensical/backlog/issues/30)
    (blog plugin) and
    [zensical/backlog#38](https://github.com/zensical/backlog/issues/38)
    (tags); on our side
    [issue #13](https://github.com/strausmann/paperless-scan-bridge/issues/13)
    tracks when to drop the workaround.

## Posts

- **2026-08-27** —
  [A sixteen-year-old scanner, a Pi, and the button that does not exist](posts/2026-08-27-a-sixteen-year-old-scanner-and-a-pi.md)
  · *project* · The Phase 1 pipeline scans, assembles and uploads a real
  document. Getting there meant discarding the feature the whole design
  was named after.

## Writing a post

Posts are parallel files, one per language:

```text
docs/en/blog/posts/<YYYY-MM-DD>-<slug>.md
docs/de/blog/posts/<YYYY-MM-DD>-<slug>.md
```

Use the front-matter template at `docs/.templates/blog-post.md`. Images
and other assets go under `docs/static/images/blog/<slug>/`.

After adding a post, list it in the "Posts" section above and add it to
`nav` in `zensical.toml` (and `zensical.de.toml` for the German
version).
