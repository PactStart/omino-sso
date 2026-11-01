# Quickstart: 应用访问按组织架构授权

**Feature**: `010-app-access-org-grant` | **Date**: 2026-11-01

本文件是**验证剧本**，不是实现说明：按顺序执行即可证明 spec 的 P1/P2 需求成立。字段与错误码细节见 [contracts/](./contracts/)，表结构与判定口径见 [data-model.md](./data-model.md)。

## 0. 前置条件

```bash
make deps                 # 依赖与 git hooks
make start                # mysql + redis + 服务（或 docker compose up -d mysql redis）
make migrate-up           # 含新迁移 0007_client_org_grant.up.sql
make dev                  # 前台跑服务（:8080 OIDC / :8081 API）
```

准备数据（既有后台或直连 SQL）：组织树 `研发部 / 研发部-平台组 / 市场部 / 市场部-品牌组`；账号 `dev1`(研发部-平台组)、`dev2`(研发部)、`dev3`(研发部本级，用于"不含子部门仍可见"的对照)、`mkt1`(市场部-品牌组)、`mkt2`(市场部-品牌组)、`gone1`(研发部-平台组，`status=0`)；应用 `AppC`（启用）。

> **迁移回滚（critique E13）**：`make migrate-down` 对 `0007` 只执行 `DROP TABLE client_org_grants`，其余表与数据不变，但**全部组织授权配置会丢失**。回滚顺序固定为「先回滚服务镜像 → 再执行 `migrate-down`」：旧代码不引用新表，提前 `down` 不会崩，但新代码在表消失后请求组织授权接口会 500。回滚后需重新配置授权，并在变更单中写明。

## 1. 授权即生效（US1 / FR-001–005）

1. admin 登录，进入 `AppC` → 应用成员 → 「组织授权」→ 添加 `研发部`（含子部门）。
2. 期望列表中一行：`研发部`、含子部门、覆盖 3 人（`dev1`、`dev2`、`dev3` 中在职者，**不含** `gone1`）。

```bash
curl -s -b cookie.txt "http://localhost:8081/api/v1/admin/clients/AppC/org-grants" | jq '.data.list[0] | {deptName,includeSubDepts,coveredUsers,uniqueUsers}'
```

3. `dev1` 登录门户 → 我的应用出现 `AppC`，点击可完成 SSO。
4. `mkt1` 登录门户 → 看不到 `AppC`。
5. 改为「研发部（不含子部门）」→ `dev2` 可见、`dev1` 不可见（`dev3` 若直属研发部则仍可见）。
6. 重复提交同一部门 → `added=0, updated=0, unchanged=1`，覆盖人数不变（幂等）。

## 2. 组织变动自动跟随（US2 / FR-011–013）

| 操作 | 期望 |
|---|---|
| 把 `mkt1` 的部门改为 `研发部-平台组` | 刷新门户，「我的应用」出现 `AppC`，**无需**任何授权侧操作 |
| 把 `mkt1` 改回 `市场部-品牌组` | 刷新后 `AppC` 消失 |
| 新建账号 `dev4` 直接分配到 `研发部-平台组` 并首次登录 | 直接可见 `AppC` |
| 把 `dev1` 置为 `status=0`（停用） | 无法登录；即使部门仍被授权也不可见，且不再计入 `coveredUsers` |

> 判定是请求时实时计算（无定时任务、无缓存）：以上每项都应「下一次刷新」即生效，不允许出现需要等待同步的窗口。

## 3. 撤销可预期（US3 / FR-007–008、FR-016、FR-019）

1. 先在 `AppC` 手动加入 `dev1`（此时 `dev1` 来源 = 手动 + 研发部）。
2. 打开「组织授权」，撤销 `研发部`：
   - 确认框文案必须显示「撤销后 N 人将不再可见」，N = `uniqueUsers`（本例不含 `dev1`）。
   - 撤销后 `dev1` **仍在**成员列表、仍可见 `AppC`（来源只剩「手动」），角色绑定数不变。
3. 成员页顶部统计必须同时显示「手动成员 1 人 / 实际可见 N 人」（`total` 与 `visibleTotal`），不得只显示一个数字。
4. 空部门授权：给一个无人的部门授权 → 列表显示「0 人」，记录保留、不自动删除。

## 4. 两端一致与权限（US4 / FR-020–022）

1. 把 `dev2` 设为 `AppC` 的应用管理员（admin 端），用 `dev2` 登录门户 → 应用管理 → 成员 → 组织授权，新增 `市场部（含子部门）`。
2. admin 端同一应用刷新 → 看到同一条授权与相同覆盖人数（两端同一实现）。
3. `dev2` 直接请求 `GET /api/v1/me/apps/AppD/org-grants`（`AppD` 未授权给他）→ `403 FORBIDDEN`，文案与其他 appAdmin 拒绝一致，不泄露应用存在性。
4. 平台 admin 角色但非该应用管理员 → 门户路径同样 `403`。

## 5. 部门删除保护（FR-028 / Edge Cases）

