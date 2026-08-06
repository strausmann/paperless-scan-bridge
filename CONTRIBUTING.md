# Contributing to paperless-scan-bridge

Thanks for considering a contribution. This project lives from
real-world reports — every confirmed hardware compatibility entry,
every fix for a Pi distribution we did not test, every improvement to
the documentation makes the next person's setup easier.

## Ways to contribute

**Hardware compatibility reports** are the most valuable kind of
contribution. If you got the bridge running with a scanner not listed
in [HARDWARE_COMPATIBILITY.md](HARDWARE_COMPATIBILITY.md), open a PR
adding a row. The template at the top of that file shows what we need
to know — model, USB ID, SANE backend, what works, what does not.

**Bug reports** belong in GitHub issues. Use the bug template under
`.github/ISSUE_TEMPLATE/bug.yml`. Include `scanimage -L` output, your
distribution, your scanner model, and relevant logs from
`docker logs scan-bridge` and the Paperless container.

**Feature requests** belong in GitHub issues too. Use the feature
template. Larger changes should be discussed in an issue before any
code is written, so we can agree on the design first.

**Pull requests** are very welcome for documentation fixes, test
additions, hardware compatibility entries, blueprint improvements, and
bug fixes. For new features, please open an issue first.

**Documentation translations.** The site is English-first with German
as the second language. Zensical has no native multi-language support
yet, so each language is a separate build with its own `docs_dir`:
English lives in `docs/en/` and is served at the domain root, German
lives in `docs/de/` and is served under `/de/`. If you want to add
another language, open an issue to coordinate — it means a third
config file and a third build step. Tracked in issue #13; upstream in
[zensical/backlog#2](https://github.com/zensical/backlog/issues/2) and
[#1](https://github.com/zensical/backlog/issues/1).

## Local development

You need Docker with Compose v2, Go 1.22+, Tilt, and pre-commit
installed. On a Mac or Linux workstation:

```bash
git clone https://github.com/strausmann/paperless-scan-bridge.git
cd paperless-scan-bridge
pip install pre-commit
pre-commit install
go work sync
```

For container-based development with live rebuild:

```bash
tilt up
```

This brings up `scan-bridge`, `sane-runtime`, and `scan-processor` in a
local Compose environment, watches the Go source files, and rebuilds
the affected container in seconds when you save a file.

If you only want to work on documentation:

```bash
python3 -m venv .venv
.venv/bin/pip install -r requirements-docs.txt
./.github/scripts/vendor-mermaid.sh                # once, see below
.venv/bin/zensical serve -f zensical.toml          # English, :8000
.venv/bin/zensical serve -f zensical.de.toml \
  --dev-addr localhost:8001                        # German, :8001
```

Or via the Makefile: `make docs-serve` and `make docs-serve-de`, which
run the vendoring step for you. The site reloads on file save.

**Why the vendoring step.** The published site must not make
third-party requests. Zensical lazy-loads Mermaid from `unpkg.com` when
it meets a diagram, so `vendor-mermaid.sh` downloads a pinned,
digest-verified copy into `docs/en/javascripts/` and
`docs/de/javascripts/` and the site serves it from its own origin. The
file is ~3.5 MB and gitignored — it is a build artifact, not source.
Google Fonts is off for the same reason (`font = false`). CI asserts
both, so a regression fails the build rather than going unnoticed.

To reproduce what CI does, including the strict-mode build of both
languages, run `make test-docs`. Build order matters: the English build
clears `site/`, so it has to run before the German build.

## Test suite

```bash
make test           # runs everything: shellcheck, golangci-lint, bats, hadolint, yamllint
make test-go        # only the Go test suite
make test-shell     # only shell script tests
make test-yaml      # only yamllint
make test-docker    # only hadolint on Dockerfiles
make test-docs      # only markdownlint on the docs
```

For the Ansible layer (under `deploy/ansible/`):

```bash
make test-ansible      # ansible-lint
make test-molecule     # full molecule suite for the optional roles
```

The Molecule tests spin up Docker containers for Ubuntu 22.04, Ubuntu
24.04, and Debian 12 and run the role idempotently. They take roughly
five minutes on a modern laptop.

The integration tests under `tests/integration/` are end-to-end —
they bring up the full compose stack against a mocked SANE scanner and
verify a scan request lands as a PDF in a fake Paperless inbox. These
are the slowest tests and only run in CI by default.

## Code style

**Go:**

- `gofmt` and `goimports` clean
- `golangci-lint run` passing with the project's `.golangci.yml`
- Errors wrapped with `fmt.Errorf("scan dispatch: %w", err)` — context
  before the error
- Context propagated through every I/O function
- Tests use the standard `testing` package; table-driven where it helps
- HTTP handlers separated from business logic; handlers thin

**Shell:**

- `#!/usr/bin/env bash` at the top
- `set -euo pipefail` immediately after the shebang
- `shellcheck` clean at the strict level (`-S style`)
- Functions documented with a leading block comment
- Long lines wrapped at 100 columns

**YAML:**

- `yamllint` clean against the project config
- Two-space indentation
- Sequence items aligned with the key (no extra indent)
- Boolean values as `true`/`false`, never `yes`/`no` or `on`/`off`

**Markdown:**

- 80-column wrap for prose; soft wraps inside paragraphs are fine
- No hard wraps inside code blocks
- ATX-style headings (`#`, `##`)
- One sentence per line is acceptable in long-form docs for
  diff-friendliness

**Dockerfiles:**

- `hadolint` clean
- Multi-stage builds for any image with build-time tools
- Pin base images by digest in production-track images
- Run as non-root user where possible
- Document every `RUN` step's purpose with a comment

Everything is enforced via pre-commit and CI. If pre-commit passes,
your PR will likely pass CI.

## Container-first principle

This is non-negotiable: do not propose changes that require additional
software installations on the Pi host. The Pi runs Docker, an NFS
mount, and the udev rules. That is the whole host surface area.

If you have a feature idea that seems to require host installation,
the right approach is one of:

1. Implement it inside an existing container (most common)
2. Add a new container to the Compose stack
3. Use a sidecar pattern with shared volumes
4. Document why no container alternative exists and discuss in an issue

If a feature truly cannot be containerized — for example, kernel-level
USB handling — that is a discussion for an issue, not a PR.

## Commit messages

Conventional Commits format:

```
type(scope): short summary

Optional longer body explaining the why.

Closes #123
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`,
`perf`, `build`, `style`.

Scopes are repository directories or component names: `scan-bridge`,
`sane-runtime`, `scan-processor`, `compose`, `ansible`, `docs`, `ci`,
`deploy`.

Examples:

- `feat(scan-bridge): add /profiles endpoint with filter support`
- `fix(sane-runtime): handle USB device disconnect during scan`
- `docs(architecture): clarify NFS polling tradeoff`
- `chore(ci): bump golangci-lint to 1.62`

## Pull request workflow

1. Fork the repository, branch from `main`, push your changes
2. Open a PR. The CI runs automatically
3. Address review comments by pushing additional commits — we squash
   on merge, so each commit does not need to be perfect
4. Once approved and CI is green, a maintainer merges

We aim for first-response within seven days. If you do not hear back,
please ping the PR.

For larger PRs (more than ~300 lines of code change), please split
into smaller PRs that can be reviewed independently. The maintainers
appreciate this enormously.

## Adding a hardware compatibility entry

This is the most common contribution and the one most needed. The
process:

1. Edit `HARDWARE_COMPATIBILITY.md` and add a row to the table
2. If your scanner needs SANE configuration that the default
   `sane-runtime` does not include, add the configuration under
   `components/sane-runtime/config/<vendor>-<model>.conf`
3. If your scanner needs a custom udev rule, add it to
   `deploy/udev/99-paperless-scan-bridge.rules`
4. If there are model-specific notes, add a short page to
   `docs/en/hardware/<vendor>-<model>.md`
5. Open the PR with the title `feat(hardware): add <vendor> <model>
   compatibility`

We do not require you to test every feature. Document what you tested,
mark the rest as "untested" in your row.

## Adding a new scan profile

Profiles are part of the runtime configuration, not the source code.
But default profile templates ship with the project:

1. Edit `components/scan-bridge/internal/profiles/defaults.yaml` to
   add the profile entry
2. Update the JSON schema under
   `components/scan-bridge/api/schema/profile.json`
3. Add a test case in
   `components/scan-bridge/internal/profiles/profiles_test.go`
4. Document the new profile in
   `docs/en/getting-started/scan-profiles.md`

## Writing a blog post

The blog drives most of the project's reach. New posts are welcome,
especially in these themes:

- "I got hardware X working — here's how"
- "We ran into problem Y in production — here's the fix"
- "We benchmarked Z — here are the numbers"

Process:

1. Add a Markdown file to `docs/en/blog/posts/<YYYY-MM-DD>-slug.md`
   for the English version
2. Add a parallel file to `docs/de/blog/posts/<YYYY-MM-DD>-slug.md`
   for the German version (or open an issue requesting translation
   help if you only speak one language)
3. Use the front matter template at `docs/.templates/blog-post.md`
4. Image assets go in `docs/static/images/blog/<slug>/`
5. List the post in the matching `blog/index.md` and add it to `nav`
   in `zensical.toml` (and `zensical.de.toml`). Zensical has no blog
   plugin yet, so nothing is picked up automatically
6. Open a PR; the docs workflow builds both language sites

## Security

Please do not file public issues for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for the responsible disclosure process.

## Code of Conduct

This project follows the [Contributor Covenant 2.1](CODE_OF_CONDUCT.md).
Be excellent to each other.

## Maintainer notes

This is a small project with a single maintainer. Response times will
sometimes be slow during work-heavy weeks. If a PR sits idle for more
than two weeks without a response, you are welcome to ping it. If a
security issue sits idle for more than 48 hours, ping me directly per
the procedure in SECURITY.md.

When the project grows beyond what one person can maintain, this
section will be updated with the maintainer team and decision-making
process.
