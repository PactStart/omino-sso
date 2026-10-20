---
name: speckit.api-evolve.classify
description: Classify each diff entry as additive, non-breaking, breaking, or removal — with rationale.
category: docs
---

# /speckit.api-evolve.classify

Read `diff.json` and produce `classification.md` — the authoritative compatibility judgement that the gate consults.

## Classification rules

| Class | Meaning | Examples |
|---|---|---|
| `additive` | New surface, no client should notice. | Adding an optional field, adding a new endpoint, adding an enum value to a response. |
| `non-breaking` | Existing clients keep working without code changes. | Loosening a `required` field to optional in a request, broadening a numeric range. |
| `breaking` | Some existing clients will fail without code changes. | Adding a `required` request field, narrowing a type, changing an error response code. |
| `removal` | Surface is gone. Always blocked unless previously deprecated. | Removing a field, endpoint, enum value, or service method. |

## Output

`specs/<branch>/api-evolve/classification.md`:

```markdown
# API Classification — feature/user-display-name

| ID | File | Change | Class | Rationale |
|----|------|--------|-------|-----------|
| users-add-display_name | openapi/users.yaml | added-field User.display_name | additive | Optional field; existing clients ignore it. |
| users-remove-legacy_id | openapi/users.yaml | removed-field User.legacy_id | removal | Field deletion. Requires a prior published deprecation. |

## Summary
- additive: 1
- non-breaking: 0
- breaking: 0
- removal: 1
- implied bump: major
```

## Inputs that influence classification

- `api-evolve/policy.yml` (per-feature overrides — e.g. allow narrowing types in 0.x.y)
- The contract format's own compatibility rules (gRPC reserved fields, GraphQL nullability, OpenAPI required arrays).

## Exit conditions

- `0` — classification written.
- `1` — at least one diff entry could not be classified (ambiguous); flagged in the report.
- `2` — runtime error.
