# Deployment compose (registry images)

This directory holds the compose file used for **GitOps deployments** via
Dockhand. It pulls published images from GHCR — it never builds.

For local development use the `compose.yaml` in the repository root
instead; that one builds from source.

## Host-side configuration lives OUTSIDE the checkout

Two files are site-specific or secret and therefore live outside the git
checkout, at a fixed absolute path:

| File | Purpose |
|---|---|
| `/docker/stacks/paperless-scan-bridge-config/profiles/default.yaml` | scan profile, contains the site's Paperless `base_url` |
| `/docker/stacks/paperless-scan-bridge-config/secrets/paperless_api_token` | Paperless API token |

**This is not a style preference — it is required.** Dockhand's git-stack
sync mirrors the stack directory against the repository's tracked file
list on *every* deploy and deletes anything that is not in it. A
gitignored file placed inside the checkout is removed before compose even
starts, and the deploy fails with `bind source path does not exist`.
Restoring the file by hand only lasts until the next deploy.

### First-time setup on a new host

```bash
install -d -m 0755 /docker/stacks/paperless-scan-bridge-config/profiles
install -d -m 0750 /docker/stacks/paperless-scan-bridge-config/secrets

cp deploy/profiles/default.yaml.example \
   /docker/stacks/paperless-scan-bridge-config/profiles/default.yaml
# then edit it and set your own Paperless base_url

install -m 0640 -o root -g root /dev/null \
   /docker/stacks/paperless-scan-bridge-config/secrets/paperless_api_token
# then write the real token into it, without echoing it to a terminal
```

The token file must be `root:root` with mode `0640`, not `0600`:
scan-bridge reads it as the distroless image's `nonroot` user (UID 65532)
and carries supplementary group 0, so root-only permissions make the
upload fail late and quietly. Do not use `0644` — that would make the
token world-readable on the host for no benefit.

`deploy/config/config.toml` is git-tracked and stays in the checkout: it
is a versioned input, not runtime state.

## Image version

`PSB_VERSION` selects the image tag and defaults to `1` — the major-version
tag, so patch and minor releases arrive automatically. Pin it to an exact
version (`PSB_VERSION=1.3.0`) to freeze a deployment.
