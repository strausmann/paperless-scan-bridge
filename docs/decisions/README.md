# Architecture Decision Records (ADRs)

Binding decisions for paperless-scan-bridge live here, one file per decision, in the
[Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions)
(Context → Decision → Consequences, plus "Options considered").

`AGENTS.md` and `CONTRIBUTING.md` reference ADRs; this is their home. Authority and process:
see `.claude/rules/adr.md`. **Precedence on conflict: ADR > guidelines/`AGENTS.md` > code/README.**

## Rules
- **One file per decision**, `NNNN-short-slug.md` (zero-padded, sequential).
- A file **stays once accepted** — to change a decision, write a **new** ADR that supersedes it and
  set the old one's status to `Superseded by NNNN`.
- Copy [`_template.md`](_template.md) for new ADRs.

## Status values
`Proposed` · `Accepted` · `Deprecated` · `Superseded by NNNN`

## Index
| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-changelog-emoji-sections.md) | Emoji section headers in changelog & release notes | Proposed |
| [0002](0002-container-first-host-thin.md) | Container-first, host-thin | Proposed |
| [0003](0003-three-custom-images.md) | Exactly three custom images | Proposed |
| [0004](0004-synology-source-of-truth.md) | Synology NAS = single source of truth for documents | Superseded by 0016 |
| [0005](0005-trigger-agnostic-scan-endpoint.md) | Trigger-agnostic `POST /scan` is canonical | Accepted |
| [0006](0006-auth-model.md) | Auth: bearer token (SHA-256) or IP allowlist | Accepted |
| [0007](0007-go-for-scan-bridge.md) | scan-bridge in Go (static, distroless) | Proposed |
| [0008](0008-sane-runtime-owns-scanner.md) | sane-runtime owns the scanner; no `--privileged` | Proposed |
| [0009](0009-bridge-sane-unix-socket.md) | bridge ↔ sane-runtime over a Unix socket | Accepted |
| [0010](0010-profiles-declarative-yaml.md) | Scan profiles as declarative YAML | Accepted |
| [0011](0011-no-latest-pinned-versions.md) | No `latest`; pinned versions + Renovate | Proposed |
| [0012](0012-release-only-semantic-release.md) | Release-only semantic-release; manual changelog | Proposed |
| [0013](0013-container-hardening-baseline.md) | Container hardening baseline | Proposed |
| [0014](0014-governance-hierarchy.md) | Governance hierarchy: ADR > AGENTS > code | Proposed |
| [0015](0015-per-profile-token-optional-authz.md) | Per-profile token = optional authz over canonical `/scan` | Accepted |
| [0016](0016-destination-routing-pluggable-interface.md) | Destination routing via a pluggable `Destination` interface + registry | Accepted |
| [0017](0017-document-assembly-and-type-taxonomy.md) | Document assembly and document-type taxonomy are per-profile, destination-interpreted | Proposed |
| [0018](0018-profile-storage-ordering-frequency.md) | Profile display metadata, bridge-side ordering, and usage-frequency tracking | Proposed |
| [0019](0019-scanner-power-control-pluggable-interface.md) | Scanner power control via a pluggable `PowerControl` interface + registry | Proposed |
| [0020](0020-mqtt-home-assistant-integration.md) | `scan-bridge` is the Home Assistant/MQTT-facing component, via MQTT discovery | Proposed |

<!-- Backfill candidates still pending clarification (95% rule): ESP32 panel (#9) · profile storage
     YAML-vs-DB · scanbd hardware-button path (ARCHITECTURE vs #7) · secrets SOPS-vs-env · storage
     topology default. -->

<!-- 2026-08-13: ADR 0016 revisited ADR 0004's scope (Synology as a mandatory single sink vs. one
     destination among several) — the operator confirmed Synology archival becomes purely
     per-profile, so ADR 0004 is now Superseded by 0016 (see both ADRs and the 2026-08-13 vision
     doc). ADR 0018 fulfills ADR 0010's explicitly deferred profile-storage follow-up. -->

<!-- Candidate ADRs to backfill from the existing concept docs:
  0001 container-first / host-thin
  0002 three custom images (scan-bridge / sane-runtime / scan-processor)
  0003 Synology as single source of truth for documents
  0004 trigger-source-agnostic HTTP + bearer auth
  0005 Conventional Commits + release-only semantic-release (manual Keep-a-Changelog kept)
-->
