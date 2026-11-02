# Phase 0 Research: 应用访问按组织架构授权

**Feature**: `010-app-access-org-grant` | **Date**: 2026-11-01

本文件收敛 spec 未预设的 11 个实现决策。每条给出 Decision / Rationale / Alternatives considered。**无遗留 NEEDS CLARIFICATION**。

---

## R1. 组织授权的存储形态：独立关系表，不复用 `client_members`

**Decision**：新增表 `client_org_grants(client_id, org_id, include_sub, granted_by, granted_at, updated_at)`，PK `(client_id, org_id)`，附加索引 `(org_id)`。授权对象只有部门，不展开成员。

**Rationale**：
- 授权是「对部门的许可」，撤销它必须与「手动加入的成员」完全解耦（spec FR-008）；复用成员表就必须再引入「来源列 + 谁有权删」的判定，撤销一个部门授权时无法原子地区分哪些行是快照、哪些是人工加。
- 现状 `AddMembersByDept` 证实了快照的代价：写进 `client_members` 之后，来源信息永久丢失，无法回答 FR-016 的「这个人从哪来的」。
- 一表一语义，`client_members` 的既有不变量（角色绑定前必须是成员）不需要修改。

**Alternatives considered**：
- 复用 `client_members` 加 `source`/`org_id` 列：被否。存量行无法回填来源，且撤销时需按 `(source='org', org_id)` 批量删，删完还要重算「是否仍有角色绑定」，与 003 的「移除成员即解除角色绑定」冲突。
- 在 `clients` 上加 `access_scope` JSON 字段：被否。违背 §6 关系统一建表的既有形态，且无法按组织反查（部门删除校验需要）。

---

## R2. 「含子部门」用物化路径前缀匹配，授权表不存路径快照

**Decision**：判定条件为
`(g.include_sub = 1 AND dept.path LIKE CONCAT(grantOrg.path, '%')) OR (g.include_sub = 0 AND dept.id = g.org_id)`，
其中 `dept.path`、`grantOrg.path` 均**实时**从 `organizations` 取，授权表只存 `org_id`。

**Rationale**：
- `organizations.path` 已是既有物化路径（`/ID/ID/`），ULID 字符集不含 `%`/`_`，`LIKE` 无需转义（`model.OrgPathPattern` 已固化该前提）。
- 部门迁父时会整体平移子树路径（`UpdateSubtreePaths`）；若授权表存路径快照，迁移后授权会指向错误的人。实时 JOIN 天然免疫。
- `include_sub = 0` 走 `dept.id = g.org_id` 的等值比较，避免把 `/A/` 前缀误命中 `/A/B/` 之外的情况（前缀匹配在 `include_sub=1` 时才使用）。

**Alternatives considered**：
- 递归 CTE 展开子树：MySQL 8.4 支持，但每条授权都要递归一次，成本高于一次前缀 range scan；且 002 已用路径方案，混用两套会新增理解成本。
- 授权时把成员物化下来（定时任务或事件触发）：见 R3，被否。

---

## R3. 只有一个判定实现：SQL 片段生成器 + 等价性测试

**Decision**：在 `internal/store/model/client_org_grant.go` 提供唯一判定片段构造函数
`OrgGrantMatchSQL(grantAlias, grantOrgAlias, deptAlias string) string`，
被 6 处复用：`/me/apps` 可见性、单条覆盖人数、独占（撤销影响）人数、`visibleTotal`、成员来源、候选标记。

**Rationale**：
- spec FR-004 要求「覆盖人数与可见性判定使用同一口径，不得两套独立逻辑」，唯一实现是最省事的满足方式。
- 片段内部只含列名与常量，不含任何用户输入，参数一律走绑定；因此不构成 SQL 注入面。
- 6 处调用点全部在 `store` 层，`service` 不感知 SQL（宪法原则 III）。

