# Data Model: 应用管理员标记与应用内管理入口

**Feature**: `006-app-admin-role` | **Date**: 2026-10-22 | **Plan**: [plan.md](./plan.md)

## 1. 变更总览

| 对象 | 变更 | 说明 |
|---|---|---|
| `client_members` 表 | 新增列 `is_admin TINYINT(1) NOT NULL DEFAULT 0` | 唯一的结构变更；无新增表、无新增索引 |
| `model.ClientMember` | 新增 `IsAdmin bool`（`gorm:"column:is_admin"`） | 与既有 `IsPublic bool` 风格一致 |
| `model.Client` / `clients` 表 | 无变更 | 应用与 OAuth client 是同一实体，不引入「应用」新表 |
| `dto.AppMemberItem` | 新增 `isAdmin bool` | 成员列表（admin 端与门户端同一 handler → 自动同步） |
| `dto.AppItem` | 新增 `isAppAdmin bool` | `GET /api/v1/me/apps` 元素，门户「管理」按钮的唯一判据 |
| `dto.AppManageContext` | 新增结构 | 门户管理页摘要，字段刻意小于 `ClientDetail` |
| Redis | 无变更 | 判定不缓存（plan D4） |

## 2. 迁移 `migrations/0006_app_admin_role.up.sql`

```sql
-- 006-app-admin-role：应用成员新增「应用管理员」标记。
-- 约定：时间列一律 BIGINT（Unix 毫秒）；client_members 主键 (client_id, user_id) 不变。
ALTER TABLE client_members
    ADD COLUMN is_admin TINYINT(1) NOT NULL DEFAULT 0
    COMMENT '1=该应用的应用管理员；授予与取消只在 admin 端' AFTER joined_at;
```

`migrations/0006_app_admin_role.down.sql`

```sql
ALTER TABLE client_members DROP COLUMN is_admin;
```

- 只加列、不改主键、不改既有列类型 → 对 0003 建立的 5 张表与既有数据零影响，`DEFAULT 0` 使存量成员自动为普通成员。
- 迁移执行方式与既有迁移一致：测试与部署均按文件名升序执行 `migrations/*.up.sql`（见 `internal/service/org_integration_test.go`），无需注册表。
- 布尔列用 `TINYINT(1)`，与 `clients.is_public`、`users.status` 的既有习惯保持一致；不使用 MySQL `BOOLEAN` 别名以外的类型。

## 3. 索引与查询评估

| 查询 | SQL 形态 | 索引 |
|---|---|---|
| 判定「是否该应用的管理员」 | `SELECT c.status, m.is_admin FROM clients c LEFT JOIN client_members m ON m.client_id = c.client_id AND m.user_id = ? WHERE c.client_id = ?` | `clients` PK + `client_members` PK `(client_id, user_id)`，单行 |
| 「我可见的应用 + 是否管理员」 | 既有 `ListClientsForMember` 增加 `m.is_admin` 选择列 | 复用 `idx_client_members_user (user_id, client_id)` |
| 设为/取消管理员 | `UPDATE client_members SET is_admin = ? WHERE client_id = ? AND user_id = ?` | PK 命中单行；`RowsAffected = 0` 即「非成员」 |

不新增索引：所有访问都走 `client_members` 的复合主键或 `idx_client_members_user`；「按 `is_admin` 反查用户」这一访问模式在需求中不存在（门户列表已按用户维度取全部成员行）。

## 4. 模型定义

```go
// internal/store/model/app_auth.go
type ClientMember struct {
    ClientID string  `gorm:"column:client_id;primaryKey"`
    UserID   string  `gorm:"column:user_id;primaryKey"`
    AddedBy  *string `gorm:"column:added_by"`
    JoinedAt int64   `gorm:"column:joined_at"`
    IsAdmin  bool    `gorm:"column:is_admin"` // 006：该应用的应用管理员标记
}
```

