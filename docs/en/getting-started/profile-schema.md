# Profile schema reference

This page is the complete field-by-field reference for `profiles.yaml`,
including the OCR, multi-page-assembly, document-type, and destination
fields the
[design doc](https://github.com/strausmann/paperless-scan-bridge/blob/main/docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)
added on top of the base schema [Scan profiles](scan-profiles.md)
introduces. Read that page first for *where* profiles live and how
they're loaded — this page only documents *what a profile can say*.

The Go struct tags in
[`internal/profiles/profiles.go`](https://github.com/strausmann/paperless-scan-bridge/blob/main/components/scan-bridge/internal/profiles/profiles.go)
are the actual reference; this page tracks that file, not the other
way around.

## Full example

```yaml
profiles:
  - name: private-duplex
    description: "Private documents, duplex, color, 300 DPI"
    source: "ADF Duplex"
    resolution: 300
    mode: Color
    format: pdf
    target_subdir: private/          # unused once a profile has explicit
                                      # destinations (kept for a future
                                      # NFS/SMB destination — see "Fields
                                      # that predate destinations" below)
    deskew: true
    remove_blank: true
    rotate_pages: false
    page_size: A4
    timeout_seconds: 120

    # OCR (off by default, matching the "Paperless does this better on
    # the bigger Docker host" default). Enabling without `languages`
    # defaults to deu+eng.
    ocr:
      enabled: true
      languages: [deu, eng]

    # Multi-page result shape scan-processor applies. Default: combined.
    assembly:
      page_grouping: combined        # combined | per_page

    # Free-form, profile-defined key — no central enum. Absent means no
    # type-specific mapping is applied at any destination.
    document_type: eingangsrechnung

    # Destination routing (one or more targets). "paperless" is the
    # only target implemented today.
    destinations:
      - target: paperless
        storage_first: false         # direct-to-API is the only mode built
        config:
          base_url: "https://paperless.example.com"
          token_secret: paperless_api_token   # config.SecretResolver name
          tag_ids: [3]                        # INTEGER Paperless tag IDs
          tag_strategy: add                   # add | override | remove
          correspondent_id: 12
          document_type_id: 3                 # fallback when document_type
                                               # has no map entry
          document_type_map:
            eingangsrechnung:
              document_type_id: 3
              tag_ids: [7]
            post:
              tag_ids: [4]
```

## Base fields

These exist since the first schema version and are documented in full
on [Scan profiles](scan-profiles.md#schema); listed here again for a
single point of reference.

| Field | Values |
| --- | --- |
| `name` | Unique, non-empty. Duplicate names fail the load. |
| `description` | Free text. |
| `source` | SANE source string, e.g. `ADF`, `ADF Duplex`, `Flatbed`. |
| `resolution` | DPI, 100–1200. |
| `mode` | `Color`, `Gray`, `Lineart` |
| `format` | `pdf`, `jpeg`, `tiff` — this is also `scan-processor`'s `output_format` (no separate field). |
| `target_subdir` | See "Fields that predate destinations" below. |
| `deskew` | `true` / `false` — passed to `scan-processor`. |
| `remove_blank` | `true` / `false` — passed to `scan-processor`. |
| `rotate_pages` | `true` / `false` — passed to `scan-processor`. |
| `page_size` | `A4`, `Letter`, `A5`, `auto` |
| `timeout_seconds` | Bounds the **entire** `POST /scan` call today — scan + `scan-processor` + every destination's upload *submission* — not just the scan itself. Must be positive. |

## `ocr`

| Field | Values |
| --- | --- |
| `ocr.enabled` | `true` / `false`. Default `false` — matching this project's long-standing "Paperless does this better on the bigger Docker host" default; this schema field makes it a per-profile override instead of a fixed global choice. |
| `ocr.languages` | List of Tesseract language codes, e.g. `[deu, eng]`, or exactly `[auto]` to request auto language detection (see "Auto language detection" below). If `enabled: true` and `languages` is omitted, it defaults to `[deu, eng]`. Ignored (but harmless) when `enabled: false`. Each entry must be non-empty; `auto` must be the only entry when used. |
| `ocr.min_confidence` | `0..100`. The mean OCR confidence (Tesseract's own scale) below which a document is flagged `low_confidence` in the `POST /scan` response (see "OCR confidence gate" below). Omitted or `0` defaults to `80`. Ignored (but harmless) when `enabled: false`. |

### Available languages

`ocr.languages` is picked **per profile**, not globally — each profile
names only the language(s) its own documents are actually in. The
`scan-processor` component's runtime image installs, and its
`/process` endpoint therefore accepts, these Tesseract language codes
(the [`scan-processor` README](../../../components/scan-processor/README.md#ocr-languages)
is the authoritative, single-source list, and documents how to add
another one):

| Code  | Language   |
| ----- | ---------- |
| `deu` | German     |
| `eng` | English    |
| `fra` | French     |
| `ita` | Italian    |
| `nld` | Dutch      |
| `por` | Portuguese |
| `spa` | Spanish    |

A code outside this set is rejected by `scan-processor` with `400
invalid_request` before OCR ever runs. Naming every installed language
on one profile is possible but not recommended: Tesseract recognises
more slowly with more language packs loaded, and languages sharing a
script/dictionary overlap (e.g. `deu`/`nld`, `spa`/`ita`) can produce
more misrecognitions from words being auto-corrected into the wrong
language's spelling than naming just the language(s) that profile
actually scans.

When OCR is enabled and `format: pdf`, the output is a **searchable
PDF** — Tesseract's own PDF output mode embeds an invisible text layer
over the original page image; there is no separate "assemble then OCR"
step. For `jpeg`/`tiff`, OCR still runs (a page that defeats OCR
entirely is still a processing failure) but the text layer is
discarded, since neither format can carry one.

### OCR confidence gate

Every OCR pass also records Tesseract's own per-word confidence score
(`0..100`); `scan-processor` averages it across a document's page(s)
and, when the mean falls below `ocr.min_confidence` (default `80`),
flags that document `low_confidence: true` in the `POST /scan`
response (`documents[].ocr_confidence` and
`documents[].low_confidence`) with a matching entry in `warnings`.
**This never fails the scan** — a flagged document is still assembled
and delivered normally; the flag is advisory, meant to route a
document Tesseract itself was not confident about to manual review.
See the [`scan-processor` README](../../../components/scan-processor/README.md#ocr-confidence-gate)
for the mechanism.

### Auto language detection

Setting `ocr.languages: [auto]` runs a **pragmatic, two-pass**
language-detection flow instead of naming a fixed language set: OCR
once with the `deu+eng` default, guess the page's language from that
pass's recognized text via a small stopword-based heuristic, and —
only if the guess differs from `deu+eng` — OCR once more with the
detected language, keeping whichever pass scored the higher
confidence. It never asks Tesseract for every installed language at
once (which was evaluated and found to reduce quality), and the
confidence gate above catches the cases the heuristic still gets
wrong. It is **not** a real language-identification model — short
text, closely related languages (Spanish/Italian/Portuguese in
particular), and heavily garbled OCR output can all be misidentified.
For a profile whose documents are reliably in one non-default
language, naming that language explicitly remains more accurate and
cheaper. Full details, including the documented accuracy limits: the
[`scan-processor` README](../../../components/scan-processor/README.md#auto-language-detection).

## `assembly`

| Field | Values |
| --- | --- |
| `assembly.page_grouping` | `combined` (default) or `per_page`. |

`combined` merges every page scan-processor produced for the job into
one document. `per_page` emits one document per surviving source page.
This is a per-profile choice read directly from this field — it is
never inferred from page count or content. A `combined` request with
`format: jpeg` and more than one surviving page is rejected by
`scan-processor` (`400 unsupported_format`) — JPEG cannot hold
multiple pages in one file.

## `document_type`

A free-form string, e.g. `eingangsrechnung`, `kontoauszug`,
`versicherung`. There is no central enum and nothing enforces that the
same concept is spelled identically across profiles — that's a
convention concern, not something the schema validates. Empty (the
default) means no destination applies a type-specific mapping for
documents from this profile.

`document_type` only has an effect through a destination's own
`document_type_map` (see below) — `scan-processor` never sees this
field at all.

## `destinations`

A list of delivery targets. Each entry:

| Field | Values |
| --- | --- |
| `destinations[].target` | Registered destination name. `paperless` is the only one implemented today — `nfs`, `smb`, `httppost`, and `fileee` are reserved names with no built module yet. An unknown/unbuilt `target` fails at profile-load time with an error listing the currently known names. |
| `destinations[].storage_first` | `true` / `false`. `false` (direct-to-API) is the only mode any built destination implements; `true` (write-then-let-the-destination-pick-it-up, e.g. an NFS consume directory) is reserved for a future storage-first module. |
| `destinations[].config` | A target-specific block. Only the named destination module interprets it — `scan-bridge`'s core does not validate its shape beyond requiring `target` to be non-empty. |

A profile with an empty (or absent) `destinations` list is valid — it
simply has nothing configured to deliver assembled documents to yet.

### The `paperless` target's `config` block

| Field | Values |
| --- | --- |
| `config.base_url` | Required. Must be a non-empty, absolute URL (e.g. `https://paperless.example.com`, no trailing slash needed — it's trimmed). Validated at profile-load time. |
| `config.token_secret` | Optional. The [`config.SecretResolver`](https://github.com/strausmann/paperless-scan-bridge/blob/main/components/scan-bridge/internal/config/secrets.go) name the Paperless API token is resolved under — Docker secret file first, then the uppercased environment variable. Defaults to `paperless_api_token` (Docker secret file `paperless_api_token`, env fallback `PAPERLESS_API_TOKEN`) when omitted. |
| `config.tag_ids` | List of **integer** Paperless tag IDs — never names. This destination's default tags for documents from this profile. |
| `config.tag_strategy` | `add` (default), `override`, or `remove` — how `tag_ids` combines with any tag IDs the caller passes in the `POST /scan` request body (`tag_ids` + `tag_strategy` fields), via the same merge algebra `internal/tag.Merge` implements. If the caller sends no tags at all, the result is exactly `tag_ids`, regardless of strategy. |
| `config.correspondent_id` | Optional integer Paperless correspondent ID. |
| `config.document_type_id` | Optional integer Paperless document-type ID — the fallback used when `document_type` (the profile field above) is empty, or is set but has no matching entry in `document_type_map`. |
| `config.document_type_map` | Optional map from the profile's `document_type` value to this destination's own `document_type_id`/`tag_ids` override (see below). |

`document_type_map` entries:

| Field | Values |
| --- | --- |
| `document_type_map.<key>.document_type_id` | Optional integer. When present, **overrides** `config.document_type_id` for documents whose profile `document_type` equals `<key>`. |
| `document_type_map.<key>.tag_ids` | Optional list of integer tag IDs. When present, these are **added to** `config.tag_ids` (not a replacement) before the caller-tag merge runs — see the flagged gap below. |

!!! warning "`document_type_map` tag-merge direction is an implementation choice"

    The design doc's own sketch for this mapping only lists
    `DocumentTypeID`/`TagIDs` fields, without stating whether a match's
    `tag_ids` should *replace* or *add to* the destination's base
    `config.tag_ids`. The shipped implementation
    ([`internal/api/scan_metadata.go`](https://github.com/strausmann/paperless-scan-bridge/blob/main/components/scan-bridge/internal/api/scan_metadata.go))
    treats a match as **additive**: effective tags going into the
    caller-tag merge are `config.tag_ids` + the matched entry's
    `tag_ids`. This matches the schema example above (a profile-level
    `post` tag plus a per-type tag both ending up on the document), but
    it has not had explicit operator sign-off. If you depend on
    "replace" semantics instead, say so before relying on this.

What `paperless` does **not** support yet, even though the field
exists on `Metadata`: `Title`, `Created`, `Labels`, `ASN`, and `Extra`
are all resolvable from `Document`/`Metadata` internally, but no
profile field or destination-config key sets `Title`/`Labels`/`ASN`
today — only `TagIDs`, `Correspondent`, and `DocumentType` are wired
from `resolveMetadata`.

### Response shape

For each assembled document, `POST /scan`'s response reports one
result per destination it was routed to:

```json
{
  "scan_id": "…",
  "documents": [
    {
      "filename": "2026-08-13T14-32-01_receipt.pdf",
      "page_count": 3,
      "destinations": [
        { "name": "paperless", "status": "submitted", "task_id": "…" }
      ]
    }
  ]
}
```

`status: "submitted"` means Paperless-ngx **accepted the upload for
its own asynchronous consumption task** — not that consumption
finished. `task_id` is Paperless's own `post_document/` task
identifier; poll `GET /api/tasks/?task_id=<id>` against Paperless
itself if you need to know when (or whether) it actually finished. A
destination that failed reports `"status": "failed"` with an `error`
field instead of `task_id` — one destination failing never stops
delivery to a document's other destinations, or to the job's other
documents.

## Fields that predate destinations

Two fields exist from before the destination-routing schema and are
**not removed**, but a profile using `destinations` doesn't need
either of them:

- **`target_subdir`** was the hint for where a not-yet-built consume-
  directory-writing pipeline would file the result. No built
  destination reads it today; it's kept because a future storage-first
  NFS/SMB destination is exactly the kind of module that would.
- **`metadata_template.paperless_tags` / `metadata_template.paperless_correspondent`**
  were the original Paperless-only hint shape (tag **names**, not
  IDs). They are superseded by a `paperless` destination's
  `config.tag_ids`/`config.correspondent_id` (integer IDs — see
  [ADR 0016](https://github.com/strausmann/paperless-scan-bridge/blob/main/docs/decisions/0016-destination-routing-pluggable-interface.md)
  for why IDs, not names) for any profile that adopts `destinations`.
  No profile in production depends on removing them, so the fields
  still exist and still validate, but new profiles should be authored
  against `destinations` directly rather than `metadata_template`.

## Not in the schema yet

- **Separator-page splitting** and **ASN-based splitting** of a single
  scan job into multiple documents by content, rather than by
  `assembly.page_grouping`'s fixed combined/per-page choice.
- **Profile CRUD over the API.** `GET /profiles` and
  `GET /profiles/{name}` are implemented; there is no `POST`/`PUT` to
  create or edit a profile without redeploying `profiles.yaml`.
- **A JSON Schema mirror.** `components/scan-bridge/api/openapi.yaml`
  documents the HTTP surface, but there is no
  `api/schema/profile.json` — the Go struct tags in
  `internal/profiles/profiles.go` remain the only machine-readable
  reference for the YAML shape itself.
- **Paperless upload-completion polling.** The `paperless` destination
  submits and returns immediately; nothing in `scan-bridge` ever calls
  `GET /api/tasks/` to confirm consumption succeeded. This is a
  deliberate v1 scope decision
  ([design doc §7, Option C](https://github.com/strausmann/paperless-scan-bridge/blob/main/docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md)),
  not an oversight — it's the natural next increment if it turns out
  to matter.
