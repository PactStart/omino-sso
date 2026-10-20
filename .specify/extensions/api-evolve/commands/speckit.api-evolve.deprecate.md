---
name: speckit.api-evolve.deprecate
description: Open a deprecation entry — required for any breaking change or removal before the gate passes.
category: process
---

# /speckit.api-evolve.deprecate

Record an active deprecation in `deprecations.json`. The gate refuses any `removal` that does not match a published, sunset-respecting entry here.

## Inputs

- `--target <id>` — the diff entry id from `classification.md` (e.g. `users-remove-legacy_id`).
- `--notice "<text>"` — the deprecation notice shown to clients.
- `--sunset <YYYY-MM-DD>` — date after which the surface is removed.
- `--migration <path>` — path to a migration doc explaining the replacement.

## Output

`specs/<branch>/api-evolve/deprecations.json`:

```json
{
  "active": [
    {
      "id": "users-remove-legacy_id",
      "target": "components.schemas.User.properties.legacy_id",
      "notice": "Use `id` instead.",
      "deprecated_at": "2026-05-07",
      "sunset_at": "2026-08-05",
      "migration_doc": "docs/migrations/legacy_id.md",
      "published_in_version": null
    }
  ]
}
```

`published_in_version` is filled by `/speckit.api-evolve.release` once the deprecation ships.

## Policy

- Notice period (`sunset_at - deprecated_at`) must meet `policy.deprecation.required_notice_days` (default 90).
- `migration_doc` must exist on disk if `policy.deprecation.required_migration_doc: true`.

## Exit conditions

- `0` — deprecation recorded.
- `1` — policy violation (notice too short, missing migration doc).
- `2` — runtime error.
