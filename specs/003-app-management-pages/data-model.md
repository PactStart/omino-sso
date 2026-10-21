# Data Model: 应用详情子页面与应用内授权体系

**Feature**: `003-app-management-pages` | **Date**: 2026-10-21 | **Plan**: [plan.md](./plan.md)

全部新表由 `migrations/0003_app_auth.up.sql` 创建、`0003_app_auth.down.sql` 逆序删除。约定沿用 `AGENTS.md` §6：`utf8mb4`；时间列为 `BIGINT` Unix 毫秒且以 `_at` 结尾；主键 `CHAR(26)` ULID；大小写敏感列显式 `COLLATE utf8mb4_0900_as_cs`；**无软删除**。

## 1. `client_members`（应用成员）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `client_id` | VARCHAR(64) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | 逻辑外键 → `clients.client_id` |
| `user_id` | CHAR(26) | NOT NULL | 逻辑外键 → `users.id` |
| `added_by` | CHAR(26) | NULL | 操作者；系统自动补齐时为 NULL |
| `joined_at` | BIGINT | NOT NULL | 加入时间（毫秒） |

- `PRIMARY KEY (client_id, user_id)`
- `KEY idx_client_members_user (user_id, client_id)` —— 支撑门户"我的应用"反查
- 说明：成员关系是门户可见性的唯一依据；不含角色信息（角色在 `client_role_members`）。

## 2. `client_roles`（应用角色）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `client_id` | VARCHAR(64) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | |
| `name` | VARCHAR(32) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | 仅字母数字，1–32 |
| `description` | VARCHAR(100) | NULL | ≤100 字符 |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | 兼作并发编辑的版本比较依据 |

- `UNIQUE KEY uk_client_roles_name (client_id, name)` —— 应用内唯一、区分大小写
- `KEY idx_client_roles_client (client_id, created_at)`

## 3. `client_permissions`（应用权限点）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `client_id` | VARCHAR(64) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | |
| `code` | VARCHAR(64) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | 权限编码，1–64，区分大小写 |
| `name` | VARCHAR(64) | NOT NULL | 展示名 |
| `description` | VARCHAR(255) | NULL | |
| `parent_id` | CHAR(26) | NULL | 自引用，NULL 为根 |
| `sort` | INT | NOT NULL DEFAULT 0 | 同级排序 |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

- `UNIQUE KEY uk_client_permissions_code (client_id, code)`
- `KEY idx_client_permissions_parent (client_id, parent_id, sort)`
- 无物化路径：树在 service 内由 `parent_id` 组装（research R11/Complexity Tracking）。

## 4. `client_role_permissions`（角色 ↔ 权限）

| 字段 | 类型 | 约束 |
|---|---|---|
| `role_id` | CHAR(26) | NOT NULL |
| `permission_id` | CHAR(26) | NOT NULL |
| `created_at` | BIGINT | NOT NULL |

- `PRIMARY KEY (role_id, permission_id)`
- `KEY idx_role_permissions_perm (permission_id)` —— 支撑"删除权限时统计/解除引用"

## 5. `client_role_members`（角色 ↔ 成员）

| 字段 | 类型 | 约束 |
|---|---|---|
| `role_id` | CHAR(26) | NOT NULL |
| `user_id` | CHAR(26) | NOT NULL |
| `created_at` | BIGINT | NOT NULL |

- `PRIMARY KEY (role_id, user_id)`
- `KEY idx_role_members_user (user_id)` —— 支撑 userinfo 快照与成员移除时级联

## 6. `access_tokens`（访问令牌 → 应用映射）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `token_id` | CHAR(26) | PK | `op.CreateAccessToken` 返回的标识（即 JWT 的 `jti`） |
| `user_id` | CHAR(26) | NOT NULL | |
| `client_id` | VARCHAR(64) | NOT NULL, `COLLATE utf8mb4_0900_as_cs` | 令牌所属应用 |
| `expires_at` | BIGINT | NOT NULL | 毫秒；`KEY idx_access_tokens_exp (expires_at)` |
| `created_at` | BIGINT | NOT NULL | |

- 仅用于 userinfo 判定"当前应用"（research R2）；不存令牌明文，不承担吊销职责。
- 写入失败即签发失败（不降级，research R2）。
- 清理：签发入口按 `expires_at < now` 限量删除 `LIMIT 500`，且进程内限频（≥1 分钟一次），避免长期未清理后的首次签发被大批删除拖慢（research R12）。

## 不变量（Invariants）

| ID | 不变量 | 保障方式 |
|---|---|---|
| INV-1 | 同一应用内角色名唯一且区分大小写 | `uk_client_roles_name`，冲突码 `1062` → `APP_ROLE_NAME_EXISTS` |
| INV-2 | 同一应用内权限编码唯一且区分大小写 | `uk_client_permissions_code` → `APP_PERMISSION_CODE_EXISTS` |
| INV-3 | 角色名只含字母数字、1–32；描述 ≤100 | service 校验（`^[A-Za-z0-9]{1,32}$`），失败 → `VALIDATION_FAILED` |
| INV-4 | 权限编码只含 `[A-Za-z0-9_.:-]`、1–64 | service 校验，失败 → `VALIDATION_FAILED` |
| INV-5 | 权限父级必须同应用、不得为自身或后代 | service 内向上遍历父链（步数上限 100），失败 → `APP_PERMISSION_INVALID_PARENT` |
| INV-6 | 删除权限时不得有子权限 | `COUNT(*) WHERE parent_id=?` > 0 → `APP_PERMISSION_HAS_CHILDREN` |
| INV-7 | **有角色必为成员**：写入 `client_role_members` 前确保 `client_members` 存在记录 | 同一事务内 `INSERT IGNORE` 成员 + 插入绑定 |
| INV-8 | 移除成员必须同时解除其全部角色绑定 | 同一事务内先删 `client_role_members` 再删 `client_members`（或反向但同事务） |
| INV-9 | 门户可见 ⇔ 是成员 ∧ 应用 `status=1` | `ListForUser` 的 join 条件 |
| INV-10 | userinfo 的 `app_roles`/`permissions` 只反映当前令牌所属应用 | `access_tokens` 反查 + 按 `client_id` 过滤 |
| INV-11 | 并发编辑不静默覆盖 | 事务内 `SELECT ... FOR UPDATE` 读 `updated_at` 与请求值比对，不符 → `APP_RESOURCE_STALE`（research R13） |
| INV-12 | 批量写入有硬上限 | 增量（成员添加/移除）`userIds` ≤500，前端分片；覆盖式（授权）`userIds` ≤5,000、`permissionIds` ≤1,000，**不分片**；`all=true` 的 ID 列表 ≤5,000 |