```bash
curl -s -o /dev/null -w '%{http_code}\n' -b cookie.txt -X DELETE \
  "http://localhost:8081/api/v1/admin/orgs/<研发部-平台组ID>"
# 期望 409，body.code = ORG_HAS_GRANTS
```

撤销该部门授权后再删 → 成功；删除仍被**其它**应用授权引用的部门 → 仍 409。

## 6. 审计（US5 / FR-023–025）

1. 新增一条授权 → 审计页出现 `app_org_granted`，`detail` 含 `deptName/deptPath/includeSubDepts/coveredUsers`，操作者正确。
2. 切换 `includeSubDepts` → `app_org_grant_updated`（`from/to`）。
3. 撤销 → `app_org_grant_revoked`（`impactUsers`）。
4. 批量提交 3 条 → 3 条独立事件，不合并。
5. 按事件类型筛选 → 三类各自可筛出，分页行为与既有事件一致。

## 6.5 边界与降级验证（FR-029 / FR-030）

1. **不做访问控制（FR-029）**：撤销 `AppC` 的全部授权后，用一个"已登录过 `AppC`、且现在既非成员也未被授权"的账号，直接在浏览器打开 `AppC` 的登录入口（SP 发起 authorize）→ **仍可完成授权并进入**。这是预期行为，页面与文档已写明"撤销不会让已登录用户掉线，也不阻止直连接入方"。
2. 同一账号在门户刷新「我的应用」→ `AppC` 已消失（可见性生效）。
3. 该账号仍持有 `AppC` 的 refresh_token 且可继续换发 access_token（本期**不**回收令牌）；若需立即失效，唯一手段是停用账号（`status=0`）→ 下一次 authorize 与 userinfo 均失败。
4. **降级（FR-030）**：临时让组织授权列表查询失败（例如 `RENAME TABLE client_org_grants TO client_org_grants_bak`）→ 成员列表照常展示、组织授权卡片显示可重试错误、可见总人数显示"—"而非 0；`RENAME` 回来后重试即恢复。

## 7. 自动化门禁（提交前必跑）

```bash
make test              # Go 单测 + 前端 vitest（新增：组织授权匹配 / 覆盖人数 / 来源列）
make test-integration  # MySQL 真库：可见性 SQL 与覆盖人数口径等价性
make lint
make docs              # 重新生成 openapi.json（新增 6 个 operation）
make docs-check        # 漂移比对
make check             # check-registry（路由/错误码/事件）/ docs-commands / artifact-refs / build-env
make e2e               # 四域 HTTPS 端到端（可选，但涉及门户可见性时建议跑）
```

**通过判据**：`make check` 四类全绿（尤其 §4.1 的 6 条新路由、`ORG_HAS_GRANTS`/`APP_ORG_GRANT_LIMIT`、3 个审计事件均已在 AGENTS 登记），`router_test.go` 的 operation 断言为 84。

**性能留证（critique E8 / SC-005）**：用固定 fixtures（2000 人在职员工的部门子树 + 该应用 21 条授权）对两条最热 SQL 做 `EXPLAIN` 并记录服务端 p95。**结论已实测落地在本节末尾**，可按下述步骤复现。

固定 fixtures（幂等，跑在独立的 `*_test` 库上，勿在生产执行）：

```sql
SET SESSION cte_max_recursion_depth = 100000;
SET @now = UNIX_TIMESTAMP() * 1000;
DELETE FROM users WHERE username LIKE 'perf-%';
DELETE FROM client_org_grants WHERE client_id = 'perf-app';
DELETE FROM clients WHERE client_id = 'perf-app';
DELETE FROM organizations WHERE name LIKE 'perf-%';

-- 三层组织：root → mid → leaf（2000 名在职员工挂在 leaf）
INSERT INTO organizations (id, parent_id, name, path, sort, tenant_id, created_at) VALUES
  ('01PERFROOT0000000000000000', NULL, 'perf-root', '/01PERFROOT0000000000000000/', 1, 'default', @now),
  ('01PERFMID00000000000000000', '01PERFROOT0000000000000000', 'perf-mid', '/01PERFROOT0000000000000000/01PERFMID00000000000000000/', 1, 'default', @now),
  ('01PERFLEAF0000000000000000', '01PERFMID00000000000000000', 'perf-leaf', '/01PERFROOT0000000000000000/01PERFMID00000000000000000/01PERFLEAF0000000000000000/', 1, 'default', @now);

-- 18 个兄弟部门 → 授权总数 21 条（根 + 中间层 + leaf + 18 兄弟）
INSERT INTO organizations (id, parent_id, name, path, sort, tenant_id, created_at)
WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 18)
SELECT CONCAT('01PERFSIB', LPAD(n, 16, '0')), '01PERFROOT0000000000000000', CONCAT('perf-sib-', n),
       CONCAT('/01PERFROOT0000000000000000/', CONCAT('01PERFSIB', LPAD(n, 16, '0')), '/'), n, 'default', @now FROM seq;

INSERT INTO client_org_grants (client_id, org_id, include_sub, granted_by, granted_at, updated_at)
SELECT 'perf-app', id, 1, NULL, @now, @now FROM organizations WHERE name LIKE 'perf-%';

-- 2000 在职 + 50 停用（验证「停用不计入覆盖人数」）
INSERT INTO users (id, username, display_name, password_hash, dept_id, roles, status, password_updated_at, created_at, updated_at)
WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 2000)
SELECT CONCAT('01PERFU', LPAD(n, 19, '0')), CONCAT('perf-u', n), CONCAT('员工', n), '', '01PERFLEAF0000000000000000', JSON_ARRAY('user'), 1, @now, @now, @now FROM seq;
```

