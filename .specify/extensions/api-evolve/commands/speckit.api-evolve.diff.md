---
name: speckit.api-evolve.diff
description: Diff working contracts against the frozen baseline; emit a structured change list.
category: docs
---

# /speckit.api-evolve.diff

Compare every contract file against its `baseline/` counterpart and produce a structured diff.

## Inputs

- `baseline/` populated by `/speckit.api-evolve.baseline`.
- Working contract files (current state in the repo).

## Output

`specs/<branch>/api-evolve/diff.json`:

```json
{
  "compared_at": "2026-05-07T00:00:00Z",
  "entries": [
    {
      "id": "users-add-display_name",
      "file": "openapi/users.yaml",
      "format": "openapi-3",
      "kind": "added-field",
      "location": "components.schemas.User.properties.display_name",
      "before": null,
      "after": { "type": "string" },
      "preliminary_class": "additive"
    },
    {
      "id": "users-remove-legacy_id",
      "file": "openapi/users.yaml",
      "kind": "removed-field",
      "location": "components.schemas.User.properties.legacy_id",
      "before": { "type": "string" },
      "after": null,
      "preliminary_class": "removal"
    }
  ]
}
```

## Diff kinds

- `added-endpoint`, `removed-endpoint`, `renamed-endpoint`
- `added-field`, `removed-field`, `renamed-field`, `changed-type`, `changed-required`
- `added-enum-value`, `removed-enum-value`
- `changed-status-code`, `changed-content-type`, `changed-error-response`
- `changed-auth`, `changed-rate-limit`

## Exit conditions

- `0` — diff written (may be empty if nothing changed).
- `2` — runtime error (malformed contract, missing baseline).

## Notes

- `preliminary_class` is a hint; `/speckit.api-evolve.classify` produces the authoritative classification.
- Diff is computed at the **semantic** level for OpenAPI/GraphQL/gRPC, not as a textual diff — reordering fields is not flagged.
