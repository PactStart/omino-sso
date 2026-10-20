---
hook: after_tasks
runs: task-injection
---

# after_tasks

Runs after `/speckit.tasks` produces `tasks.md`. Injects API governance tasks based on the planned contract changes.

## What it does

1. Reads `plan.md` and the diff (running `/speckit.api-evolve.diff` if needed).
2. For each planned change kind, appends a task with a dispatch trailer:

| Planned change | Injected task |
|---|---|
| Any contract edit | `[API-1] speckit.api-evolve.diff` |
| Any | `[API-2] speckit.api-evolve.classify` |
| Any breaking/removal | `[API-3] speckit.api-evolve.deprecate <id>` |
| Any | `[API-4] speckit.api-evolve.bump` |
| Any breaking/removal | `[API-5] speckit.api-evolve.client-impact` |

3. Tasks are inserted before the first non-API implementation task so they run early.

## Failure mode

Soft — if injection fails, original `tasks.md` is preserved and a warning is logged.

## When to disable

Set `hooks.after_tasks.enabled: false` to manage API tasks manually.
