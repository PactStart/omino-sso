# API Evolve · Orchestration — `001-api-docs`

**状态说明**：`after_specify`（detect）与 `after_plan`（baseline）两个 hook 均为 `optional: true`，本特性从未执行，故不存在 `detected.json` / `baseline/` / `diff.json` / `classification.md` / `bump.json` / `deprecations.json` / `policy.yml` / `client-impact.md`。缺省策略（项目根与特性目录均无 `policy.yml`）下的门禁判定如下。

契约面事实：本特性不修改任何既有端点的请求/响应契约，仅新增 16 个端点的人类可读文档（`internal/api/docs/openapi.json`）与 `/docs/*` 非业务路由；`/oauth2/*`、`/.well-known/*` 由 `zitadel/oidc/v3` 生成，未改动。

---

## Gate — 2026-10-20T15:07:00+08:00

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking` / `removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`，不存在 `breaking` / `removal` 条目；本特性零契约删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 `none`（纯增量文档，未触碰既有契约面），非 `major`，无需 `bump.json` |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活于契约 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但其弃用 `published_in_version` 为 `null` | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml` 要求迁移文档；且无 breaking/removal，无迁移面 |

Exit code: `0`

### 本次门禁执行方式

规则 1/3/4 为空集判定（无 `diff.json`、无 `deprecations.json`）；规则 2/5 依赖 `bump.json`、`policy.yml`，两者均不存在时按缺省策略取「无 major 变更」「不要求迁移文档」。逐条核验命令：

```bash
for f in detected.json diff.json classification.md policy.yml deprecations.json bump.json client-impact.md orchestration.md; do
  [ -e "specs/001-api-docs/api-evolve/$f" ] && echo "FOUND $f" || echo "absent $f"
done   # 除本文件外全部 absent
ls api-evolve 2>/dev/null   # 项目根无 api-evolve/policy.yml
```
