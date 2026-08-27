# API reference

Interactive documentation for the `scan-bridge` HTTP API, rendered from
[`components/scan-bridge/api/openapi.yaml`](https://github.com/strausmann/paperless-scan-bridge/blob/main/components/scan-bridge/api/openapi.yaml).
That is the same file a code generator or an `openapi-spec-validator`
run would use. It is grounded against the actually implemented
handlers in `internal/api/`, not the aspirational surface once
sketched in `CONTAINER_SUITE.md` §4.4. Three details worth knowing
before you read it, all called out again at the top of the spec
itself:

- `GET /profiles/{name}` returns capitalized field names (`Name`,
  `Resolution`, ...) — the Go struct behind it only carries `yaml`
  tags, so `encoding/json` falls back to the field names verbatim.
  Everywhere else in this API the wire format is `snake_case`.
- `GET /ready` has a real `200`/`503` contract as of Phase 1.2h.
- `GET /jobs`, `GET /jobs/{id}` and `POST /jobs/{id}/cancel` are real
  `501 Not Implemented` stubs — there is no job store yet.

<div id="scalar-mount"></div>

<script src="/en/javascripts/scalar/standalone.js"></script>
<script>
  Scalar.createApiReference('#scalar-mount', {
    url: 'openapi.yaml',
    proxyUrl: '',
    withDefaultFonts: false,
  })
</script>

Rendered by [Scalar](https://github.com/scalar/scalar), self-hosted
like everything else on this site — see [No third-party
requests](../architecture/no-third-party-requests.md) for why
`proxyUrl` and `withDefaultFonts` are set to disable Scalar's two
default calls to `proxy.scalar.com` and `fonts.scalar.com`.