聚合行类型（`store/app_auth_store.go`）：

```go
// MemberRow 增加 IsAdmin，供成员列表直接渲染身份。
// MemberClientRow 取代 ListClientsForMember 的返回值：model.Client + IsAdmin。
```

## 5. DTO 定义

```go
// AppMemberItem 追加
IsAdmin bool `json:"isAdmin" example:"true"`

// AppItem 追加（门户「管理」按钮判据）
IsAppAdmin bool `json:"isAppAdmin" example:"true"`

// AppManageContext 门户管理页摘要（GET /api/v1/me/apps/:id）
// 刻意不含 redirectUris / scopes / TTL / isPublic / 密钥，保证壳层轻量。
type AppManageContext struct {
    ClientID    string `json:"clientId"`
    Name        string `json:"name"`
    LogoURL     string `json:"logoUrl"`
    Description string `json:"description"`
    Status      int8   `json:"status"`      // 恒为 1（停用应用已在中间件被拒）
    MemberCount int64  `json:"memberCount"`
}

// AppDetail 应用信息页详情（GET /api/v1/me/apps/:id/detail）
// 复用 admin 端 dto.ClientDetail（不含密钥）；只读字段仅用于展示。
// type ClientDetail struct { ... same as admin ... }

// AppUpdateClientRequest 门户可编辑字段白名单（PATCH /api/v1/me/apps/:id）
// 只有这 4 个业务字段 + 版本号；status / postLogoutUris / accessTokenTtl /
// refreshTokenTtl 不在结构体内，请求体携带也会被忽略。
type AppUpdateClientRequest struct {
    ClientName   *string   `json:"clientName"`
    Description  *string   `json:"description"`
    LogoURL      *string   `json:"logoUrl"`
    RedirectURIs *[]string `json:"redirectUris"`
    UpdatedAt    *int64    `json:"updatedAt"` // Unix 毫秒，乐观版本校验
}
```

## 6. 不变量

| ID | 不变量 | 落地方式 |
|---|---|---|
| INV-1 | 同一用户在同一应用只有一条成员关系，其中最多一个 `is_admin` 取值 | 复合主键 `(client_id, user_id)` |
| INV-2 | 移除成员即失去管理员身份 | 删除成员行的既有事务（同时删 `client_role_members`），`is_admin` 随行消失 |
| INV-3 | 成员被重新加入时默认为普通成员 | `AddMembers` 的 `OnConflict{DoNothing}` / `INSERT IGNORE` 不会把旧值带回来；新行取 `DEFAULT 0` |
| INV-4 | 授权写入自动补成员（既有 INV-7）补入的成员 `is_admin = 0` | `ReplaceRoleMembers` 的补入语句不设置该列 |
| INV-5 | 账号停用期间标记保留，不可用 | 标记与 `users.status` 解耦；`Auth` 已对停用账号返回 `AUTH_ACCOUNT_DISABLED` |
| INV-6 | 应用停用期间管理操作全部被拒，标记保留 | `AppAdmin` 中间件校验 `clients.status = 1`（FR-015） |
| INV-7 | 授予/取消管理员幂等 | 单条 `UPDATE ... SET is_admin = ?`；同值重复提交 `RowsAffected = 0` 仍返回 200 与当前值 |
| INV-8 | 取消最后一名管理员允许 | 无「至少保留一名」的约束或触发器；仅前端二次确认（A3） |

## 7. 上线顺序与兼容性

1. 先执行迁移（加列），旧代码不受影响（新列有默认值、旧查询不选择它）。
2. 再发后端（读写 `is_admin`）。旧前端不受影响：`isAdmin` / `isAppAdmin` 是新增字段，`Apps.vue` 未读取时「管理」按钮不会出现。
3. 最后发前端（admin 端身份列 + 门户管理页）。
4. 回滚：先回退前端，再回退后端，最后执行 `0006.down.sql`（丢弃标记，成员关系保留）。