两条最热 SQL 的执行计划与耗时（MySQL 8.4，`SET profiling_history_size=100; SET profiling=1;` 各跑 20 次取分位）：

```sql
-- ① 组织授权列表：3 个计数子查询共用 OrgGrantMatchPlanSQL（保存后回执也用这一条）
EXPLAIN SELECT g.org_id, go.name, go.path, g.include_sub,
  (SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id
   WHERE u.status = 1 AND ((g.include_sub = 1 AND o.path LIKE CONCAT((SELECT p.path FROM organizations p WHERE p.id = g.org_id), '%'))
                        OR (g.include_sub = 0 AND o.id = g.org_id))) AS covered_users,
  (SELECT COUNT(*) ... ) AS covered_disabled_users,
  (SELECT COUNT(*) ... NOT EXISTS client_members ... NOT EXISTS 其它授权 ...) AS unique_users
FROM client_org_grants g JOIN organizations go ON go.id = g.org_id
WHERE g.client_id = 'perf-app';

-- ② 门户可见性（/me/apps）：成员 PK + 授权 EXISTS，与部门规模无关
EXPLAIN SELECT c.*, COALESCE(m.is_admin,0) FROM clients c
JOIN users u ON u.id = '<uid>' AND u.status = 1 JOIN organizations d ON d.id = u.dept_id
LEFT JOIN client_members m ON m.client_id = c.client_id AND m.user_id = u.id
WHERE c.status = 1 AND (m.user_id IS NOT NULL OR EXISTS (
  SELECT 1 FROM client_org_grants g JOIN organizations go ON go.id = g.org_id
  WHERE g.client_id = c.client_id
    AND ((g.include_sub = 1 AND d.path LIKE CONCAT(go.path, '%')) OR (g.include_sub = 0 AND d.id = g.org_id))));
```

**实测结论（2026-11-01，本机 docker MySQL 8.4，21 条授权 / 2000 名在职员工）**

| 查询 | 计划要点 | min | median | **p95** | max | 预算 |
|---|---|---|---|---|---|---|
| ① 授权列表（含 3 个计数子查询） | 授权表走 `PRIMARY(client_id, org_id)`；`organizations` 走 `idx_orgs_path`；`users` 侧为 `idx_users_dept` 上的 hash join | 327 ms | 350 ms | **499 ms** | 551 ms | 列表 ≤2s ✅ |
| ② 门户可见性 `/me/apps` | `users`/`organizations` 均为主键 const，`client_org_grants` 走 `ref(PRIMARY)`，`client_members` 走 `ref(PRIMARY)` | 0.5 ms | 0.6 ms | **0.8 ms** | 3.6 ms | 与部门规模无关 ✅ |

**保存路径**（`POST .../org-grants` ≤3s）：写入本身是 `PRIMARY` 上的 upsert（<10ms 量级），随后为出回执**再跑一次查询 ①** 取覆盖人数，因此保存耗时 ≈ ① + 一次事务，实测同量级（≈0.5s），远低于 3s 预算 ✅

**已知短板（不改口径，只记录）**：查询 ① 的计数子查询对 `users` 是**按授权数放大的扫描**（21 条授权 → 21 次 `users` 扫描）。授权上限 500 条时最坏约为当前的 20 倍，会突破 2s。届时按 `data-model.md` §7 的兜底方案改（先把授权部门展开成「祖先 ID 集合」再走 `dept_id IN (...)` 聚合），对外契约不变；当前上限内的实际用量（单应用十余条）无需改动。

**上线影响面验证（critique E12）**：在**未配置任何组织授权**的库上跑一次 §2 的全部用例，期望"行为与改造前完全一致"（`/me/apps` 结果集不变）；这证明本次是纯增量变更，不需要灰度。

## 8. 关键回归面（改动容易踩到的既有行为）

- `GET /api/v1/me` / `userinfo` 的 `app_roles`、`permissions` 仍只按**成员**计算：组织授权覆盖但非成员的用户，这两个字段应为空数组。
- `GET /api/v1/me/apps` 的 `isAppAdmin` 仍只来自 `client_members.is_admin`。
- 应用停用：门户看不到，但组织授权列表照常显示「停用」状态与覆盖人数。
- portal 侧 element-plus 仅允许出现在 `views/app-admin/`：成员页改造不得把 EP 引用扩散到其他页面。
