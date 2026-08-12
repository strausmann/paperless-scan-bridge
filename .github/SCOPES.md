# Commit Scopes

paperless-scan-bridge uses [Conventional Commits](https://www.conventionalcommits.org/) with a
**mandatory, singular, lowercase scope**. Scopes align with the `scope:*` issue labels and the
component layout. Versioning is automated **release-only** (semantic-release computes SemVer + tag +
GitHub Release from these commits); the **`CHANGELOG.md` (Keep a Changelog) stays hand-curated**.

## Format
`type(scope): subject` — `type` ∈ `feat|fix|perf|refactor|docs|test|build|ci|chore|style|revert`,
subject imperative, header ≤ 120 chars.

## Release impact
| Type | Release |
|------|---------|
| `feat` | minor |
| `fix`, `perf`, `refactor` | patch |
| `docs`, `test`, `build`, `ci`, `chore`, `style` | none |
| `!` / `BREAKING CHANGE:` | major |

## Scopes
| Scope | Area |
|-------|------|
| `scan-bridge` | the Go daemon (overall) |
| `sane-runtime` | SANE/scanbd/USB container |
| `scan-processor` | PDF assembly / Paperless ingestion |
| `api` | HTTP API (`internal/api`) |
| `profiles` | scan profiles (`internal/profiles`) |
| `tag` | Paperless tag-merge engine (`internal/tag`) |
| `dispatch` | job dispatch / IPC (`internal/dispatch`) |
| `jobs` | job store/lifecycle (`internal/jobs`) |
| `config` | configuration (`internal/config`) |
| `metrics` | metrics/health (`internal/metrics`, `internal/healthcheck`) |
| `deploy` | compose / Tilt / udev / NFS |
| `docker` | Dockerfiles / images |
| `ci` | GitHub Actions / tooling |
| `docs` | documentation, ADRs |
| `deps` | dependency updates |
| `release` | release plumbing |

## Rules
- Scope **mandatory**, singular, lowercase, most-specific. One issue → one branch → one PR.
- New scope? Add it here **and** to `commitlint.config.cjs` in the same PR.
