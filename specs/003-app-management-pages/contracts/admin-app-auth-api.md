# Contract: 管理端应用详情与授权接口

**Feature**: `003-app-management-pages` | **Base**: `:8081` `/api/v1` | **Auth**: `sid` cookie + admin 角色

统一响应：`{code, message, data, traceId}`（`AGENTS.md` §5）。下表 17 个操作新增后，管理 API 由 **16 条 path / 19 个操作** 变为 **26 条 path / 36 个操作**。

| # | 方法 | 路径 | 说明 |
|---|---|---|---|
| 1 | GET | `/admin/clients/:id` | 单应用详情（子页面刷新直达） |
| 2 | PATCH | `/admin/clients/:id` | 更新应用可编辑字段 |
| 3 | GET | `/admin/clients/:id/members` | 成员分页列表；`all=true` 时返回全部 userId |
| 4 | POST | `/admin/clients/:id/members` | 批量添加成员（幂等） |
| 5 | POST | `/admin/clients/:id/members/remove` | 批量移除成员（级联解除角色绑定） |
| 6 | GET | `/admin/clients/:id/members/candidates` | 候选用户（排除已是成员者） |
| 7 | GET | `/admin/clients/:id/roles` | 角色列表（含权限数/成员数） |
| 8 | POST | `/admin/clients/:id/roles` | 新建角色 |
| 9 | GET | `/admin/clients/:id/roles/:rid` | 角色详情（含 permissionIds / memberIds） |
| 10 | PATCH | `/admin/clients/:id/roles/:rid` | 改角色名/描述 |
| 11 | DELETE | `/admin/clients/:id/roles/:rid` | 删除角色（连带解除两类绑定） |
| 12 | PUT | `/admin/clients/:id/roles/:rid/permissions` | 覆盖式设置角色权限 |
| 13 | PUT | `/admin/clients/:id/roles/:rid/members` | 覆盖式设置角色成员（自动补成员） |
| 14 | GET | `/admin/clients/:id/permissions` | 权限扁平列表（前端自行组树） |
| 15 | POST | `/admin/clients/:id/permissions` | 新建权限 |
| 16 | PATCH | `/admin/clients/:id/permissions/:pid` | 修改权限 |
| 17 | DELETE | `/admin/clients/:id/permissions/:pid` | 删除权限 |

> `:id` 为 `client_id`（大小写敏感，精确匹配）。所有路径都先校验应用存在，不存在 → `404 NOT_FOUND`。
>
> **并发编辑**：凡带 `updatedAt` 的请求体（第 2、10、12、13、16 号操作）都做乐观版本校验——与服务端当前 `updated_at` 不一致 → `409 APP_RESOURCE_STALE`（"已被他人修改，请刷新后重试"）。`updatedAt` 缺省表示不校验（兼容脚本调用）。两类授权请求的版本取**角色**的 `updatedAt`。
>
> **path 数说明**：第 1 与第 2 号共用 `/admin/clients/:id`，其余按 path 归并后共 **10 条新 path**；操作 17 个 → 19 + 17 = **36 个操作**。

## 0. `GET /admin/clients`（既有接口，纯新增字段）

该接口由 002 提供，本特性只做加法，**不增 path、不改既有字段**：

| 字段 / 参数 | 位置 | 规则 |
|---|---|---|
| `memberCount` | 响应（列表每项） | 该应用的成员总数，`client_members` 分组计数子查询；无成员为 `0` |
| `hasMembers` | Query | 仅接受 `true` / `false`；`false` 时只返回 `memberCount = 0` 的应用（FR-032，上线前定位待补员应用） |

> 前端"应用"列表展示成员数并提供"仅看无成员"开关；`hasMembers` 与 `members?all=true` 同时使用时，`all=true` 的 5,000 上限仍生效。

## 1. `GET /admin/clients/:id`

子页面刷新直达与详情回填的唯一取数入口（FR-002 / FR-003）。

响应 `data` 为 `ClientDetail`：

