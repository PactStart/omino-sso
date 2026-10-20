---
hook: before_implement
runs: speckit.api-evolve.gate
gate: true
---

# before_implement

**The gate.** Runs immediately before `/speckit.implement` dispatches any task. Refuses to proceed if API policy is unmet.

## What it does

1. Invokes `/speckit.api-evolve.gate`.
2. Validates every dispatch trailer in `tasks.md`:
   - API-prefixed tasks must dispatch `speckit.api-evolve.*` only.
   - No task may rewrite the contract version directly — that is reserved for `/speckit.api-evolve.release`.
3. On failure, prints the failing rule and exits non-zero. `/speckit.implement` aborts before writing any code.

## Failure mode

Hard — this is the whole point. The gate is the only safety net between an undeclared breaking change and a shipped break.

## Override

Set `gate.enabled: false` in `policy.yml` to disable the gate (not recommended). For one-off bypass, run `/speckit.implement --skip-hook before_implement` (logged in `orchestration.md`).
