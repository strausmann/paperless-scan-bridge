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

No posts yet. The first one is planned for the Phase 1 launch.

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
