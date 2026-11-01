# Contract: admin 端组织授权接口

**Feature**: `010-app-access-org-grant` | **Base**: `http://<api>:8081/api/v1` | **Auth**: `cookieAuth` + `Admin()`（平台管理员）

统一响应封装（AGENTS §5）：成功 `{code:"OK", message:"success", data:{...}, traceId}`；失败 `data:null`。所有时间字段为 **Unix 毫秒**（number）。

## GT-1 `GET /admin/clients/:id/org-grants`

列出该应用的全部组织授权（不分页，上限 500 条）。

**200**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "list": [
      {
        "orgId": "01J9Z0K3M4N5P6Q7R8S9T0V1W2",
        "deptName": "研发部",
        "deptPath": "/01J9Y.../01J9Z0K3.../",
        "includeSubDepts": true,
        "coveredUsers": 42,
        "coveredDisabledUsers": 3,
        "uniqueUsers": 37,
        "grantedBy": "01J8X...",
        "grantedAt": 1793491200000,
        "updatedAt": 1793491200000
      }
    ],
    "total": 1
  },
  "traceId": "..."
}
```

字段语义：
- `coveredUsers`：命中该授权、在职启用（`users.status=1`）的**账号数**（spec FR-004/FR-005）。列表列头 MUST 标注"在职启用账号数"。
- `coveredDisabledUsers`：命中该授权但账号已停用（`status≠1`）的数量，仅用于撤销确认框的补充说明「另有 N 个停用账号被覆盖（不影响可见性）」（spec FR-007）。MUST 与 `coveredUsers` 由同一次聚合得出。
- `uniqueUsers`：命中该授权 **且** 非手动成员 **且** 未被其它授权命中的**在职启用**账号数，供撤销二次确认文案「撤销后 N 人将不再可见」（spec FR-007）。
- `deptPath`：授权时该部门在组织树中的路径，前端用于展示层级、不参与判定。
- 与成员口径一致：应用停用不影响 `coveredUsers`；账号停用不计入 `coveredUsers`/`uniqueUsers`。

**错误**：`401 UNAUTHENTICATED`、`403 FORBIDDEN`、`404 NOT_FOUND`（应用不存在）、`500 INTERNAL_ERROR`、`503 DEPENDENCY_UNAVAILABLE`。

## GT-2 `POST /admin/clients/:id/org-grants`

新增 / 调整组织授权（幂等）。批量语义：整次请求在**一个事务**内完成，任一条失败则全部回滚（spec FR-009）。

**Request**

```json
{
  "grants": [
    { "deptId": "01J9Z0K3M4N5P6Q7R8S9T0V1W2", "includeSubDepts": true },
    { "deptId": "01J9Z0K3M4N5P6Q7R8S9T0V1W3", "includeSubDepts": false }
  ]
}
```

约束：`grants` 长度 1–500；`deptId` 必填；`includeSubDepts` 缺省视为 `true`；同一请求内重复 `deptId` 去重。

**写入语义（critique E2/E3/E4）**

1. 整批在**一个事务**内完成：先对每个目标部门 `LockOrganization(orgID)`（行锁；不存在 → `ErrAppDeptNotFound` → `400 VALIDATION_FAILED`，整批回滚）。这样可杜绝"校验通过后部门被并发删除"导致指向已删部门的悬空授权。
2. 在同一事务内，按 `(client_id, org_id)` 读出既有行并分流：

   | 情况 | 动作 | 计数 |
   |---|---|---|
   | 不存在 | 插入（`granted_by`/`granted_at`/`updated_at` = 当前） | `added++` |
   | 存在且 `include_sub` 不同 | 仅更新 `include_sub` 与 `updated_at` | `updated++` |
   | 存在且 `include_sub` 相同 | 不动（`updated_at` **不**刷新） | `unchanged++` |

   MUST NOT 依赖 `INSERT ... ON DUPLICATE KEY UPDATE` 的 `RowsAffected` 做三态区分（无条件刷新 `updated_at` 会让其恒为 changed）。
3. 同一事务内校验上限：`INSERT` 后的条数 > 500 → `400 APP_ORG_GRANT_LIMIT`，整批回滚。
4. 审计与写入同源：每条成功变更写 1 条事件（见下）；事务回滚则不写成功事件。

**200**

```json
{
  "code": "OK",
  "message": "success",
  "data": { "added": 2, "updated": 0, "unchanged": 0, "coveredUsers": 61 },
  "traceId": "..."
}
```

- `added` / `updated`（仅 `includeSubDepts` 变化）/ `unchanged`（完全幂等）三态计数，与 003「新增/已存在/已停用」的计数风格一致。
- `coveredUsers`：本次提交涉及部门**合计**的覆盖人数（同人多部门去重）。

**错误**

| 场景 | HTTP | code |
|---|---|---|
| `grants` 为空或 > 500、字段缺失 | 400 | `VALIDATION_FAILED` |
| 部门不存在（任一） | 400 | `VALIDATION_FAILED`（service `ErrAppDeptNotFound`，整批回滚） |
| 该应用授权条数将超过 500 | 400 | `APP_ORG_GRANT_LIMIT` |
| 应用不存在 | 404 | `NOT_FOUND` |
| 未登录 / 非平台管理员 | 401 / 403 | `UNAUTHENTICATED` / `FORBIDDEN` |

**审计**：每条新增记 `app_org_granted`、每个 `includeSubDepts` 变化记 `app_org_grant_updated`；`detail = {deptId, deptName, deptPath, includeSubDepts|from,to, coveredUsers}`。失败整批不写成功事件，写 1 条 `result=failure` 且 `detail.reason` 为分类（`dept_not_found` / `limit_exceeded`）。

## GT-3 `POST /admin/clients/:id/org-grants/remove`

撤销组织授权（幂等，不存在的视为已撤销）。用 POST + 子路径而非 DELETE 带 body，与既有 `members/remove` 保持一致。

**Request**

```json
{ "orgIds": ["01J9Z0K3M4N5P6Q7R8S9T0V1W2"] }
```

约束：长度 1–500；去重。

**200**

```json
{
  "code": "OK",
  "message": "success",
  "data": { "removed": 1, "impactUsers": 37, "remainingVisibleUsers": 3 },
  "traceId": "..."
}
```

- `removed`：实际删除的授权行数（`RowsAffected`）。
- `impactUsers`：**在同一事务内、删除之前**计算的「命中待撤销授权 ∧ 非手动成员 ∧ 未被其它保留授权命中」的在职启用去重账号数；语义是"撤销将导致多少人不再可见"，**不是**撤销后的剩余量（若在删除后计算，该值恒为 0）。计数失败则整次撤销失败，不允许"删了但不知道影响多少人"（spec FR-007）。
- `remainingVisibleUsers`：同一计算中得到"仍有其它来源"的人数，用于提示"另有 3 人仍可通过其它途径访问"。
- 不触碰 `client_members`、`client_role_members`；也不回收既有会话与令牌（spec FR-029）。

**错误**：同 GT-2 的 400/401/403/404；撤销不存在的 `orgId` 不算错误。

## MT-1 成员列表响应新增字段（`GET /admin/clients/:id/members`）

保持既有 `{list, total, page, pageSize}`，**纯新增**：

```json
{
  "list": [
    {
      "userId": "01J8X...",
      "username": "zhangsan",
      "displayName": "张三",
      "deptId": "01J9Z...",
      "deptName": "研发部",
      "status": 1,
      "joinedAt": 1793000000000,
      "roleCount": 2,
      "isAdmin": false,
      "orgGrants": [
        { "orgId": "01J9Z...", "deptName": "研发部", "includeSubDepts": true }
      ]
    }
  ],
  "total": 3,
  "visibleTotal": 40,
  "page": 1,
  "pageSize": 20
}
```

- `orgGrants`：该成员命中的组织授权（可能多个；空数组表示「仅手动成员」），前端据此渲染「手动 + N 个组织」的来源列（spec FR-016）。
- `visibleTotal`：该应用当前实际可见人数（手动成员 ∪ 组织授权覆盖，去重，仅计在职启用账号）；`total` 语义不变（手动成员数）。两者可不同，页面必须同时展示（spec FR-019）。**仅在 `page=1` 时返回数字，其余页返回 `null`**（critique E9：该值是全表聚合，翻页/筛选不必重算，前端沿用首屏值）。
- `orgGrantHints`（仅 `page=1` 返回，最多 3 条）：存量迁移提示（spec FR-031）。当某部门在本应用中的成员数 ≥ 20 且该部门及其祖先均未被组织授权时给出 `{orgId, deptName, memberCount}`；前端以只读提示展示，**不得**自动转换成员关系，可逐条关闭。

## MT-2 候选列表新增字段（`GET /admin/clients/:id/members/candidates`）

每项新增 `orgCovered: boolean` —— 该候选是否已可通过组织授权访问（spec FR-018）。候选集合本身**不变**（仍是「不是成员」的用户），前端对 `orgCovered=true` 的行加标记，由管理员显式决定是否加为正式成员。

## 兼容与弃用

- `POST /admin/clients/:id/members` 的 `{deptId, includeSubDepts}` 形态**保留**但不再被 UI 调用；OpenAPI 描述追加：「已由组织授权取代（010），仅为兼容存量调用保留，后续版本移除」。语义仍是一次性快照（spec FR-026/FR-027）。