## 关键查询

**门户"我的应用"**（替换 `ListClients` 全量返回）：

```sql
SELECT c.* FROM clients c
JOIN client_members m ON m.client_id = c.client_id
WHERE m.user_id = ? AND c.status = 1
ORDER BY c.created_at ASC;
```

**候选成员**（排除已是成员者，支持用户名/姓名/部门子树）：

```sql
SELECT u.id, u.username, u.display_name, u.dept_id
FROM users u
JOIN organizations o ON o.id = u.dept_id
WHERE u.status = 1
  AND NOT EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = ? AND m.user_id = u.id)
  AND (? = '' OR u.username LIKE CONCAT(?, '%') OR u.display_name LIKE CONCAT('%', ?, '%'))
  AND (? = '' OR o.path LIKE CONCAT((SELECT path FROM organizations WHERE id = ?), '%'))
ORDER BY u.username ASC
LIMIT ? OFFSET ?;
```

> 排序键必须是 `u.username`（`users` 上的唯一索引 `uk_users_username`，天然有序、稳定分页），**不得**用 `u.created_at`（无索引，会触发文件排序，E5）。`display_name` 的包含匹配无法走索引，因此候选接口 `pageSize ≤ 50`、`keyword ≤ 64` 字符。
> `all=true`（跨页全选）返回 ID 列表时上限 **5,000**，超出返回 `400 VALIDATION_FAILED`（E6）。

**应用列表的成员数**（`GET /admin/clients` 的 `memberCount` / `hasMembers`）：

```sql
SELECT c.*,
       (SELECT COUNT(*) FROM client_members m WHERE m.client_id = c.client_id) AS member_count
FROM clients c
WHERE 1 = 1
  -- 仅当 hasMembers=false 时追加；true 时改 EXISTS，缺省不加条件
  AND NOT EXISTS (SELECT 1 FROM client_members m2 WHERE m2.client_id = c.client_id)
ORDER BY c.created_at ASC
LIMIT ? OFFSET ?;
```

> `client_members` 主键为 `(client_id, user_id)`，计数子查询走主键前缀，无需额外索引；`hasMembers` 用 `EXISTS` / `NOT EXISTS` 短路。

**userinfo 快照**（一次取角色名，一次取权限编码，均在应用内）：

```sql
SELECT r.name FROM client_roles r
JOIN client_role_members rm ON rm.role_id = r.id
WHERE r.client_id = ? AND rm.user_id = ?;

SELECT DISTINCT p.code FROM client_permissions p
JOIN client_role_permissions rp ON rp.permission_id = p.id
JOIN client_role_members rm ON rm.role_id = rp.role_id
WHERE p.client_id = ? AND rm.user_id = ?;
```

## 错误映射

| 触发 | HTTP | code | 说明 |
|---|---|---|---|
| 角色名重复（1062 on `uk_client_roles_name`） | 409 | `APP_ROLE_NAME_EXISTS` | message 固定"该应用下已存在同名角色" |
| 权限编码重复（1062 on `uk_client_permissions_code`） | 409 | `APP_PERMISSION_CODE_EXISTS` | "该应用下已存在相同权限编码" |
| 删除仍有子权限的权限 | 409 | `APP_PERMISSION_HAS_CHILDREN` | "仍有 N 个子权限" |
| 父级为自身/后代/跨应用 | 400 | `APP_PERMISSION_INVALID_PARENT` | "上级权限不存在或不可作为上级" |
| 提交的 `updatedAt` 与服务端不一致 | 409 | `APP_RESOURCE_STALE` | "已被他人修改，请刷新后重试" |
| 角色名/编码格式、描述超长、TTL 越界、URL 非法、批量超上限（500 / 5,000 / 1,000） | 400 | `VALIDATION_FAILED` | 字段级 message |
| 应用/角色/权限不存在 | 404 | `NOT_FOUND` | |
| 非管理员 | 403 | `FORBIDDEN` | 既有中间件 |
| 未登录 | 401 | `UNAUTHENTICATED` | 既有中间件 |

## 与既有模型的关系

- `clients` 表结构**不变**；`PATCH /admin/clients/:id` 只更新既有列（`client_name`、`description`、`logo_url`、`redirect_uris`、`post_logout_uris`、`access_token_ttl`、`refresh_token_ttl`、`status`、`updated_at`）。
- `users` 表结构**不变**；成员与角色绑定通过 `user_id` 逻辑外键关联，不做数据库外键（与全库既有约定一致）。
- 删除用户 = `status=0`（既有约定），因此**不触发**成员级联；成员列表对 `status!=1` 的用户标记"已禁用"（FR-014）。真正的物理删除在当前系统中不存在，`ON DELETE` 级联不需要；若未来引入物理删除，删除路径 MUST 在同一事务内清理 `client_members` 与 `client_role_members`（记入 plan 的后续工作）。