```json
{"clientId":"demo-app","clientName":"验收应用","description":"…","logoUrl":null,
 "redirectUris":["https://demo.example.com/cb"],"postLogoutUris":[],
 "grantTypes":["authorization_code","refresh_token"],"scopes":["openid","profile"],
 "isPublic":false,"accessTokenTtl":900,"refreshTokenTtl":2592000,"status":1,
 "createdAt":1792540000000,"updatedAt":1792540000000,"memberCount":12}
```

- **不含** `client_secret_hash`；密钥无任何读取入口（FR-005）。
- 不存在 → `404 NOT_FOUND`；非 admin → `403 FORBIDDEN`；未登录 → `401 UNAUTHENTICATED`。
- 与 `PATCH` 共用 `/admin/clients/:id` 这条 path，**不增加 path 数**。

## 2. `PATCH /admin/clients/:id`

请求（全部可选，缺失即不变；`*` 表示"显式 null 表示清空"仅用于可空文本）：

```json
{
  "clientName": "订单中心",
  "description": "订单相关自助服务",
  "logoUrl": "https://cdn.example.com/logo/order.png",
  "redirectUris": ["https://order.example.com/callback"],
  "postLogoutUris": ["https://order.example.com/"],
  "accessTokenTtl": 1800,
  "refreshTokenTtl": 2592000,
  "status": 1,
  "updatedAt": 1792540000000
}
```

| 字段 | 约束 |
|---|---|
| `clientName` | trim 后 1–128 |
| `description` | ≤255，可空 |
| `logoUrl` | `http(s)` 绝对地址，≤512，可空 |
| `redirectUris` | 1–10 条，每条 `http(s)` 绝对地址、≤512、去重 |
| `postLogoutUris` | 0–10 条，同上 |
| `accessTokenTtl` | 60–86400 秒 |
| `refreshTokenTtl` | 300–31536000 秒 |
| `status` | 0 或 1；改为 0（停用）时前端 MUST 二次确认并提示"停用后全员在门户不可见，成员与角色配置保留" |
| `updatedAt` | 可选，读取时的 `updated_at`（毫秒）；与服务端不一致 → `409 APP_RESOURCE_STALE` |

响应 `data` 为更新后的 `ClientDetail`（**不含** `client_secret_hash`）。有效期变更提示文案由前端固定展示：「仅对此后签发的令牌生效」。

错误：`400 VALIDATION_FAILED`、`404 NOT_FOUND`、`409 APP_RESOURCE_STALE`。审计事件 `client_updated`：`changed[]` 记字段名；`redirectUris` / `postLogoutUris` 额外记 `before` / `after` 数组。

## 3. `GET /admin/clients/:id/members`

Query：`keyword`（`username` 前缀匹配 + `display_name` 包含匹配，长度 ≤64）、`deptId`（含子部门）、`page`（默认 1）、`pageSize`（默认 20，最大 200）、`all`（`true` 时忽略分页）。

```json
{
  "list": [
    {"userId":"01J…","username":"zhangsan","displayName":"张三","deptId":"01J…","deptName":"技术中心",
     "status":1,"joinedAt":1792540000000,"roleCount":2}
  ],
  "total": 137
}
```

排序固定为 `u.username` 升序（唯一索引，稳定且不触发文件排序）。

`all=true` 时返回 `{"userIds":["01J…","01J…"]}`（用于跨分页全选/反选）。**上限 5,000**：筛选结果超过 5,000 时返回 `400 VALIDATION_FAILED`，message「结果过多，请先按部门或关键字缩小范围」。

## 4. `POST /admin/clients/:id/members`

两种互斥的入参形态（同时提交 → `400 VALIDATION_FAILED`）：

```json
{"userIds": ["01J…", "01J…"]}
```

