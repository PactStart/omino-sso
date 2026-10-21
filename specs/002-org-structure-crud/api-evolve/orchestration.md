# API Evolve Orchestration — 002-org-structure-crud

特性：[组织架构增删改](../spec.md) | 契约：`internal/api/docs/openapi.json`（OpenAPI 3.0.3）

---

## detect — 2026-10-21T00:00:00Z

见 `detected.json`：1 个契约文件 `internal/api/docs/openapi.json`（`openapi-3`，标题「omino-sso 管理 API」，`info.version = 1.0`）。

## baseline — 2026-10-21T00:00:00Z

见 `baseline/manifest.json`：冻结 sha256 `e29a35aa…`（16 端点 / 22 schema）。该快照捕获于 `/speckit.plan` 阶段，是本特性实施前的契约锚点。

---

## Gate — 2026-10-21T09:52:00Z

Result: **PASS**（exit 0）

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking` / `removal` 无对应 `deprecations.json` 行 | 通过（空集） | `diff.json` 中 `breaking: 0`、`removal: 0`，无 `deprecations.json` 需求 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 **minor**（纯增量端点 + 增量 schema），非 `major`，无需 `bump.json` 确认 |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活于契约 | 通过（空集） | 无 `deprecations.json`，不存在弃用面 |
| 4 | Removal preceded by publish — 有 `removal` 但其弃用 `published_in_version` 为 `null` | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml` 要求迁移文档；无 breaking/removal，无迁移面 |

### 差分依据（可复现）

```bash
python3 - <<'PY'
import json
base=json.load(open('specs/002-org-structure-crud/api-evolve/baseline/internal/api/docs/openapi.json'))
work=json.load(open('internal/api/docs/openapi.json'))
ops=lambda d:{(p,m):v for p,ms in d['paths'].items() for m,v in ms.items() if m in ('get','post','put','patch','delete')}
bo,wo=ops(base),ops(work)
print("added",sorted(f"{m.upper()} {p}" for p,m in wo.keys()-bo.keys()))
print("removed",sorted(f"{m.upper()} {p}" for p,m in bo.keys()-wo.keys()))
print("modified",[f"{m.upper()} {p}" for p,m in bo.keys()&wo.keys()
                 if json.dumps(bo[(p,m)],sort_keys=True)!=json.dumps(wo[(p,m)],sort_keys=True)])
bs,ws=base['components']['schemas'],work['components']['schemas']
print("schemas+",sorted(ws.keys()-bs.keys()),"schemas-",sorted(bs.keys()-ws.keys()))
print("schemas~",[k for k in bs.keys()&ws.keys()
                 if json.dumps(bs[k],sort_keys=True)!=json.dumps(ws[k],sort_keys=True)])
PY
```

输出：

```text
added    ['DELETE /api/v1/admin/orgs/{id}', 'PATCH /api/v1/admin/orgs/{id}', 'POST /api/v1/admin/orgs']
removed  []
modified []                      # 16 个既有操作逐字节未变
schemas+ ['admin.CreateOrgRequest', 'admin.UpdateOrgRequest', 'dto.OrgDetail']
schemas- []
schemas~ ['admin.OrgNode']       # 仅新增 example 注解（properties/type/required 未变）
```

### 结果解读

- **无破坏性变更**：既有 16 个操作的 path、请求/响应 schema 逐字节一致；`GET /admin/orgs/tree` 的报文契约未变（`OrgTree` handler 仅换文件归属，`OrgNode` 只补 `example`）。
- **`PATCH /api/v1/admin/users/:id` 的行为修正不属于契约变更**：请求/响应字段与错误码集合未变，仅把「`deptId` 指向不存在部门」从静默写入改为 `400 VALIDATION_FAILED`（`VALIDATION_FAILED` 早已在该端点的 `@Failure 400` 声明中），因此无需 deprecation 流程；该行为修正已在 `AGENTS.md §4.1` 显式登记。
- **免检项**：`/oauth2/*`、`/.well-known/*` 由协议库生成且本特性未触碰；`/docs/*` 属非业务面。

### 钩子第 2 项核验（tasks.md 派发规则）

`tasks.md` 中不存在 API 前缀任务，也没有任何任务直接改写契约版本号（唯一涉及文档的任务是 T033/T034 的 `make docs` 重新生成与漂移校验），因此不触发 `speckit.api-evolve.*` 派发约束。

---

## 后续

- `diff.json` 已按本条基线生成，供下一个特性做差分锚点。
- 已知限制（沿用 001）：`policy.yml` 与 `clients:` 段仍未建立；若后续出现 breaking 变更，必须先补这两项，否则 `client-impact` 只能给空表。