**Alternatives considered**：
- Go 侧内存匹配（把全量在职用户与授权加载后比对）：被否。管理页面的列表每次都要全表扫用户，规模一涨就是隐性瓶颈；且会与 `/me/apps` 的 SQL 判定形成两套逻辑。
- 生成视图（VIEW）/存储过程：被否。违背「结构变更只走手写 SQL 迁移 + 应用层判定」的既有约束，且难以在测试里按 fixture 断言。
- **风险与缓解**：片段是字符串拼接，重命名列时容易静默失效。缓解：全部调用点集中在 `org_grant_store.go` 与 `app_auth_store.go` 两处，并由 integration 测试对同一组 fixture 断言「单条覆盖人数 = 逐用户可见性判定结果」。

---

## R4. 覆盖人数与可见性用 SQL 聚合，不引入缓存

**Decision**：
- 单应用授权列表：`SELECT g.org_id, (SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id WHERE u.status = 1 AND <match>) AS covered FROM client_org_grants g WHERE g.client_id = ?`，一次查询返回全部授权的覆盖人数。
- 可见性（`/me/apps`）：在既有 `ListClientsForMember` 上改为 `LEFT JOIN client_members` + `EXISTS (组织授权命中)`，仍是单条 SQL。

**Rationale**：授权条数上限 500、实际通常 < 20，相关子查询数量可控；子树的 `users.dept_id` 有索引、`organizations.path` 有索引，2000 人部门的前缀扫描是毫秒级（对应 SC-005）。不缓存是 spec 的硬要求（组织变动必须下一次请求即生效，FR-013）。

**Alternatives considered**：
- Redis 缓存覆盖人数：被否。组织一变就要失效一批 key，失效逻辑比省下的查询更贵，且违反 FR-013。
- 引入 `COUNT(*)` 估算（`information_schema`）：被否，精度不够，且 admin 会看到与成员页不一致的数字。

---

## R5. 「撤销影响人数」预置在列表响应里，不新增预检端点

**Decision**：`GET .../org-grants` 的每条记录同时返回 `coveredUsers`（覆盖人数）与 `uniqueUsers`（**仅**因该授权可见的人数：命中该授权 ∧ 非手动成员 ∧ 未被其它授权命中）。撤销确认框直接使用列表里的 `uniqueUsers` 作为「撤销后 X 人将不再可见」（spec FR-007）。

**Rationale**：
- 撤销是一次点击动作，多一次往返换取一个数字并不划算；列表已经把整页授权带回来了，把 `uniqueUsers` 一起算掉只多一个 `NOT EXISTS`。
- 避免为一个只读数字新增 path，从而少 2 个 operation 与一份契约维护成本。

**Alternatives considered**：
- 独立预检端点 `GET .../org-grants/:orgId/impact`：被否（见上）。若未来授权条数接近 500 导致列表超时，可退化为此端点，对外契约不变。

---

## R6. 成员来源与 `visibleTotal` 都用限域 SQL，不做 N+1

**Decision**：
- 来源：先按既有条件分页取成员（`LEFT JOIN organizations o` 已带 `dept_id`），再用**一条** `JOIN client_org_grants ON <match> WHERE m.user_id IN (当页 ≤50 个 id)` 取回当页成员的命中授权（部门名 + 是否含子部门），内存按用户分组。
- `visibleTotal`：一条聚合 `COUNT(DISTINCT u.id)`，条件为「账号启用 ∧ 应用启用 ∧（是成员 ∨ 被授权命中）」，与 `GET .../org-grants` 的覆盖比并集一致。
- 成员列表响应新增 `visibleTotal`，`total` 语义不变（仍是手动成员数，spec FR-019 要求两者可区分）。

**Rationale**：来源只对当页 ≤50 行有意义；跨页全选仍走既有 `memberIds` 端点，不受影响。`visibleTotal` 是单值，一次聚合即可，避免前端自己相加（相加会因多来源重复计数）。

