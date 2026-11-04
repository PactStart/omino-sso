# 契约：admin 端应用权限点导入 / 导出

**Owner**: `internal/api/admin/app_auth.go`（两端共用的唯一实现）
**Date**: 2026-11-03 | **Plan**: [../plan.md](../plan.md) | **Data Model**: [../data-model.md](../data-model.md)

三个端点均为**新增**，纯 additive；不改变任何既有端点的报文。全部走统一响应（`{code, message, data, traceId}`），鉴权为平台管理员（`Auth + Admin()`，与同组既有权限端点一致）。

---

## 1. `POST /api/v1/admin/clients/:id/permissions/import/validate`

**用途**：校验预览。只读，不写权限点、不写审计（FR-002）。

**请求头 / 查询参数**

| 位置 | 名称 | 必填 | 取值 | 说明 |
|---|---|---|---|---|
| header | `Content-Type` | 是 | `application/json` | 否则 `400 VALIDATION_FAILED` |
| query | `source` | 否 | `paste` \| `file` | 缺省与**非法值一律按 `paste`**；仅用于审计的"来源"字段，不参与校验。用 query 而非自定义头：自定义头在前后端分域部署时会触发 CORS 预检，而管理面未挂 CORS（research R1） |

**请求体**：导入文档（见 [data-model.md §1](../data-model.md)），上限 512KB。

```json
{
  "version": 1,
  "mode": "merge",
  "permissions": [
    { "code": "order", "name": "订单", "children": [{ "code": "order.write", "name": "创建订单" }] }
  ]
}
```

**成功响应 `200`**（无论内容是否合法，只要"读到了并完成了判断"）

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "ok": true,
    "added": 132,
    "updated": 3,
    "unchanged": 5,
    "moved": 1,
    "total": 140,
    "issues": [],
    "issueTotal": 0,
    "warnings": [],
    "warningTotal": 0,
    "movedItems": [
      { "code": "order.read", "fromParent": "order", "toParent": "pay" }
    ]
  },
  "traceId": "8f1c0a3e5b7d4a2c"
}
```

校验不通过（`ok: false`）同样 `200`，`issues` 非空：

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "ok": false, "added": 0, "updated": 0, "unchanged": 0, "moved": 0, "total": 0,
    "issues": [
      { "path": "permissions[2].code", "field": "code", "code": "invalid_code",
        "message": "编码含非法字符，只允许字母、数字与 _.:-", "line": null },
      { "path": "", "field": "", "code": "syntax_error",
        "message": "内容不是合法的 JSON（第 7 行附近）", "line": 7 }
    ],
    "issueTotal": 2,
    "warnings": [
      { "path": "permissions[4].code", "field": "code", "code": "case_hint",
        "message": "与 permissions[1].code 忽略大小写后相同，将作为两个不同权限点导入", "line": null }
    ],
    "warningTotal": 1
  },
  "traceId": "8f1c0a3e5b7d4a2c"
}
```

**计数口径**：`added + updated + unchanged = total`；`moved ≤ updated`；`movedItems` 至多 20 条（`moved > 20` 时前端显示"共 N 条，仅列出前 20 条"），每条 `{code, fromParent, toParent}`，父级用 `code` 表达、根级为 `""`（明细见 data-model §4）。`ok=false` 时计数全 0（不做半截统计，避免误导）。

**失败响应**

| HTTP | code | 触发 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | `Content-Type` 不是 `application/json` / 请求体无法读取 |
| 401 | `UNAUTHENTICATED` | 未登录 |
| 403 | `FORBIDDEN` | 非平台管理员 / 应用已停用（`message` 为"应用已停用，请先启用后再导入"） |
| 404 | `NOT_FOUND` | 应用不存在 |
| 413 | `APP_PERMISSION_IMPORT_TOO_LARGE` | 请求体 > 512KB（**无法解析，故不能走 200**）；`message` 给出实际上限，不声称知道条数 |
| 429 | `RATE_LIMITED` | 触发全局限流中间件 |
| 500 | `INTERNAL_ERROR` | 其它 |

> 注意：**"内容非法"不返回 4xx**。这是 spec FR-005（可分位置清单）与 FR-029（失败响应 `data` 为 null）的唯一自洽解（research R4）。前端据 `data.ok` 决定是否允许"确认导入"。

