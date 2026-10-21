# Contract: 管理后台组织架构写接口

**Date**: 2026-10-21 | **Spec**: [spec.md](../spec.md) | **Data model**: [data-model.md](../data-model.md)

适用于 `:8081` 的 `/api/v1/admin/orgs*`。全部端点要求管理员会话（`sid` cookie，`HttpOnly`），响应遵循统一响应 `{code, message, data, traceId}`（规范 §5），`Content-Type: application/json`（请求与响应）。

---

## 0. 通用

| 项 | 约定 |
|---|---|
| 鉴权 | `middleware.Auth` + `middleware.Admin`；缺失/失效会话 `401 UNAUTHENTICATED`，非 admin `403 FORBIDDEN` |
| 请求体 | 仅 JSON；未知字段被忽略（含 `tenantId`），不报错 |
| 租户 | 不接受客户端指定：根级固定 `default`，子节点继承父节点 |
| `path` | 服务端派生，客户端提交无效（被忽略） |
| 错误响应体 | `{"code":"<CODE>","message":"<中文提示>","data":null,"traceId":"<ULID>"}` |
| traceId | 同时写响应头 `X-Trace-Id` |
| 审计 | 成功与业务拒绝写 `audit_events`；401/403/参数格式错误不写 |

---

## 1. 创建组织节点

`POST /api/v1/admin/orgs`

### 请求体

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `parentId` | string \| null | 否 | 缺失或 `null` = 根级；非空时必须是已存在节点（ULID，26 字符） |
| `name` | string | 是 | 去首尾空白后 1–64 字符；不得含控制字符；与同级节点区分大小写比较 |
| `sort` | number | 否 | 整数，默认 0；MUST 落在 `int32` 范围（建议 `0..1_000_000`），越界返回 `VALIDATION_FAILED`；同级按升序展示，相同时按创建时间升序 |
| `tenantId` | — | — | **不属于契约**；若提交则被忽略 |

```json
{
  "parentId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
  "name": "平台工程组",
  "sort": 3
}
```

### 响应 201

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZK9M1N3P5Q7R9S1T3V5W7X",
    "parentId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "name": "平台工程组",
    "path": "/01J9ZJ7A1B2C3D4E5F6G7H8J9K/01J9ZK9M1N3P5Q7R9S1T3V5W7X/",
    "sort": 3,
    "tenantId": "default",
    "userCount": 0,
    "createdAt": "2026-10-21T02:15:07Z"
  },
  "traceId": "01J9ZK9N8P0Q2R4S6T8V0W2X4Y"
}
```

### 错误

| HTTP | code | 触发条件 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 名称空/全空白/超长/含控制字符；创建后深度 > 10（message 固定"层级不能超过 10 级"）；`sort` 超出 `int32` 范围 |
| 400 | `ORG_INVALID_PARENT` | `parentId` 非空但节点不存在 |
| 401 | `UNAUTHENTICATED` | 无有效会话 |
| 403 | `FORBIDDEN` | 非 admin |
| 409 | `ORG_NAME_ALREADY_EXISTS` | 同租户 + 同父 + 同名（区分大小写） |
| 500 | `INTERNAL_ERROR` | 未预期错误 |
| 503 | `DEPENDENCY_UNAVAILABLE` | MySQL/Redis 不可用 |

---

## 2. 修改组织节点（重命名 / 调序 / 移动）

`PATCH /api/v1/admin/orgs/:id`

### 路径参数

| 参数 | 说明 |
|---|---|
| `id` | 目标节点 ULID（26 字符）；不存在 → `404 NOT_FOUND` |

### 请求体（全部可选，局部更新）

| 字段 | 类型 | 三态语义 |
|---|---|---|
| `name` | string | 缺失 = 不变；提交则按创建时的名称规则校验（含同级唯一，排除自身） |
| `sort` | number | 缺失 = 不变；提交时须落在 `int32` 范围（与创建一致） |
| `parentId` | string \| null | 缺失 = 不变；`null` = 移到根级；非空 = 移到该节点下（须存在、非自身/后代、同租户）。文档以 `x-nullable` 标注，并在描述中写明三态语义 |

**空请求体**：`{}`（或三个字段均为 `null`/缺失）视为无变更 → `200` + 当前节点，不写库、不写审计（幂等）。

```json
{ "name": "平台工程组", "sort": 1 }
```

```json
{ "parentId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K" }
```

```json
{ "parentId": null }
```

### 响应 200

返回修改后的节点（`userCount` 为该节点及其子部门汇总）：

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZK9M1N3P5Q7R9S1T3V5W7X",
    "parentId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "name": "平台工程组",
    "path": "/01J9ZJ7A1B2C3D4E5F6G7H8J9K/01J9ZK9M1N3P5Q7R9S1T3V5W7X/",
    "sort": 1,
    "tenantId": "default",
    "userCount": 12,
    "createdAt": "2026-10-21T02:15:07Z"
  },
  "traceId": "01J9ZK9N8P0Q2R4S6T8V0W2X4Y"
}
```

**移动语义**：该节点及其全部后代的 `path` 在同一事务内整体搬迁；`sort` 不被隐式改写；用户归属与人数统计不变。管理后台通过"移动到…"入口（选择新上级，含"根级"）调用本端点，不做拖拽。

### 错误

