# API Evolve · Orchestration — `006-app-admin-role`

**状态说明**：本特性在 `after_specify`（detect）与 `after_plan`（baseline）阶段未产出 `detected.json` / `baseline/`；项目根与特性目录均无 `policy.yml`，按缺省策略判定。

契约面事实：本特性为**纯 additive**——新增 `PUT /api/v1/admin/clients/:id/members/:uid/admin`、17 条门户端点（`/api/v1/me/apps/:id/*` 16 条 + `/api/v1/me/orgs/tree`）；响应字段新增 `AppItem.isAppAdmin`、`AppMemberItem.isAdmin`；新增错误码 `APP_MEMBER_NOT_FOUND`；新增审计事件 `app_member_admin_granted` / `app_member_admin_revoked`；新增列 `client_members.is_admin`（带默认值，向后兼容）。未删除、改名或收窄任何既有端点/字段。

---

## Gate — 2026-10-22

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`；零契约删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 无 `bump.json`，隐含 bump 为 `minor`（纯 additive） |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但 `published_in_version` 为 null | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml`；无 breaking/removal |

Exit code: `0`

### 备注

- 唯一的行为性质变化：门户侧新增一条独立的授权路径（`appAdmin`）。它**不改变**既有 `/api/v1/admin/*` 的语义（平台管理员能力不变，FR-013），因此不构成对外 breaking。
- 未注册的路径（门户密钥 / 设为管理员）在 `NoRoute` 下返回统一 `404 NOT_FOUND`，由 `internal/api/router_test.go` 断言。门户基础信息与 Logo 已于 2026-10-22 口径调整后注册（`/me/apps/:id/detail`、`PATCH /me/apps/:id`、`POST|DELETE /me/apps/:id/logo`，见下方复跑 Gate）。

---

## Gate — 2026-10-22（实现后复跑，含门户应用信息页）

Result: **PASS**

新增契约面（纯 additive）：`GET /api/v1/me/apps/:id/detail`、`PATCH /api/v1/me/apps/:id`（4 字段白名单）、`POST|DELETE /api/v1/me/apps/:id/logo`。无删除、改名或收窄既有端点/字段；隐含 bump 仍为 `minor`。

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking | 通过（空集） | 无 `breaking.json` / `diff.json`；零删除 |
| 2 | Bump confirmed | 通过（不适用） | 无 `bump.json`，隐含 bump `minor` |
| 3 | Sunset honored | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present | 通过（不适用） | 无 `policy.yml`；本阶段无结构变更（`is_admin` 已在 0006 落地） |

Exit code: `0`

## Gate — 2026-10-22（Phase 11 实现前复跑）

Result: **PASS**（本轮仅文档措辞与前端用例，契约面无变化；`internal/api/docs/openapi.json` 无新增 diff）
