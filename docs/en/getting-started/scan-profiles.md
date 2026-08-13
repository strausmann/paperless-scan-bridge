# Scan profiles

A **profile** is a named bundle of scanner settings plus the metadata
hints that travel with the resulting document. Profiles are what makes
"press one button, get the right outcome" possible: the trigger only
carries a profile name, everything else is configuration.

## Where profiles live

Profiles are declarative YAML in a single file. The daemon reads the
path configured as `paths.profiles`, which defaults to
`/etc/scan-bridge/profiles.yaml`. The shipped defaults live in
`components/scan-bridge/internal/profiles/defaults.yaml`.

!!! warning "Mounting a profile file replaces the defaults"

    There is no merge. The daemon loads exactly one file. If you mount
    your own `profiles.yaml` over `/etc/scan-bridge/profiles.yaml`, the
    shipped defaults are gone — copy the ones you want to keep into your
    file. The daemon refuses to start if the file defines no profiles.

Unknown keys are a hard error: the YAML decoder runs with strict field
checking, so a typo or a field from a future schema version fails the
load rather than being silently ignored.

## Reading profiles at runtime

```bash
# List all profiles
curl -s http://your-pi-host:8080/profiles

# Fetch one profile by name
curl -s http://your-pi-host:8080/profiles/private-duplex
```

Both endpoints are implemented today.

## Schema

This is the base set of fields every profile can set.

```yaml
profiles:
  - name: private-duplex
    description: "Private documents, duplex, color, 300 DPI"
    source: "ADF Duplex"
    resolution: 300
    mode: "Color"
    format: "pdf"
    target_subdir: "private/"
    deskew: true
    remove_blank: true
    rotate_pages: true
    page_size: "A4"
    timeout_seconds: 300
    metadata_template:
      paperless_tags: ["private"]
      paperless_correspondent: null
```

| Field | Values |
| --- | --- |
| `name` | Unique, non-empty. Duplicate names fail the load. |
| `description` | Free text. |
| `source` | SANE source string, e.g. `ADF`, `ADF Duplex`, `Flatbed`. Spelled exactly as the backend reports it. |
| `resolution` | DPI, 100–1200. |
| `mode` | `Color`, `Gray`, `Lineart` |
| `format` | `pdf`, `jpeg`, `tiff` |
| `target_subdir` | Superseded by `destinations` for any profile that adopts it — see [Profile schema reference](profile-schema.md#fields-that-predate-destinations). |
| `deskew` | `true` / `false` |
| `remove_blank` | `true` / `false` |
| `rotate_pages` | `true` / `false` |
| `page_size` | `A4`, `Letter`, `A5`, `auto` |
| `timeout_seconds` | Bounds the whole `POST /scan` call — scan, `scan-processor`, and every destination's upload submission together. |
| `metadata_template.paperless_tags` | List of tag names. Superseded by a `paperless` destination's `config.tag_ids` (integer IDs) — see [Profile schema reference](profile-schema.md#fields-that-predate-destinations). |
| `metadata_template.paperless_correspondent` | Correspondent name, or `null`. Same caveat as above. |

The post-processing flags (`deskew`, `remove_blank`, `rotate_pages`) are
acted on by the `scan-processor` container, over the same `POST /scan`
call — see [Profile schema reference](profile-schema.md) for the full
set of fields, including `ocr`, `assembly`, `document_type`, and
`destinations`.

## Not in the schema yet

- **Tag-merge modes** (`add` / `override` / `remove`) exist, but only
  as a `paperless` destination's `config.tag_strategy` — there is no
  profile-wide tag-merge default outside a destination's own config.
- **Separator-page splitting** and ASN-based splitting.
- **Profile CRUD over the API**, including a JSON Schema mirror of
  `profiles.yaml` itself.

See [Profile schema reference](profile-schema.md#not-in-the-schema-yet)
for the complete, current list.

## Adding a profile

1. Edit `components/scan-bridge/internal/profiles/defaults.yaml`
2. Add a table-driven test case in
   `components/scan-bridge/internal/profiles/profiles_test.go`
3. Document the profile on this page

Step 2 of the repository playbook — updating
`components/scan-bridge/api/schema/profile.json` — does not apply yet:
the file does not exist. Until it does, the Go struct tags in
`internal/profiles/profiles.go` are the reference schema.
