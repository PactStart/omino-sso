# Phase 1 Data Model: 应用访问按组织架构授权

**Feature**: `010-app-access-org-grant` | **Date**: 2026-11-01 | **Spec**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

## 1. 实体关系

```text
clients (client_id) 1 ─── n client_org_grants (client_id, org_id)
organizations (id)  1 ─── n client_org_grants (client_id, org_id)
users (dept_id)     n ─── 1 organizations (id)        # 既有，物化路径 path 支撑子树判定
client_members      成员身份（手动）—— 与组织授权并列，互不覆盖
```

- 组织授权是「应用 → 部门」的许可；**不**展开成用户行。
- 手动成员（`client_members`）与组织授权是**并集**关系，各自独立增删。
- 角色绑定仍只挂在 `client_members` 上（既有不变量：有角色必是成员）。

## 2. 新增表 `client_org_grants`（migrations/0007）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `client_id` | VARCHAR(64) `COLLATE utf8mb4_0900_as_cs` | PK(复合)，逻辑外键 → `clients.client_id` | 与 `clients.client_id` 排序规则一致，大小写敏感 |
| `org_id` | CHAR(26) | PK(复合)，逻辑外键 → `organizations.id` | 授权对象部门 |
| `include_sub` | TINYINT(1) | NOT NULL DEFAULT 1 | 1 = 命中该部门整棵子树；0 = 仅该部门本级 |
| `granted_by` | CHAR(26) | NULL | 操作者用户 ID；系统/脚本路径为空 |
| `granted_at` | BIGINT | NOT NULL | 首次授权时间（Unix 毫秒） |
| `updated_at` | BIGINT | NOT NULL | 最近一次 `include_sub` 变更时间 |

**索引**

| 名称 | 列 | 用途 |
|---|---|---|
| PRIMARY | `(client_id, org_id)` | 幂等写入、按应用列授权、按 `(client_id, org_id)` 撤销 |
| `idx_client_org_grants_org` | `(org_id)` | 部门删除前的引用校验 `HasOrgGrants`；按部门反查被哪些应用授权 |

不存 `org_path` 快照：路径随部门迁移整体平移，快照会失效（research R2）。审计事件里另存路径用于事后追溯（research R11）。

**迁移 up（要点）**

```sql
CREATE TABLE IF NOT EXISTS client_org_grants (
  client_id   VARCHAR(64) COLLATE utf8mb4_0900_as_cs NOT NULL,
  org_id      CHAR(26)    NOT NULL,
  include_sub TINYINT(1)  NOT NULL DEFAULT 1,
  granted_by  CHAR(26)    NULL,
  granted_at  BIGINT      NOT NULL,
  updated_at  BIGINT      NOT NULL,
  PRIMARY KEY (client_id, org_id),
  KEY idx_client_org_grants_org (org_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
```

**迁移 down**：`DROP TABLE IF EXISTS client_org_grants;`

回滚影响：仅丢失组织授权关系；手动成员、角色、权限、审计事件均不受影响（组织授权的撤销不会级联任何数据）。

## 3. 判定口径（唯一实现）

核心谓词（`OrgGrantMatchSQL` 生成，`g` = 授权行，`go` = 授权的部门，`d` = 用户所属部门）：

```text
(g.include_sub = 1 AND d.path LIKE CONCAT(go.path, '%'))
OR (g.include_sub = 0 AND d.id = g.org_id)
```

前置条件（所有口径共同）：`d` 来自 `users.dept_id → organizations`，且 `users.status = 1`（在职启用）。

| 口径 | 定义 | 出现位置 | 对应 spec |
|---|---|---|---|
| **可访问** | 应用 `status=1` ∧ 账号 `status=1` ∧（是成员 ∨ 被任一授权命中） | `GET /api/v1/me/apps`、`visibleTotal` | FR-011 / FR-012 |
| **覆盖人数**（单条授权 `coveredUsers`） | 命中该授权的在职启用**账号数** | 组织授权列表 | FR-004 / FR-005 |
| **停用覆盖数**（`coveredDisabledUsers`） | 命中该授权但 `status≠1` 的账号数 | 撤销确认框补充说明 | FR-007 |
| **独占人数**（`uniqueUsers`） | 命中该授权 ∧ 非手动成员 ∧ 未被其它授权命中 ∧ 在职启用 | 组织授权列表（供撤销确认） | FR-007 |
| **可见总人数**（`visibleTotal`） | 可访问 ∧ 在职启用 的去重人数，仅成员列表 `page=1` 计算 | 成员列表响应 | FR-019 |
| **来源**（`orgGrants`） | 该成员命中的授权列表（部门名 + includeSub） | 成员列表每行 | FR-016 |
| **存量迁移提示**（`orgGrantHints`） | 某部门成员数 ≥ 20 且该部门及其祖先均未被授权 | 成员列表 `page=1` | FR-031 |

