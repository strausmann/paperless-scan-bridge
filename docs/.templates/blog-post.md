---
title: "POST TITLE"
date: YYYY-MM-DD
authors:
  - strausmann
categories:
  - "one of: hardware | architecture | operations | project"
tags:
  - TAG
description: "One or two sentences. Used for the social preview and search."
---

# POST TITLE

Opening paragraph — the hook. What broke, what surprised you, or what
you set out to do. Two or three sentences.

<!-- more -->

## FIRST SECTION

Body.

## Takeaway

What the reader should remember. Keep it to a few lines.

---

<!--
Checklist before publishing:

- [ ] Parallel file created for the other language
      (docs/en/blog/posts/ and docs/de/blog/posts/)
- [ ] Assets under docs/static/images/blog/SLUG/
- [ ] Listed in docs/LANG/blog/index.md and in nav
      (zensical.toml / zensical.de.toml)
- [ ] Front matter complete: title, date, categories, description
- [ ] Prose wrapped at 80 columns
- [ ] Both site builds pass:
        zensical build -f zensical.toml --strict
        zensical build -f zensical.de.toml --strict

Notes:

* Keep the H1 in the body. The front-matter `title` sets the browser
  and navigation title but does not render a heading, and there is no
  blog plugin to render one either.
* `authors`, `categories` and `tags` are recorded for forward
  compatibility. Zensical has no blog plugin yet, so they are not
  rendered today — see issue #13.
-->
