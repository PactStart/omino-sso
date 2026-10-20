---
name: speckit.api-evolve.gate
description: Run the policy gate that blocks /speckit.implement on undeclared breaking changes or unmet deprecation policy.
category: process
---

# /speckit.api-evolve.gate

Evaluate every gate rule and report the first failure (or PASS).

## Rules

| # | Rule | Failing condition |
|---|------|-------------------|
| 1 | Declared breaking | A `breaking` or `removal` entry has no matching `deprecations.json` row. |
| 2 | Bump confirmed | Implied bump is `major` and `bump.json.confirmed` is false. |
| 3 | Sunset honored | A deprecation with `status: published` and past `sunset_at` is still present in the working contract — required, but only as a removal, not as a live surface. |
| 4 | Removal preceded by publish | A `removal` exists but the matching deprecation has `published_in_version: null`. |
| 5 | Migration doc present | Policy requires `migration_doc` and the file is missing. |

Per-feature `policy.yml` may relax any rule (e.g. `gate.allow_undeclared_additive: true`).

## Output

Updates `orchestration.md`:

```markdown
## Gate — 2026-05-07T00:00:00Z

Result: **BLOCKED**

Failing rule: #4 — Removal preceded by publish
Detail: `users-remove-legacy_id` requires a deprecation that has been published in a prior release.
Suggested fix: ship the deprecation first, then re-run the gate.
```

## Exit conditions

- `0` — gate passed.
- `1` — gate failed; details in `orchestration.md` and stderr.

## Hook integration

`before_implement` runs this command and aborts the dispatch with exit `1` if it fails. The user sees the failing rule directly in their terminal.
