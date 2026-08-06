# Gemini Code Assist — Style Guide (paperless-scan-bridge)

When reviewing this repo, these are the sources of truth (in precedence order):

1. **ADRs** in `docs/decisions/` — highest authority.
2. `AGENTS.md` / `CLAUDE.md` — **container-first, host-thin, exactly three custom images**
   (`scan-bridge` / `sane-runtime` / `scan-processor`), Synology = source of truth, no `latest`
   tags, no host-level installs.
3. `ARCHITECTURE.md`, `CONTAINER_SUITE.md`, `THREAT_MODEL.md`, `SECURITY.md`.
4. `CONTRIBUTING.md` and `.github/SCOPES.md` (Conventional Commits + scopes).

Notes for findings:
- Report concretely and **by severity**.
- Stack: **Go** (`scan-bridge`), SANE/`avision` runtime, Docker/compose, Tilt; auth = bearer token
  (SHA-256 `TokenHash`) or ip_allowlist.
- `sane-runtime` legitimately needs `--device=/dev/bus/usb` (+ udev) — that is **not** a finding;
  flag `--privileged` or host installs instead.
- No secrets/tokens hard-coded or logged; pin versions (no `latest`).