**swag 注解要点**（`make docs` 会据此生成，`internal/api/docs/docs_test.go` 断言每个 operation 有 summary/tags/2xx/500，管理端点须有 401+403，写操作须有 400）

```
@Summary      校验权限点导入文档
@Description  只读预览：返回将新增/更新/无变化条数与问题清单。内容非法时仍返回 200 且 ok=false。
@Tags         应用权限
@Accept       json
@Produce      json
@Param        id     path  string  true   "应用 ID"
@Param        source query string  false  "来源（paste|file），缺省与非法值按 paste"
@Param        body   body  dto.PermissionImportDocument  true  "导入文档（≤512KB）"
@Success      200  {object}  response.Body{data=dto.PermissionImportPreview}
@Failure      400  {object}  response.Body  "VALIDATION_FAILED"
@Failure      401  {object}  response.Body  "UNAUTHENTICATED"
@Failure      403  {object}  response.Body  "FORBIDDEN"
@Failure      404  {object}  response.Body  "NOT_FOUND"
@Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
@Failure      500  {object}  response.Body  "INTERNAL_ERROR"
@x-auth-level  "admin"
@Router        /admin/clients/{id}/permissions/import/validate [post]
```

---

## 2. `POST /api/v1/admin/clients/:id/permissions/import`

**用途**：确认导入。单事务内合并写入（新增 + 更新），成功写 1 条聚合审计事件。

**请求头 / 查询参数 / 请求体**：同 §1（`?source=` 会进入审计）。

**成功响应 `200`**（不是 201：本端点语义是"合并"，可能只更新、也可能无变化）

```json
{
  "code": "OK",
  "message": "success",
  "data": { "added": 28, "updated": 2, "unchanged": 0, "moved": 1, "total": 30, "source": "file" },
  "traceId": "8f1c0a3e5b7d4a2c"
}
```

- 计数口径与 §1 完全一致（FR-003）；并发改动下允许与预览不同。
- `source` 回显实际使用的来源（`paste` / `file`）。该值由客户端头声明，**仅供统计，不得作为安全依据**。

**审计（成功）**：仅当 `added > 0 || updated > 0` 时写 1 条 `app_permissions_imported`（`result=success`），`detail` 含 `clientId/added/updated/unchanged/moved/source/total/addedIds/addedCount/docSha256`：

- `addedIds`：本次新增权限点 ID，**>20 项时只记前 20 个**，`addedCount` 为实际总数（沿用既有"被解绑标识快照"的截断规则）；这是 v1 误导入后唯一可批量化处置的线索。
- `docSha256`：导入文档的 SHA-256（十六进制），用于事后确认"当时导入的是哪份文件"，不存原文。

**`added=0 && updated=0`（全量无变化）不写**——见 spec FR-023 / SC-003 / US3 独立测试。

**失败响应**

| HTTP | code | 触发 | 是否写失败审计 | 前端处置建议 |
|---|---|---|---|---|
| 400 | `APP_PERMISSION_IMPORT_INVALID` | 内容非法（结构/字段/编码/跨层重复/超深/超长/空/语法错） | **不写** | 引导"先校验预览"，据 `issues` 修内容 |
| 413 | `APP_PERMISSION_IMPORT_TOO_LARGE` | 请求体 > 512KB，或条数 > 1000 | 写（`reason=too_large`） | 提示裁剪后重试，并给"重新校验"按钮 |
| 409 | `APP_RESOURCE_STALE` | 唯一键冲突（外部路径并发插入同 `code`） | 写（`reason=stale`） | 提示"已被他人修改"，并自动重跑一次预览 |
| 403 | `FORBIDDEN` | 非管理员 / 应用停用 | 停用写（`reason=client_disabled`）；非管理员不写 | — |
| 404 | `NOT_FOUND` | 应用不存在 | 写（`reason=client_not_found`） | — |
| 401 / 500 | 同上表 | | 不写 | — |

`data` 恒为 `null`（FR-029）。**明细只在预览里给**，前端正常流程必经预览。

> `409` **只**表示唯一键冲突；导入期间的并发单条编辑会被文档覆盖（导入以文档为准），不构成 409。

