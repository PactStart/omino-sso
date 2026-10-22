# API 契约: 应用管理员标记与应用内管理入口

**Feature**: `006-app-admin-role` | **Plan**: [../plan.md](../plan.md) | **Date**: 2026-10-22

本文只记录**新增**或**语义变化**的部分；既有 admin 端成员/角色/权限/授权的请求与响应字段不变（见 `AGENTS.md` §4.6 与 `internal/api/dto/dto.go`）。

## 1. 通用约定

| 项 | 约定 |
|---|---|
| 响应格式 | 统一响应 `{code,message,data,traceId}`（`/oauth2/*` 不适用，本特性不涉及） |
| 鉴权（门户） | `middleware.Auth`（cookie `sid`）→ `middleware.AppAdmin`；**不挂** `Admin()` |
| 鉴权（admin） | 既有 `middleware.Auth` → `middleware.Admin()` |
| 拒绝语义 | 门户侧「应用不存在 / 非成员 / 非管理员 / 应用已停用」一律 `403 FORBIDDEN`，文案统一为「无权限访问该应用」；原因只写应用日志（`reason=not_found\|not_member\|not_admin\|client_disabled`） |
| 未登录 / 会话失效 | `401 UNAUTHENTICATED`；账号停用 `403 AUTH_ACCOUNT_DISABLED`（由 `Auth` 给出） |
| 即时生效 | 判定每请求实时查库，无缓存，无「旧身份残留」窗口 |
| 分页 | 与 admin 端一致：`page` 默认 1、`pageSize` 默认 20、上限 100；候选成员 `pageSize` ≤50；`all=true` 走全选 ID 接口（≤5000） |
| 乐观锁 | 写请求可带 `updatedAt`（Unix 毫秒），不匹配 → `409 APP_RESOURCE_STALE` |

## 2. admin 端新增端点

### `PUT /api/v1/admin/clients/:id/members/:uid/admin`

平台管理员把成员设为 / 取消应用管理员。鉴权：`admin`。

请求体：

