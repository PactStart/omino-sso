---
name: speckit.api-evolve.bump
description: Recommend the SemVer bump implied by the classified diff and refuse silent semver violations.
category: process
---

# /speckit.api-evolve.bump

Compute the SemVer bump implied by `classification.md` and write `bump.json`.

## Bump rules

| Highest class observed | Bump |
|---|---|
| `removal` or `breaking` | `major` |
| `non-breaking` (only) | `minor` (if the contract version is `>= 1.0.0`) — `patch` (if `0.x.y`) |
| `additive` (only) | `minor` |
| no diff | none |

For contracts under `0.x.y`, the policy `versioning.allow_unstable_breaking` controls whether a breaking change is allowed on a `minor` bump (defaults to `true`).

## Output

`specs/<branch>/api-evolve/bump.json`:

```json
{
  "computed_at": "2026-05-07T00:00:00Z",
  "contracts": [
    {
      "file": "openapi/users.yaml",
      "current_version": "1.4.0",
      "implied_bump": "major",
      "next_version": "2.0.0",
      "confirmed": false
    }
  ]
}
```

`confirmed: true` is only set after a human confirms via `/speckit.api-evolve.bump --confirm <file>`. The gate refuses to pass on a `major` bump until `confirmed: true`.

## Calver fallback

When `versioning.scheme: calver` is set in policy, the bump is reported as the next CalVer slot for the current date (e.g. `2026.05.0`) and `confirmed` is auto-set after the gate's other rules pass.

## Exit conditions

- `0` — `bump.json` written.
- `1` — version field missing from the contract (`info.version` for OpenAPI, etc.).
- `2` — runtime error.
