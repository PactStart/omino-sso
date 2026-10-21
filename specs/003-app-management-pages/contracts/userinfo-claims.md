# Contract: 用户信息接口（`GET /oauth2/userinfo`）

**Feature**: `003-app-management-pages` | **Server**: `:8080`（RFC 原生报文，**不得**套统一响应） | **Auth**: `Authorization: Bearer <access_token>`

## 变更性质

**仅新增两个字段**：`app_roles`、`permissions`。既有字段（`sub`、`name`、`preferred_username`、`email`、`email_verified`、`picture`、`roles`、`tenant`、`dept_id`）的名称、类型与语义全部不变；错误行为（无效令牌 → 401、缺失令牌 → 401）不变。

```json
{
  "sub": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
  "name": "张三",
  "preferred_username": "zhangsan",
  "email": "zhangsan@example.com",
  "email_verified": true,
  "roles": ["user"],
  "tenant": "default",
  "dept_id": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
  "app_roles": ["opsAdmin", "viewer"],
  "permissions": ["order:read", "order:write"]
}
```

## 字段规则

| 字段 | 类型 | 规则 |
|---|---|---|
| `roles` | `string[]` | **平台角色**，取自 `users.roles`，语义冻结（research R1） |
| `app_roles` | `string[]` | 该用户在**当前令牌所属应用**内的角色名；无角色时为 `[]`，**不得**为 `null` 或省略 |
| `permissions` | `string[]` | 上述角色所绑定的权限编码**去重并集**；无权限时为 `[]` |

## 与既有概念的区别（接入方必读）

| 字段 | 归属 | 数据来源 | 典型值 | 用途 |
|---|---|---|---|---|
| `roles` | **平台角色** | `users.roles`，跨应用统一 | `["admin"]` | 平台内部权限判定，语义冻结 |
| `app_roles` | **应用内角色** | `client_roles.name`，仅在令牌所属应用内有效 | `["opsAdmin"]` | 接入方做应用内功能鉴权 |

- 两者**互不覆盖、互不推导**：应用内角色不会出现在 `roles` 里，平台管理员也不会因为 `admin` 而自动拥有任何 `app_roles`。管理后台"应用角色"页也标注同一句说明（FR-036）。
- `permissions` 与应用 `scopes` **无映射关系**：scope 决定"这次授权能拿到哪些 claim"，`permissions` 描述"该用户在本应用内能做什么"。接入方 MUST NOT 用 scope 推导 `permissions`，也 MUST NOT 用 `permissions` 反推授权范围（FR-020 的编码字符集允许 `:`、`.`，仅为可读性，不表示 scope 命名空间）。
- `app_roles` 与 `permissions` 只在 userinfo 出现，**不进 id_token**（research R6）。

## 应用上下文的确定方式

1. 从 Bearer 令牌解析出 `tokenID`（`jti`）与 `sub`（由 `op.Storage.SetUserinfoFromToken` 提供）。
2. 用 `tokenID` 查 `access_tokens` 得到 `client_id`（research R2；该行在签发时写入，TTL 与 `expires_at` 一致）。
3. 以 `(client_id, sub)` 查询：
   - `app_roles` = `client_roles.name`（经 `client_role_members` 关联），按 `name` 升序；
   - `permissions` = `client_permissions.code`（经 `client_role_permissions` → `client_role_members` 关联），去重后按 `code` 升序。
4. 查不到映射、用户非成员、或无任何绑定 → 两个字段均为空数组，**不报错**。
5. 可观测性：`access_tokens` 无该 `tokenID`（映射缺失）与"映射存在但用户已非成员"两种情形 MUST 各打一条 `slog.Warn("userinfo app context miss", "token_id", …, "sub", …, "client_id", …)`——这是唯一的失效信号（E10）；"用户是成员但无任何角色绑定"属正常状态，**不打日志**，避免噪声。

## 一致性要求

- **即时生效**（FR-030）：成员移除、角色/权限解绑、角色改名后，下一次调用必须反映最新结果；实现不缓存（每次 userinfo 两次索引查询）。
- **应用隔离**（FR-028）：同一用户用不同应用的令牌调用，结果各自独立。
- **不进入 id_token**（research R6）：`id_token` 保持现状，避免放大令牌体积与影响既有接入方。

## 验收映射

| 场景 | 期望 |
|---|---|
| 成员 + 1 个角色（2 权限） | `app_roles` 1 项；`permissions` 2 项 |
| 成员 + 2 个角色（权限有交集） | `app_roles` 2 项；`permissions` 为去重后的并集 |
| 成员但无角色 | 两个字段均为 `[]` |
| 非成员（历史令牌未过期） | 两个字段均为 `[]`（不以角色绑定兜底） |
| 移除角色后立即再调用 | 该角色及其独有权限不再出现（SC-003） |
| 令牌无 `access_tokens` 记录（如历史令牌） | 两个字段均为 `[]`，其余字段正常返回 |
