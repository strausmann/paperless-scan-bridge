# Security Policy

## Reporting a Vulnerability

If you believe you have found a security vulnerability in
`paperless-scan-bridge`, please report it through coordinated disclosure.

**Please do not report security vulnerabilities through public GitHub
issues, discussions, or pull requests.**

Instead, please use one of the following channels:

- **GitHub Security Advisories** (preferred): Open a private advisory
  via the [Security tab](https://github.com/strausmann/paperless-scan-bridge/security/advisories/new)
  of the repository. This creates a confidential, structured channel
  with the maintainer.
- **Email**: Send your report to `security@strausmann.de`. PGP
  encryption is supported; request the public key via the same
  address if needed.

Please include the following information in your report:

- Type of issue (e.g. command injection, authentication bypass,
  insufficient input validation, supply chain risk)
- Affected component (`scan-bridge`, `sane-runtime`, `scan-processor`,
  bootstrap script, documentation site, or repository configuration)
- Affected version or commit SHA
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code, if available
- Impact assessment from your perspective
- Any potential mitigations you have already identified

Reports in German or English are equally welcome.

## What to Expect

When you report a vulnerability, you will receive:

- An acknowledgement within **48 hours**, confirming we have received
  the report
- An initial assessment within **7 days**, with our understanding of
  the issue and an indication of severity
- Regular progress updates, at least every **14 days**, until the
  issue is resolved or determined to be out of scope
- A coordinated disclosure timeline that we agree on together
- Public credit in the security advisory and CHANGELOG, unless you
  prefer to remain anonymous

If we determine the report is not a security issue (for example, a
behavior that is documented as intended), we will explain our
reasoning and you are free to publish your findings.

## Disclosure Timeline

Our default disclosure timeline:

- **Day 0**: Report received and acknowledged
- **Day 0–14**: Investigation and reproduction
- **Day 14–60**: Patch development and testing
- **Day 60–90**: Coordinated release with public advisory
- **Day 90+**: Full disclosure if the issue cannot be resolved earlier,
  unless extension is mutually agreed

We aim to ship a patch within 30 days for high-severity issues and
within 60 days for medium-severity issues. Critical issues that allow
immediate compromise are handled with same-week patches when possible.

## Severity Classification

We use the [CVSS 3.1](https://www.first.org/cvss/v3.1/specification-document)
framework for severity scoring:

| CVSS Score    | Severity   | Initial response | Patch target |
| ------------- | ---------- | ---------------- | ------------ |
| 9.0 – 10.0    | Critical   | 24 hours         | 7 days       |
| 7.0 – 8.9     | High       | 48 hours         | 30 days      |
| 4.0 – 6.9     | Medium     | 7 days           | 60 days      |
| 0.1 – 3.9     | Low        | 14 days          | Next release |

## Scope

The following are **in scope** for security reports:

- The three custom container images (`scan-bridge`, `sane-runtime`,
  `scan-processor`) and their Dockerfiles
- The Go source code under `components/`
- The bootstrap script under `deploy/bootstrap/`
- The udev rules under `deploy/udev/`
- The Compose stacks under `deploy/compose/`
- The Ansible roles under `deploy/ansible/`
- The Home Assistant blueprints under `homeassistant/`
- The n8n workflow exports under `n8n/`
- The release pipeline (GitHub Actions workflows)
- The container signing and SBOM process
- The documentation site build pipeline

The following are **out of scope**:

- Vulnerabilities in upstream dependencies (Paperless-ngx,
  scanservjs, watchtower, SANE, scanbd, Docker, the Linux kernel,
  Synology DSM). Please report these to the upstream projects.
  We will, however, accept reports about how this project's
  configuration of those dependencies introduces additional risk.
- Issues that require a malicious or already-compromised host. The
  threat model in [THREAT_MODEL.md](THREAT_MODEL.md) defines our
  trust boundaries.
- Denial-of-service via crafted scan jobs that consume legitimate
  resources (e.g. very large multi-page batches). These are
  documented operational considerations, not vulnerabilities.
- Issues in third-party hardware (scanners, NAS firmware, Zigbee
  devices). Please report to the hardware vendor.
- Social engineering attacks on maintainers or contributors.

## Out of Scope by Design

Some configurations are deliberately permissive in this project. We
will not treat the following as vulnerabilities:

- Webhook endpoints accepting unauthenticated calls when configured
  for the `ip_allowlist` auth mode. This is a documented opt-in
  behavior for trusted-LAN scenarios.
- Container images requiring USB device access (`sane-runtime`).
  This is necessary for the project's core function.
- Secrets stored encrypted in the Git repository via SOPS. This is
  the documented secrets management approach.
- The bootstrap script requiring sudo. Installing Docker and writing
  to `/etc/udev/rules.d/` requires elevated privileges by design.

## Hall of Thanks

Researchers who responsibly disclose vulnerabilities are recognized
in our security advisories and in the project CHANGELOG, unless they
request anonymity. We do not currently offer monetary bounties.

If you would like to be listed, please indicate your preferred name
and (optionally) a link to your profile in your initial report.

## Coordinated Disclosure With Upstream

Some vulnerabilities affect both this project and an upstream
dependency (for example, a SANE backend bug that we expose). In
those cases, we will:

1. Confirm the issue affects this project independently
2. Coordinate with the upstream project on a joint disclosure
   timeline if appropriate
3. Document the relationship in our advisory
4. Credit you in both advisories

We will not unilaterally disclose vulnerabilities in upstream
projects without giving them a reasonable opportunity to respond.

## Security Updates

Security advisories are published through:

- GitHub Security Advisories on this repository
- Release notes for the affected component versions
- The CHANGELOG entries marked with the **Security** label
- Optional notification via GitHub Releases RSS feed

Users running production deployments are encouraged to:

- Subscribe to release notifications on GitHub
- Watch the repository with "Custom > Releases and security
  advisories" enabled
- Review the CHANGELOG before each upgrade

## Cryptographic Verification

All released container images are signed using
[cosign keyless signing](https://docs.sigstore.dev/cosign/signing/overview/)
via GitHub OIDC. To verify an image:

```bash
cosign verify ghcr.io/strausmann/paperless-scan-bridge/scan-bridge:vX.Y.Z \
    --certificate-identity-regexp '^https://github\.com/strausmann/paperless-scan-bridge/.*$' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

A successful verification proves the image was built by our official
GitHub Actions workflow from the claimed commit. SLSA provenance
attestations are also published for each release.

The bootstrap script's SHA-256 hash is published in each release
notes. We recommend verifying before execution:

```bash
curl -sSL https://raw.githubusercontent.com/strausmann/paperless-scan-bridge/vX.Y.Z/deploy/bootstrap/install.sh \
    | sha256sum
# Compare with the value in the release notes
```

## Maintainer Security Practices

For transparency about how the project's own security posture is
maintained:

- Two-factor authentication is required on all maintainer GitHub
  accounts
- Repository signing key is hardware-backed (YubiKey or equivalent)
- Branch protection requires pull request reviews and status checks
  on `main`
- Renovate is configured to open pull requests for dependency updates,
  which a maintainer reviews before merging
- `govulncheck` runs in CI on every pull request
- Trivy container image scanning runs on every release build
- The release process requires a signed git tag

## Questions

For non-security questions about the project, please use
[GitHub Discussions](https://github.com/strausmann/paperless-scan-bridge/discussions)
or [GitHub Issues](https://github.com/strausmann/paperless-scan-bridge/issues).

For security-specific questions that are not vulnerability reports,
the same channels apply but please prefix the title with `[security
question]` so the maintainer can prioritize accordingly.
