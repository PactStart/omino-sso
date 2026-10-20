---
name: speckit.api-evolve.deprecation-status
description: Show every active deprecation, days until sunset, and which endpoints still use it.
category: visibility
---

# /speckit.api-evolve.deprecation-status

Read-only summary of `deprecations.json` annotated with current contract usage.

## Output (stdout)

```
Active deprecations (2):

 ID                         Sunset       Days   Status        Migration
 users-remove-legacy_id     2026-08-05   90     pending       docs/migrations/legacy_id.md
 billing-remove-v1-charge   2026-06-01   25     OVERDUE       docs/migrations/billing_v1.md ⚠

⚠ Past-sunset entries still present in working contract — gate will fail.
```

## Statuses

- `pending` — deprecation declared, not yet published in any release.
- `published` — declared and shipped; sunset clock running.
- `pending-removal` — sunset reached, surface still present.
- `OVERDUE` — sunset passed and surface still present (gate will fail).

## Exit conditions

- `0` — at least one deprecation listed (or none active).
- `1` — at least one OVERDUE entry detected (the gate will block on these).