> 前置条件 `应用 status=1` 只作用于「可访问」；管理页的覆盖人数**不**因应用停用而归零（管理员需要看到停用期间仍被授权的范围，spec Edge Cases）。

## 4. 校验规则

| 规则 | 位置 | 失败语义 | spec |
|---|---|---|---|
| `org_id` 必须存在于 `organizations` | **写事务内** `LockOrganization(orgID)`（不仅 `GetOrganization`；避免"校验后部门被并发删除"留下悬空行） | `400 VALIDATION_FAILED`（沿用 `ErrAppDeptNotFound` 映射），整批回滚 | FR-009 |
| 同一 `(client_id, org_id)` 幂等 | store：先读后分流为 insert / update（仅 `include_sub` 真变化）/ no-op，**不依赖 `ON DUPLICATE KEY` 影响行数** | 不报错；`include_sub` 变化时写 `app_org_grant_updated` | FR-003 |
| 单应用授权条数 ≤ 500 | service：与插入**同一事务内** `COUNT`（并发下不可超限） | `400 APP_ORG_GRANT_LIMIT`，整批回滚 | FR-010 |
| 单次提交条数 1–500 | service | `400 VALIDATION_FAILED` | FR-002 |
| 部门被授权引用时禁止删除 | `OrgService.Delete`（既有全表锁事务内） | `409 ORG_HAS_GRANTS` | FR-028 |
| 撤销只删授权行 | store（`DELETE ... WHERE client_id=? AND org_id IN (...)`，按 `RowsAffected` 计实际撤销数） | 不存在视为成功（幂等） | FR-008 |

## 5. 生命周期

组织授权无状态机，只有三种写事件：

1. **新增**：不存在 → 插入，写 `app_org_granted`。
2. **调整**：存在且 `include_sub` 变化 → 更新 + `updated_at`，写 `app_org_grant_updated`；`include_sub` 未变化 → 完全幂等，不写事件。
3. **撤销**：删除，写 `app_org_grant_revoked`。

**自动生效路径**（无写入，纯读）：用户 `dept_id` 变更 / 账号停用 / 部门迁移 / 部门删除前置校验 —— 都在下一次请求按新归属重新计算，不产生任何组织授权行变更（FR-013）。

## 6. 与既有模型的兼容约束

- `client_members` 结构不变；`ListClientsForMember` 由「成员 INNER JOIN」改为「`LEFT JOIN` 成员 + 组织授权 `EXISTS`」，返回行新增 `is_member` 标记，`is_admin` 语义不变（仍只来自成员标记）。
- 003 的不变量保持不变：角色绑定必然对应成员行；组织授权覆盖者被授角色时自动补成员（FR-015）。
- `CountUsersInSubtree`（002 的子树汇总人数）**不改动**——它统计全部账号；本特性的覆盖人数是「在职启用」口径，另建查询，避免把 002 的语义悄悄改掉。
- `audit_events` 表结构不变，只新增 `event_type` 枚举值（§6.7 登记）。

## 7. 规模与索引评估

| 查询 | 驱动与访问路径 | 2000 人部门子树 | 备注 |
|---|---|---|---|
| `/me/apps` 可见性 | 按 `users.id` 定位 → `client_members` PK 命中 → 授权表按 `client_id` 扫描（≤500）→ `organizations` PK 取 path | 与部门规模无关 | 单条 SQL，无缓存 |
| 组织授权列表 + 覆盖人数 | 授权表按 `client_id`（≤500 行）× 每行一次子树聚合（`organizations.path` 前缀 range + `users.dept_id` 索引） | 单条 ms 级；总数受授权条数线性 | SC-005 目标内 |
| `visibleTotal` | `users` 扫描（`status=1`）× 两次 `EXISTS` | 1e3–1e4 行，管理页低频 | 若未来超时，可加「按应用 `COUNT` 物化」；对外契约不变 |
| 成员来源 | 当页 ≤50 个 `user_id` → `client_members` PK + 授权 JOIN | 常数级 | 无 N+1 |
| 部门删除校验 | `idx_client_org_grants_org` 等值命中 | 常数级 | 在既有全表锁事务内 |

> **索引限制（critique E8，必须留证）**：判定谓词里的 `LIKE CONCAT(列,'%')` 模式来自列值，MySQL **无法**用它走 `organizations.path` 索引；覆盖人数聚合实际是"每条授权扫一遍 organizations"。`organizations` 通常 <1e3 行，20 条授权即 2e4 次比较，仍在 SC-005 内。实现时 MUST 对覆盖人数聚合与可见性 `EXISTS` 各做一次 `EXPLAIN` 并记录固定 fixtures 的计时，写入 PR；若实测超 SC-005，升级路径为「Go 侧从 `dept.path` 解析祖先 ID 集合 → `g.org_id IN (...)`」（索引友好，对外契约不变）。