- `userIds` 去重后 1–500 个；空数组 → `400 VALIDATION_FAILED`；**超过 500 → `400 VALIDATION_FAILED`**，message 明确「单次最多 500 个，请分片提交」。前端"全选全部"（可能上万）MUST 按 500 分片顺序提交并汇总计数，不得把限制留给调用方猜测。
- 已存在成员不报错、不重复插入；用户不存在或已停用 → 计入 `skipped`。

```json
{"deptId": "01J…", "includeSubDepts": true}
```

- 按部门整批加入（FR-033）：`deptId` 必须存在（否则 `400 VALIDATION_FAILED`），`includeSubDepts` 默认 `true`。
- 服务端用一条 `INSERT IGNORE ... SELECT` 完成，不构造参数列表，**因此不受 500 上限约束**。
- 结果按"新增/已存在/已停用"分类计数：`{"added":4,"existing":100,"disabled":2}`（停用账号跳过，不写入）。

响应（`userIds` 形态）：`{"code":"OK","message":"success","data":{"added":4,"skipped":1},"traceId":"01J…"}`。

## 5. `POST /admin/clients/:id/members/remove`

```json
{"userIds": ["01J…"]}
```

- `userIds` 去重后 1–500 个；超过 500 → `400 VALIDATION_FAILED`（同样要求分片提交）。
- 同一事务内：统计并删除 `client_role_members`，再删除 `client_members`。
- 非成员 ID 不报错，计入 `notMember`。

```json
{"removed":3,"roleBindingsRemoved":7,"notMember":1}
```

## 6. `GET /admin/clients/:id/members/candidates`

Query 同上（`keyword` 对 `username` 前缀匹配、`display_name` 包含匹配，长度 ≤64；`deptId` 含子部门），返回结构同成员列表，但元素为 `{userId, username, displayName, deptId, deptName}`，且**不含**已是成员的用户。`pageSize` 最大 50（`display_name` 无索引，包含匹配用不到索引，必须用小页限制扫描量）。`all=true` 返回当前筛选下的全部候选 `userIds`，同样受 **5,000 上限**约束（超出 → `400 VALIDATION_FAILED`）。

> 候选查询用 `NOT EXISTS` 反连接排除已成员，`ORDER BY u.username`（唯一索引）——不使用 `u.created_at`（无索引，会退化为文件排序）。

## 7–11. 角色

`GET /roles` 返回：

```json
{"list":[{"id":"01J…","name":"opsAdmin","description":"运维管理员",
          "permissionCount":3,"memberCount":5,
          "createdAt":1792540000000,"updatedAt":1792540000000}],"total":20}
```

`POST /roles` 请求 `{"name":"opsAdmin","description":"运维管理员"}`：

- `name` 必须匹配 `^[A-Za-z0-9]{1,32}$`，应用内唯一（区分大小写）→ 冲突 `409 APP_ROLE_NAME_EXISTS`；格式错误 `400 VALIDATION_FAILED`。
- `description` ≤100 字符，可空。
- 响应 `201` + `RoleItem`。

`GET /roles/:rid` 返回 `RoleItem` + `permissionIds: []string` + `memberIds: []string`。

`PATCH /roles/:rid` 请求同 `POST`（字段可选），另接受可选 `updatedAt`；`name` 冲突同样 `409 APP_ROLE_NAME_EXISTS`，版本不符 `409 APP_RESOURCE_STALE`。

`DELETE /roles/:rid`：同一事务内删除两类绑定与角色本身；响应 `data:null`。审计 `app_role_deleted` 的 detail MUST 记录被解除绑定的快照 `removedPermissionIds` / `removedUserIds`（各超 20 项时记数量 + 前 20 项），使误删后能按审计手工重建。

## 12–13. 授权（覆盖式）

`PUT /roles/:rid/permissions`

```json
{"permissionIds": ["01J…","01J…"], "updatedAt": 1792540000000}
```

- `permissionIds` 可为空数组（= 清空），去重后最多 1,000 个（与权限列表整体上限一致）。每个 ID 必须属于同一应用，否则 `400 VALIDATION_FAILED`。
- `updatedAt` 为角色版本；不符 → `409 APP_RESOURCE_STALE`。
- 事务内：删除差集、插入新增；返回 `{"permissionIds":[…],"added":2,"removed":1}`。

