# API Evolve · Orchestration — `004-object-storage-upload`

**状态说明**：`after_specify`（detect）与 `after_plan`（baseline）在本特性执行期间已产出 `detected.json` 与 `baseline/`（`manifest.json` + `baseline/internal/api/docs/openapi.json`，捕获于 2026-10-21T09:12:54Z）。缺省策略（项目根与特性目录均无 `policy.yml`）下的门禁判定如下。

契约面事实：本特性新增文件上传与管理相关端点（`/files/upload-credential`、`/files/confirm`、`/admin/clients/:id/logo`、`/me/avatar`、`/admin/users/:id/avatar`、`/admin/files` 及其 `PATCH`），并为 `FileItem`、`CreateClientRequest`、`FileQuery` 等新增字段，均为 **additive**（新增端点/新增字段），未删除或收窄任何既有端点/字段，故无 `breaking` / `removal`。

---

## Gate — 2026-10-21

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`，不存在 `breaking`/`removal` 条目；本特性零契约删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 无 `bump.json`，隐含 bump 非 `major`（纯 additive），无需 `bump.json` |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活于契约 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但其弃用 `published_in_version` 为 `null` | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml` 要求迁移文档；且无 breaking/removal，无迁移面 |

Exit code: `0`

### 本次门禁执行方式

规则 1/3/4 为空集判定（无 `diff.json`、无 `deprecations.json`）；规则 2/5 依赖 `bump.json`、`policy.yml`，两者均不存在时按缺省策略取「无 major 变更」「不要求迁移文档」。逐条核验：

```bash
for f in detected.json diff.json classification.md policy.yml deprecations.json bump.json client-impact.md orchestration.md; do
  [ -e "specs/004-object-storage-upload/api-evolve/$f" ] && echo "FOUND $f" || echo "absent $f"
done   # 仅 detected.json 存在；baseline/ 目录存在
ls api-evolve 2>/dev/null   # 项目根无 api-evolve/policy.yml
```