| HTTP | code | 触发条件 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 名称不合法；移动后子树深度 > 10；`sort` 超出 `int32` 范围 |
| 400 | `ORG_INVALID_PARENT` | 新父不存在；新父是自身或自身后代（成环）；新父与节点租户不一致（仅存量脏数据可能） |
| 401 | `UNAUTHENTICATED` | 无有效会话 |
| 403 | `FORBIDDEN` | 非 admin |
| 404 | `NOT_FOUND` | `:id` 不存在或已被删除 |
| 409 | `ORG_NAME_ALREADY_EXISTS` | 改名后与同级重名 |
| 500 | `INTERNAL_ERROR` | 未预期错误 |
| 503 | `DEPENDENCY_UNAVAILABLE` | MySQL/Redis 不可用 |

**整体性**：任一校验失败则整个请求不产生任何写入（名称与父节点同时非法时也不会只写一半）。

---

## 3. 删除组织节点

`DELETE /api/v1/admin/orgs/:id`

无请求体。

### 响应 200

```json
{
  "code": "OK",
  "message": "success",
  "data": null,
  "traceId": "01J9ZK9N8P0Q2R4S6T8V0W2X4Y"
}
```

### 错误

| HTTP | code | 触发条件 |
|---|---|---|
| 401 | `UNAUTHENTICATED` | 无有效会话 |
| 403 | `FORBIDDEN` | 非 admin |
| 404 | `NOT_FOUND` | `:id` 不存在或已被删除（重复删除同样返回此码） |
| 409 | `ORG_HAS_CHILDREN` | 仍有子部门（message：请先处理子部门） |
| 409 | `ORG_HAS_USERS` | 仍有账号归属（message：请先迁移账号） |
| 500 | `INTERNAL_ERROR` | 未预期错误 |
| 503 | `DEPENDENCY_UNAVAILABLE` | MySQL/Redis 不可用 |

删除为物理删除；成功后该节点立即从组织树中消失。

---

## 4. 既有读取端点（不变）

`GET /api/v1/admin/orgs/tree` 的响应结构、字段名与含义保持不变：

```json
[
  {
    "id": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
    "name": "总公司",
    "parentId": null,
    "path": "/01J9ZJ8Q2M4N6P8R0T2V4X6Z8A/",
    "userCount": 3,
    "children": []
  }
]
```

新增端点不得改变该端点的路径、方法与报文；前端其它消费方无需改造（FR-015、SC-006）。

---

## 5. 契约不变量与并发保证

| 编号 | 保证 | 验证方式 |
|---|---|---|
| C-1 | 三个写端点均需管理员会话；无 cookie 时在触达业务前返回 401 | `router_test` 断言 401 + 统一响应体 |
| C-2 | 任一失败请求不产生部分写入（含级联路径的一半） | quickstart 场景 10（拒绝后对比树快照） |
| C-3 | 同级同名最多存在一个（并发下同样成立） | quickstart 场景 9（并发脚本） |
| C-4 | 任何时刻组织树无环、无孤儿、深度 ≤ 10 | quickstart 场景 6/7/11 |
| C-5 | 移动后整棵子树在新父下可见，用户归属不变 | quickstart 场景 4/5 |
| C-6 | 删除仅在无子节点且无账号时成功 | quickstart 场景 7/8 |
| C-7 | 响应键集与 `openapi.json` 一致，且文档与路由集合一致 | `make docs` + `TestDocsPathsMatchRouterRoutes` |
| C-8 | 账号归属写入（创建用户 / 改 `deptId`）与部门删除互斥，不产生悬空 `dept_id` | quickstart 场景 14（并发删部门 + 建用户） |

---

## 6. 文档与规范同步要求（交付前置）

1. 每个 handler 必须有 `@Summary/@Description/@Tags/@Security/@Param/@Success/@Failure/@x-auth-level/@Router` 注解，`@Tags` 统一为 `组织架构`，`@Failure` 的业务码必须与第 1–3 节的错误表逐字一致。
2. `make docs` 重生成 `internal/api/docs/openapi.json`，使其包含 **16 条 path / 19 个操作**（现为 14 条 / 16 个；3 个新端点只新增 2 条 path：`/admin/orgs` 与 `/admin/orgs/{id}`）；`make docs-check` 必须通过。⚠️ `router_test.go` 与 `docs/docs_test.go` 各有一处硬编码 `ops != 16` 断言，必须同步改为 19，否则 `make test` / `make build` 失败。
3. `AGENTS.md` 同批更新：§4.1 接口总览新增三行、§4.6 字段约定新增三行、§5.3 新增 4 个错误码、§6.7 新增 3 个事件类型。
4. 错误码常量写入 `internal/api/response/errors.go`，禁止在 handler 内拼接字符串。
5. `web/admin/src/utils/format.ts` 的 `EVENT_LABEL` 同批新增 `org_created` / `org_updated` / `org_deleted` 三个中文标签；审计页下拉由该常量生成，缺一项即无法按事件类型筛选（FR-014）。
6. **相邻端点的行为修正**：为消除 `dept_id` 悬空竞态，`PATCH /api/v1/admin/users/:id` 的 `deptId` 由"静默写入"改为"短事务内锁定目标部门并校验存在"，不存在时返回 `400 VALIDATION_FAILED`；`POST /api/v1/admin/users` 的锁定方式同步调整（对外行为不变）。该修正需写入 `AGENTS.md` §4.1 对应用户端点行。
7. **`parentId` 的 null 语义**：`UpdateOrgRequest.ParentID` 注解 MUST 带 `extensions:"x-nullable"`，`@Description` 写明"缺失 = 不变 / null = 移到根级 / 值 = 移到该节点下"；`make docs` 后确认 `openapi.json` 呈现 `nullable: true`，若转换链未保留该扩展，则以 description 兜底并保持三态文字完整。