`PUT /roles/:rid/members`

```json
{"userIds": ["01J…"], "updatedAt": 1792540000000}
```

- `userIds` 去重后 **0–5,000**；超过 → `400 VALIDATION_FAILED`。此处**不做 500 分片**：角色成员实际规模为数十到数百，一次整体提交即可；且该接口是整体覆盖（提交最终态），分片会互相覆盖、破坏语义。5,000 仅为防御性边界（与成员/候选的 `all=true` 上限一致），不构成日常使用约束，前端全选拿到的 ID 集合可直接提交。
- 事务内：对每个 userId，若不在 `client_members` 则插入（`added_by=NULL`），再同步 `client_role_members`（满足 INV-7）。
- 返回 `{"memberIds":[…],"added":3,"removed":1,"autoJoinedMembers":2}`。

## 14–17. 权限

`GET /permissions` 返回**扁平**列表（一次最多 1000 条，按 `sort, created_at` 升序）：

```json
{"list":[{"id":"01J…","code":"order:read","name":"订单查询","description":null,
          "parentId":null,"sort":0,"createdAt":1792540000000,"updatedAt":1792540000000}],"total":30}
```

`POST /permissions` 请求：

```json
{"code":"order:read","name":"订单查询","description":null,"parentId":null,"sort":0}
```

- `code` 匹配 `^[A-Za-z0-9_.:-]{1,64}$`，应用内唯一（区分大小写）→ `409 APP_PERMISSION_CODE_EXISTS`；`name` 1–64；`description` ≤255；`parentId` 为空表示根。
- `parentId` 不存在、跨应用、为自身或后代 → `400 APP_PERMISSION_INVALID_PARENT`。

`PATCH /permissions/:pid`：同上字段可选，另接受可选 `updatedAt`（不符 → `409 APP_RESOURCE_STALE`）；移动父级时同样执行成环校验。

`DELETE /permissions/:pid`：存在子权限 → `409 APP_PERMISSION_HAS_CHILDREN`（message 含子节点数量）；同一事务内解除角色引用后删除。审计 `app_permission_deleted` 的 detail MUST 记录 `removedRoleIds` 快照（规则同角色删除）。

## 审计

| 事件类型 | 触发 | detail 关键字段 |
|---|---|---|
| `client_updated` | 应用详情保存成功（批次 1） | `clientId`、`changed[]`；`redirectUris` / `postLogoutUris` 额外记 `before` / `after` 完整数组，其余字段只记名 |
| `app_member_added` | 添加成员成功 | `clientId`、`added`、`skipped`（按部门加入另记 `existing`、`disabled`、`deptId`） |
| `app_member_removed` | 移除成员成功 | `clientId`、`removed`、`roleBindingsRemoved` |
| `app_role_created` / `app_role_updated` | 角色写操作 | `clientId`、`roleId`、`name`、`changed[]` |
| `app_role_deleted` | 删除角色 | `clientId`、`roleId`、`name`、`removedPermissionIds[]`、`removedUserIds[]`（各超 20 项记数量 + 前 20 项） |
| `app_permission_created` / `app_permission_updated` | 权限写操作 | `clientId`、`permissionId`、`code`、`changed[]` |
| `app_permission_deleted` | 删除权限 | `clientId`、`permissionId`、`code`、`removedRoleIds[]`（超 20 项同上） |
| `app_grant_updated` | 两类授权覆盖式写入 | `clientId`、`roleId`、`permissionsAdded/Removed`、`membersAdded/Removed/autoJoined` |

业务拒绝（重名、编码冲突、含子权限、非法父级）写 `failure`；版本冲突（`APP_RESOURCE_STALE`）写 `failure` 并记 `expectedUpdatedAt` / `actualUpdatedAt`。JSON 绑定失败与格式校验错误不写审计。