```json
{ "isAdmin": true }
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `isAdmin` | bool | 是 | `true` 设为应用管理员，`false` 取消 |

响应 `data`：

```json
{ "userId": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A", "isAdmin": true }
```

- **幂等**：提交与当前相同的值仍返回 200 与当前值，不产生重复记录、不写第二条审计（`RowsAffected = 0` 视为无变更）。
- 目标不是该应用成员 → `404 APP_MEMBER_NOT_FOUND`（新增错误码），文案「该用户不是本应用成员，请先添加成员」。
- 审计：`app_member_admin_granted` / `app_member_admin_revoked`，`detail = {clientId, userId, isAdmin:{before,after}}`。
- 应用不存在 → `404 NOT_FOUND`（admin 端维持既有语义，不做隐藏）。
- 允许取消最后一名管理员（A3），服务端不阻止。

### `GET /api/v1/admin/clients/:id/members`（响应字段变化）

`data.items[]` 每条新增：

```json
{ "isAdmin": true }
```

## 3. 门户端新增端点

分组：`/api/v1/me/apps/:id`，中间件 `Auth + AppAdmin`。应用信息页**参考** admin 详情，但可编辑字段只有名称 / 描述 / Logo / 回调地址；**不含**密钥与「设为管理员」入口（口径调整见 spec.md Clarifications 2026-10-22）。

| 方法 | 路径 | 说明 | 对应 admin 端点 |
|---|---|---|---|
| GET | `/api/v1/me/apps/:id` | 管理页摘要（context） | 新增（等价于 `GET /admin/clients/:id` 的裁剪版） |
| GET | `/api/v1/me/apps/:id/detail` | 应用信息页详情（字段同 admin 详情，不含密钥） | `GET /admin/clients/:id` |
| PATCH | `/api/v1/me/apps/:id` | 更新基础信息（仅 `clientName`/`description`/`logoUrl`/`redirectUris`） | `PATCH /admin/clients/:id`（缩减版） |
| POST | `/api/v1/me/apps/:id/logo` | 设置应用 Logo（直传确认） | `POST /admin/clients/:id/logo` |
| DELETE | `/api/v1/me/apps/:id/logo` | 清除应用 Logo（幂等） | `DELETE /admin/clients/:id/logo` |
| GET | `/api/v1/me/apps/:id/members` | 成员列表（支持 `keyword`/`deptId`/`page`/`pageSize`/`all`） | `GET /admin/clients/:id/members` |
| GET | `/api/v1/me/apps/:id/members/candidates` | 候选成员 | `GET .../members/candidates` |
| POST | `/api/v1/me/apps/:id/members` | 添加成员（`userIds` 或 `deptId` 二选一） | `POST .../members` |
| POST | `/api/v1/me/apps/:id/members/remove` | 移除成员 | `POST .../members/remove` |
| GET | `/api/v1/me/apps/:id/roles` | 角色列表 | `GET .../roles` |
| POST | `/api/v1/me/apps/:id/roles` | 新建角色 | `POST .../roles` |
| GET | `/api/v1/me/apps/:id/roles/:rid` | 角色详情（含两类绑定） | `GET .../roles/:rid` |
| PATCH | `/api/v1/me/apps/:id/roles/:rid` | 修改角色 | `PATCH .../roles/:rid` |
| DELETE | `/api/v1/me/apps/:id/roles/:rid` | 删除角色 | `DELETE .../roles/:rid` |
| PUT | `/api/v1/me/apps/:id/roles/:rid/permissions` | 设置角色权限（覆盖） | `PUT .../roles/:rid/permissions` |
| PUT | `/api/v1/me/apps/:id/roles/:rid/members` | 设置角色成员（覆盖） | `PUT .../roles/:rid/members` |
| GET | `/api/v1/me/apps/:id/permissions` | 权限列表（扁平，前端组树） | `GET .../permissions` |
| POST | `/api/v1/me/apps/:id/permissions` | 新建权限 | `POST .../permissions` |
| PATCH | `/api/v1/me/apps/:id/permissions/:pid` | 修改权限（`parentId` 可置 null） | `PATCH .../permissions/:pid` |
| DELETE | `/api/v1/me/apps/:id/permissions/:pid` | 删除权限 | `DELETE .../permissions/:pid` |

> 未注册的路径一律落入 `NoRoute` → `404 NOT_FOUND`。特别是 `PUT /api/v1/me/apps/:id/members/:uid/admin` **不存在**（FR-007），直接请求即 404。

### 3.1 `GET /api/v1/me/apps/:id`

```json
{
  "code": "OK",
  "data": {
    "clientId": "demo-app",
    "name": "验收应用",
    "logoUrl": "https://cdn.example.com/logo/demo.png",
    "description": "内部验收用",
    "status": 1,
    "memberCount": 12
  }
}
```

- 字段刻意小于 `ClientDetail`：不含 `redirectUris` / `postLogoutUris` / `scopes` / `grantTypes` / `accessTokenTtl` / `refreshTokenTtl` / `isPublic` / 密钥。
- `status` 恒为 1（停用应用已由 `AppAdmin` 拒绝）。

### 3.1a `GET /api/v1/me/apps/:id/detail`

响应 `data` 字段与 admin 端 `ClientDetail` 一致：`clientId`、`clientName`、`description`、`logoUrl`、`redirectUris[]`、`postLogoutUris[]`、`grantTypes[]`、`scopes[]`、`isPublic`、`accessTokenTtl`、`refreshTokenTtl`、`status`、`createdAt`、`updatedAt`（毫秒，兼作版本）、`memberCount`；**不含任何密钥**。

### 3.1b `PATCH /api/v1/me/apps/:id`

请求体白名单（缺失=不变）：

```json
{
  "clientName": "订单中心",
  "description": "内部订单系统",
  "logoUrl": "https://cdn.example.com/logo/order.png",
  "redirectUris": ["https://order.example.com/cb"],
  "updatedAt": 1792540000000
}
```

- **只接受上述 4 个业务字段**：`postLogoutUris` / `accessTokenTtl` / `refreshTokenTtl` / `status` 即使出现在请求体也被忽略（`ClientService.UpdateBasicInfo` 统一清空），保证门户无法改动只读配置。
- 响应同 `GET /api/v1/me/apps/:id/detail`；版本不符 `409 APP_RESOURCE_STALE`。
- 审计：`client_updated`，`detail = {clientId, changed[], redirectUris:{before,after}}`（与 admin 端口径一致）。

### 3.1c `POST|DELETE /api/v1/me/apps/:id/logo`

- `POST` 请求体 `{"key","format","size"}`，`scene` 固定 `app_logo`、`targetId` 取路径 `:id`；成功后回写该应用 `logo_url`，响应为文件记录 `dto.FileItem`。
- `DELETE` 删除对象并清空 `logo_url`；无活跃 Logo 时幂等成功（`data: null`）。

### 3.2 请求体与响应

与 admin 端**逐字段相同**（同一 handler 处理）：

- 添加成员 `{"userIds":["..."]}` 或 `{"deptId":"...","includeSubDepts":true}`
- 移除成员 `{"userIds":["..."]}`
- 新建角色 `{"name":"opsAdmin","description":"运维管理员"}`
- 修改角色 `{"name":"opsAdmin2","description":null,"updatedAt":1792540000000}`
- 新建权限 `{"code":"order:read","name":"订单读取","description":"...","parentId":null,"sort":0}`
- 设置角色权限 `{"permissionIds":["..."],"updatedAt":1792540000000}`
- 设置角色成员 `{"userIds":["..."],"updatedAt":1792540000000}`

成员列表元素新增 `isAdmin`（同一 DTO，admin 端与门户端一致）：

```json
{
  "userId": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
  "username": "zhangsan",
  "displayName": "张三",
  "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
  "deptName": "技术中心",
  "status": 1,
  "joinedAt": "2026-10-20T10:00:00Z",
  "roleCount": 2,
  "isAdmin": true
}
```

### 3.3 `GET /api/v1/me/apps`（响应字段变化）

元素新增 `isAppAdmin`，其余不变：

```json
{
  "clientId": "demo-app",
  "name": "验收应用",
  "description": "内部验收用",
  "logoUrl": "https://cdn.example.com/logo/demo.png",
  "homeUrl": "https://demo.example.com",
  "lastAccessAt": "2026-10-22T02:11:05Z",
  "isAppAdmin": true
}
```

`isAppAdmin` 只来自 `client_members.is_admin`，**不**因平台 `admin` 角色为 true（A2 / US2-AC5）。

## 4. 门户端只读组织树

### `GET /api/v1/me/orgs/tree`

鉴权：`session`（任意登录用户）。用途：应用管理页的部门筛选与「按部门加入」。

- 响应与 `GET /api/v1/admin/orgs/tree` 完全一致（同一 handler / 同一 `OrgService` 读路径）。
- **只读**：门户侧不注册任何组织写端点（创建/改名/移动/删除仍仅 admin）。

## 5. 错误码增量

| HTTP | code | 含义 | 登记位置 |
|---|---|---|---|
| 404 | `APP_MEMBER_NOT_FOUND` | 目标用户不是该应用成员（`PUT /admin/clients/:id/members/:uid/admin`） | `AGENTS.md` §5.3、`internal/api/response/errors.go` |

其余全部复用既有码：`FORBIDDEN`、`UNAUTHENTICATED`、`AUTH_ACCOUNT_DISABLED`、`VALIDATION_FAILED`、`APP_ROLE_NAME_EXISTS`、`APP_PERMISSION_CODE_EXISTS`、`APP_PERMISSION_HAS_CHILDREN`、`APP_PERMISSION_INVALID_PARENT`、`APP_RESOURCE_STALE`、`NOT_FOUND`、`RATE_LIMITED`、`INTERNAL_ERROR`。

## 6. 审计事件增量

| 事件 | 触发 | `detail` | 登记位置 |
|---|---|---|---|
| `app_member_admin_granted` | admin 端把成员设为管理员（值发生变化） | `{clientId, userId, isAdmin:{before:false,after:true}}` | `AGENTS.md` §6.7、`store/model/audit_event.go` |
| `app_member_admin_revoked` | admin 端取消管理员（值发生变化） | `{clientId, userId, isAdmin:{before:true,after:false}}` | 同上 |

门户侧的全部管理操作沿用既有事件（`app_member_added` / `app_member_removed` / `app_role_*` / `app_permission_*` / `app_grant_updated`），因为走同一 handler；`user_id` 字段记录的就是当次操作者（应用管理员本人），与 admin 端口径一致（FR-014）。

## 7. 门户与 admin 端的差异清单

| 能力 | admin 端 | 门户端 |
|---|---|---|
| 成员列表 / 候选 / 添加 / 移除 | ✅ | ✅ |
| 角色 CRUD 与双向绑定、权限 CRUD | ✅ | ✅ |
| 应用信息：名称 / 描述 / Logo / 回调地址 | ✅ | ✅（同样可改） |
| 应用信息：登出地址 / TTL / 启停 | ✅（可改） | 只读（提交也被忽略） |
| 密钥轮换 | ✅ | ❌（不注册） |
| 设为/取消应用管理员 | ✅ | ❌（不注册，FR-007） |
| 组织写操作 | ✅ | ❌（仅只读树） |
| 审计日志、发送记录、文件管理 | ✅ | ❌ |
| 鉴权 | 平台 `admin` 角色 | 该应用 `is_admin`（平台管理员不自动获得） |