**Alternatives considered**：
- 成员表加冗余 `source` 列：被否（见 R1）。
- 前端并行拉取全部授权再本地匹配：被否。前端拿不到全量用户-部门映射，且会把「口径」搬到浏览器。

---

## R7. 既有「按部门批量加入」：接口保留一期、UI 入口下线

**Decision**：
- 前端（admin `ClientMembers.vue` 与 portal `Members.vue`）删除「按部门加入」按钮与对话框，替换为「添加组织授权」。
- 后端 `POST /admin/clients/:id/members`（含 `{deptId, includeSubDepts}` 形态）**保留**，OpenAPI 描述标注「已由组织授权取代，仅为兼容存量脚本保留，后续版本移除」；`AddMembersByDept` 逻辑不动。
- 存量由该能力写入的成员，一律视为「手动成员」，`joinedAt`/`addedBy` 保持不变（spec FR-026）。

**Rationale**：接口层直接删是破坏性变更，与「先声明、后下线」的既有节奏不符；但用户能看到的入口必须只剩一个，否则管理员无法判断哪套生效（spec FR-027 与 Clarifications）。

**Alternatives considered**：
- 同时保留两个 UI 入口：被否，直接违反 FR-027。
- 立刻删除接口：被否（外部脚本/示例可能引用；且需要单独提交与迁移说明）。

---

## R8. 部门删除保护落在既有 `OrgService.Delete` 的同一事务内

**Decision**：`OrgService.Delete` 在「有子部门 / 有账号」两项校验后，增加 `HasOrgGrants(orgID)` 校验，命中返回 `409 ORG_HAS_GRANTS`（「部门仍被应用的组织授权引用，请先撤销授权」）。校验在该函数既有的 `withWriteTx` + `LockAllOrganizations` 内执行，与现有并发语义一致。

**Rationale**：`client_org_grants` 是逻辑外键（项目不使用物理外键），悬空 `org_id` 会让授权静默失效、覆盖人数变 0 而无人察觉；把校验塞进已存在的临界区，天然与并发删除互斥。

**Alternatives considered**：
- 数据库外键 + `ON DELETE RESTRICT`：被否，项目既有约定不使用物理外键。
- 删除时级联清理授权：被否，会让「删部门」静默撤掉一批人的访问权，违反 spec 的 Edge Case 要求（必须显式提示）。

---

## R9. 文档与注册表同步清单（缺一即门禁失败）

**Decision**：随代码提交同步更新以下 5 处，并在 PR 描述里逐条打勾：

| 位置 | 内容 | 校验者 |
|---|---|---|
| `AGENTS.md` §4.1 | 6 行新路由（admin 3 + 门户 3） | `scripts/check-registry.sh`（逐条比对 `router.go`） |
| `AGENTS.md` §5.3 | `ORG_HAS_GRANTS`、`APP_ORG_GRANT_LIMIT` | 同上（错误码双向比对） |
| `AGENTS.md` §6.7 | `app_org_granted` / `app_org_grant_updated` / `app_org_grant_revoked` | 同上（事件双向比对） |
| `AGENTS.md` §6.12 | `client_org_grants` 表定义 | 人工（§6 无脚本比对，但 `check-artifact-refs` 校验 §3 目录树） |
| `internal/api/docs/openapi.json` | 重新生成 | `make docs-check` + `router_test.go` 的 operation 断言 78 → 84 |

**Rationale**：项目把「代码 / 规范 / 文档」三向漂移做成了脚本门禁；本节把它固化成清单，避免实现期漏项。

**Alternatives considered**：无（这是既有流程，不重设计）。

---

## R10. 前端两端复用方式：复制实现，不抽公共包

**Decision**：`web/admin/src/views/client/ClientMembers.vue` 先改，然后按 006 的既定做法把同一段实现复制到 `web/portal/src/views/app-admin/Members.vue`（含「组织授权」卡片、来源列、二次确认文案、组织树加载失败提示）。API 层两侧各自新增 3 个函数与类型。

