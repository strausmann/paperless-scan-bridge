# Tilt configuration — the container-first development loop.
#
# CONTRIBUTING.md has documented `tilt up` since Phase 0; this is the
# file that makes it run. It drives the repository-root compose.yaml,
# which builds all three images from source, rather than
# deploy/compose/scan-bridge.yml, which pulls pinned releases from GHCR.
# Those are different jobs: one is for changing the code, the other for
# running it.
#
# The point of Tilt here rather than `docker compose watch`: a Go change
# in scan-bridge should not rebuild sane-runtime, and a failing build
# should not take the other two services down with it. Tilt tracks the
# dependency per service.
#
#   tilt up          # start, with the web UI on :10350
#   tilt up -- --no-scanner   # skip sane-runtime (no USB scanner here)
#   tilt down        # stop and remove
#
# No SANE, no Go toolchain and no Python is required on the host — the
# builds happen in containers, same as everywhere else in this project.

config.define_bool('no-scanner')
cfg = config.parse()

# The scan path needs real hardware. On a laptop without the scanner
# attached, sane-runtime starts and every scan fails at the USB layer,
# which is noise rather than signal while working on the bridge or the
# processor. --no-scanner leaves it out entirely.
NO_SCANNER = cfg.get('no-scanner', False)

docker_compose('./compose.yaml')

# ---------------------------------------------------------------------
# Builds
# ---------------------------------------------------------------------
#
# only= is what keeps the three independent. Without it Tilt watches the
# whole repository for every image and a docs change rebuilds all three
# Go services — which on an arm64 cross-build is minutes, not seconds.

docker_build(
    'paperless-scan-bridge-scan-bridge',
    context='components/scan-bridge',
    dockerfile='components/scan-bridge/Dockerfile',
    only=['./'],
)

docker_build(
    'paperless-scan-bridge-scan-processor',
    context='components/scan-processor',
    dockerfile='components/scan-processor/Dockerfile',
    only=['./'],
)

if not NO_SCANNER:
    docker_build(
        'paperless-scan-bridge-sane-runtime',
        context='components/sane-runtime',
        dockerfile='components/sane-runtime/Dockerfile',
        only=['./'],
    )

# ---------------------------------------------------------------------
# Resources
# ---------------------------------------------------------------------

dc_resource('scan-bridge', labels=['services'])
dc_resource('scan-processor', labels=['services'])
if not NO_SCANNER:
    dc_resource('sane-runtime', labels=['services'])
else:
    dc_resource('sane-runtime', labels=['services'], auto_init=False, trigger_mode=TRIGGER_MODE_MANUAL)

dc_resource('init-permissions', labels=['setup'])

# ---------------------------------------------------------------------
# Manual triggers
# ---------------------------------------------------------------------
#
# Buttons in the Tilt UI, not auto-run: `go test ./...` across three
# modules takes long enough that running it on every keystroke turns the
# UI into a wall of red while you are mid-edit. `make test-go` is the
# same command CI runs.

local_resource(
    'go-test',
    cmd='make test-go',
    deps=['components'],
    labels=['checks'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)

local_resource(
    'go-lint',
    cmd='make lint-go',
    deps=['components', '.golangci.yml'],
    labels=['checks'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)

# The docs site is a separate toolchain (Python + Zensical) and a
# separate build. Kept here so `tilt up` is one entry point, but manual:
# most sessions touch either the Go code or the docs, not both.
local_resource(
    'docs',
    serve_cmd='make docs-serve',
    labels=['docs'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
    links=[link('http://localhost:8000', 'English site')],
)

print("""
paperless-scan-bridge — Tilt

  scan-bridge      http://localhost:18080/ready
  UI               http://localhost:10350

  go-test, go-lint and docs are manual triggers in the UI.
  Without a scanner attached: tilt up -- --no-scanner
""")