**swag 注解**：与 §1 同构，`@Success 200 {object} response.Body{data=dto.PermissionImportResult}`，`@Failure` 增加 `400 APP_PERMISSION_IMPORT_INVALID`、`409 APP_RESOURCE_STALE`、`413 APP_PERMISSION_IMPORT_TOO_LARGE`。

---

## 3. `GET /api/v1/admin/clients/:id/permissions/export`

**用途**：导出为与导入同构的文档（FR-019）。只读。

**成功响应 `200`**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "version": 1,
    "mode": "merge",
    "importable": { "ok": true, "reasons": [] },
    "permissions": [
      { "code": "order", "name": "订单", "description": "订单域",
        "children": [ { "code": "order.write", "name": "创建订单" } ] }
    ]
  },
  "traceId": "8f1c0a3e5b7d4a2c"
}
```

- `data` 里**没有** `id` / `parentId` / `sort` / 计数 / 时间（FR-007、FR-019）。
- **必须用 `ListClientPermissionsForExport`**（无 `LIMIT`、无 `child_count`/`role_count` 子查询）。复用 `ListClientPermissions` 会让 >1000 条的应用静默导出残缺文件（research R9）。
- `importable` 是"这份文件能否回导"的自证，`reasons` 可同时出现多个：

| `reasons` | 判定 |
|---|---|
| `empty` | `permissions` 为空 |
| `too_many` | 条数 > 1000 |
| `too_large` | `{version, mode, permissions}` 以 **2 空格缩进** 序列化后 > 512KB（与下载文件逐字节一致；**不得用 compact 判定**，否则会给出"可回导"的假承诺） |
| `too_deep` | 深度 > 100 |

  **仅用于前端提示，不写入下载文件**。
- 空应用返回 `permissions: []` + `importable={ok:false, reasons:["empty"]}`。
- 每层按 `(sort, created_at)` 升序（与列表端点同口径，但不带其 `LIMIT`）。

**失败响应**：`401`、`403`、`404`、`500`。无 400/413（无请求体）。

**前端下载**：`importable.ok=false` 时先按 `reasons` 提示（空 / 过多 / 过大 / 过深），`too_many` 附带一句"列表页最多展示 1000 条"（列表端点既有 `LIMIT 1000`，否则会被误认为导出出错）；用户可继续下载。随后 `JSON.stringify({version, mode, permissions}, null, 2)` → `Blob` → `a[download="permissions-<clientId>-<yyyyMMdd>.json"]`。**`importable` 不能进文件**（否则回导被 `unknown_field` 拒绝）。

---

## 4. 变更清单（供实现与评审核对）

| 位置 | 变更 |
|---|---|
| `internal/api/router.go` | `adminGroup` 增 3 条（validate / import / export） |
| `internal/api/admin/app_permission_import.go` | **新增文件**：3 个 handler + `failPermissionImport` 错误映射（`app_auth.go` 已有 1049 行，不再加厚） |
| `internal/api/dto/dto.go` | `PermissionImportDocument` / `PermissionImportNode` / `PermissionImportIssue` / `PermissionImportPreview` / `PermissionImportResult` / `PermissionExportDocument` / `PermissionExportable` |
| `internal/service/app_permission_import.go` | `ValidateImport` / `ConfirmImport` / `ExportPermissions` + 纯函数 `parseImportDocument` / `buildImportPlan` |
| `internal/store/app_auth_store.go` | `LockClientRow` / `LockClientPermissions` / `CreateClientPermissions` / `UpdateClientPermissionFields` / **`ListClientPermissionsForExport`（无 LIMIT）** |
| `internal/api/response/errors.go` | `APP_PERMISSION_IMPORT_INVALID`(400) / `APP_PERMISSION_IMPORT_TOO_LARGE`(413) |
| `internal/store/model/audit_event.go` | `EventAppPermissionsImported` |
| `AGENTS.md` | §4.6 路由 3 条、§5.3 错误码 2 条、§6.7 事件 1 条（含 no-op 与"内容非法不记"规则）、§3 目录树 |
| `web/{admin,portal}/src/utils/file.ts` | 新增：`readJsonFile` / `downloadJson`（各工程内唯一实现） |
| `internal/api/docs/openapi.json` + `docs_test.go`(84→90) + `specs/007-deployment/api-evolve/baseline/manifest.json` | `make docs` 后同步 |
