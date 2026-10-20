---
hook: after_plan
runs: speckit.api-evolve.baseline
---

# after_plan

Runs after `/speckit.plan` finalizes `plan.md`. Captures the contract baseline before any plan-driven edits land.

## What it does

1. Invokes `/speckit.api-evolve.baseline`.
2. Skips silently if a baseline already exists for this feature branch.

## Failure mode

Hard — exits non-zero if baseline cannot be written (disk full, permission). The hook is the only safe place to anchor the baseline; later phases assume it exists.

## When to disable

Set `hooks.after_plan.enabled: false` in `policy.yml` only when the project tracks contracts outside the repo (e.g. a separate registry).