**Rationale**：两个独立 Vite 工程 + 两套 `package.json`；抽公共包要么引入 workspace 要么做路径别名穿透，构建耦合成本高于一次重复。006 已用同样决策交付并写明理由，保持一致性比"更 DRY"更重要。

**Alternatives considered**：
- 建 `web/shared` 包并由两个工程 `import`：被否（跨工程构建、类型声明与版本锁定成本，且 element-plus 引入范围在 portal 被限死在 `views/app-admin/`）。
- 只做 admin 端、门户跳过：被否，违反 spec FR-020。

---

## R11. 审计事件划分：3 个新事件，按条件而非按 HTTP 状态码

**Decision**：新增 `app_org_granted`、`app_org_grant_updated`、`app_org_grant_revoked`。
- `app_org_granted`：新增一条授权，`detail = {deptId, deptName, deptPath, includeSubDepts, coveredUsers}`
- `app_org_grant_updated`：仅切换 `includeSubDepts`，`detail = {deptId, deptName, deptPath, from, to, coveredUsers}`
- `app_org_grant_revoked`：撤销，`detail = {deptId, deptName, deptPath, includeSubDepts, impactUsers}`
- 批量提交按条记录，不合并（FR-024）；失败（部门不存在、超上限、并发已删除）记 `result=failure` 且 `detail` 带原因分类；操作者由 `Auditor.Emit` 从 request context 回填（006 已实现）。

**Rationale**：三个事件分别对应 spec 的「新增」「撤销」与 FR-003 的「调整含子部门设置也要留痕」；`deptPath` 冗余存储是为了部门改名/迁移后仍能追溯当时授权的是哪棵子树（不依赖实时 JOIN）。

**Alternatives considered**：
- 只用 `app_grant_updated`（复用既有事件）：被否，FR-023 要求可按组织授权类型筛选，混用会让审计页无法区分「角色-权限绑定」与「部门授权」。
- 用一条事件 + `action` 字段：被否，既有审计页按 `event_type` 筛选，不解析 `detail`。

---

## R12. critique 复评结论（2026-11-01）

`/speckit.critique` 提出 3 条 Must-Address，用户确认后按下列方案落地（详见 [critiques/critique-20261101-2221.md](./critiques/critique-20261101-2221.md)）：

**Decision**
1. **定位 = 门户可见性，不做访问控制**（P3/X1/E6）。举证：`internal/oidc/claims.go:55` 的 `IsClientMember` 仅用于 userinfo 计字段、`internal/oidc/storage.go:76` 只校验账号 `status`、`RemoveMembers` 不吊销会话/令牌。因此 spec 新增 FR-029 明确「不阻断直连 SP 的 SSO、不终止既有令牌」，方案②（authorize 拦截 + 令牌吊销）另立专项。
2. **写入进事务并锁目标部门**（E2/E4）。R4 原设想"service 层 `GetOrganization` 预检"改为事务内 `LockOrganization` + 同事务计数上限，与 002 对 `users.dept_id` 的临界区一致。
3. **`impactUsers` 在删除前、同事务内计算**（E5）。原契约措辞「撤销后仅因该授权可见的人数」恒为 0，已改为"撤销将导致多少人不再可见"。

**同时采纳的实现约束**：三态计数不依赖 `ON DUPLICATE KEY`（E3）、`visibleTotal` 仅 `page=1` 计算（E9）、`LIKE CONCAT(列,'%')` 的索引限制需 EXPLAIN 留证（E8）、门户路由必须挂 `appAdminGroup`（E7）。

**Rationale**：这四项都是"改一行 SQL / 一句文案就能避免的事故"，成本远低于上线后排查。

**Alternatives considered**：把定位升级为真访问控制——被否，触及宪法 I 的边界且需定义令牌吊销范围，与本期目标（消除补员链路）不成比例。
