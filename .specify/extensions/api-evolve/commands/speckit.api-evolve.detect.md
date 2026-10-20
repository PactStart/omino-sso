---
name: speckit.api-evolve.detect
description: Discover every API contract file in the project and classify by format.
category: docs
---

# /speckit.api-evolve.detect

Walk the project tree, identify every contract file, and write `specs/<branch>/api-evolve/detected.json`.

## Detected formats

| Format | Heuristic |
|---|---|
| OpenAPI 3.x | YAML/JSON with top-level `openapi: 3.x` |
| Swagger 2.0 | YAML/JSON with top-level `swagger: "2.0"` |
| GraphQL SDL | `*.graphql`, `*.gql`, or files containing `type Query` / `schema {` |
| gRPC | `*.proto` with a `syntax = "proto3"` (or proto2) declaration |
| JSON Schema | `*.schema.json` / `*.schema.yaml`, or files with `$schema` |

## Output

`specs/<branch>/api-evolve/detected.json`:

```json
{
  "scanned_at": "2026-05-07T00:00:00Z",
  "contracts": [
    {
      "path": "openapi/users.yaml",
      "format": "openapi-3",
      "title": "Users API",
      "version": "1.4.0",
      "endpoints": 12,
      "components": 18
    }
  ]
}
```

## Exit conditions

- `0` — at least one contract found and recorded.
- `1` — no contract files found (project may not expose an API yet).
- `2` — contract file present but unparseable; see stderr.

## Side effects

- Updates `spec.md` with a `## API Surface` section listing detected contracts and current versions (only when invoked by the `after_specify` hook).

## Notes

- Skips `node_modules/`, `vendor/`, `.git/`, `dist/`, `build/`, and any path matching `.gitignore`.
- Detection is content-based, so non-standard file names still work.
