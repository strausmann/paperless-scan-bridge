# Scan profiles

A **profile** is a named bundle of scanner settings plus the routing
metadata that tells the pipeline what to do with the result. Profiles
are what makes "press one button, get the right outcome" possible: the
trigger only carries a profile name, everything else is configuration.

## Where profiles live

Profiles are declarative YAML. The shipped defaults are in
`components/scan-bridge/internal/profiles/defaults.yaml`; user profiles
are mounted into the container and merged over the defaults.

## Reading profiles at runtime

```bash
# List all profiles
curl -s http://your-pi-host:8080/profiles

# Fetch one profile by name
curl -s http://your-pi-host:8080/profiles/default
```

Both endpoints are implemented today.

## What a profile controls

| Group | Examples |
| --- | --- |
| Scanner settings | source (flatbed / ADF simplex / ADF duplex), resolution, mode (color / grayscale / lineart), page size |
| Post-processing | deskew, blank-page removal, cropping, PDF assembly |
| Splitting | one PDF per stack, or split on separator pages |
| Routing | Paperless correspondent, document type, tags, storage path |

## Tag merge semantics

When a profile and the trigger both specify tags, the profile declares
how they combine:

- `add` — union of profile tags and request tags
- `override` — request tags replace profile tags entirely
- `remove` — request tags are subtracted from the profile tags

## Adding a profile

The repository playbook for adding a profile is:

1. Edit `components/scan-bridge/internal/profiles/defaults.yaml`
2. Update the JSON schema at
   `components/scan-bridge/api/schema/profile.json`
3. Add a table-driven test in
   `components/scan-bridge/internal/profiles/profiles_test.go`
4. Document the profile on this page

!!! note "Schema file not written yet"

    Step 2 refers to `components/scan-bridge/api/schema/profile.json`,
    which does not exist yet. The loader and its tests do exist. The
    schema lands with the profile CRUD work in Phase 1.2.

## Secrets in profiles

Profiles reference secrets **by name**, never by value:

```yaml
paperless:
  api_token_secret: paperless_api_token
```

The daemon resolves the name through, in order: a Docker secret file
under `/run/secrets/<name>`, an uppercase environment variable, then a
SOPS-encrypted YAML file. A cleartext token in a profile file is
rejected at parse time.
