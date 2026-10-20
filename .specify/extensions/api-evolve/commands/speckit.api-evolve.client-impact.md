---
name: speckit.api-evolve.client-impact
description: Map breaking changes to known consumers so impact is visible before the change ships.
category: visibility
---

# /speckit.api-evolve.client-impact

Cross-reference `classification.md` against the known consumer list in `policy.yml` and report which client is affected by which change.

## Inputs

- `classification.md`
- `policy.yml` → `clients` section:
  ```yaml
  clients:
    - name: web-app
      endpoints: ["/api/v1/users", "/api/v1/auth/*"]
    - name: mobile-android
      endpoints: ["/api/v1/feed", "/api/v1/auth/*"]
  ```

## Output

`specs/<branch>/api-evolve/client-impact.md`:

```markdown
# Client Impact — feature/user-display-name

## web-app
- ⚠ removal: User.legacy_id (used via /api/v1/users)
  Action required: stop reading `legacy_id`; use `id`.

## mobile-android
- (no impact)

## (unmapped)
- additive: User.display_name — not yet consumed by any registered client.
```

## Exit conditions

- `0` — report generated.
- `1` — at least one breaking/removal change has no matching consumer (information missing in policy).
- `2` — runtime error.

## Notes

- Consumers can be discovered automatically when the project ships an OpenAPI/GraphQL client registry; otherwise the maintainer keeps the list in `policy.yml`.
- Endpoint patterns support `*` glob and exact match.
