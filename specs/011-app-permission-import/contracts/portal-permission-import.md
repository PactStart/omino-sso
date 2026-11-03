# 契约：门户端（应用管理员）权限点导入 / 导出

**Owner**: `internal/api/admin/app_auth.go`（同一实现）；`internal/api/me/app_admin.go` 仅加 3 个薄 wrapper
**Date**: 2026-11-03 | **Plan**: [../plan.md](../plan.md) | **管理端契约**: [admin-permission-import.md](./admin-permission-import.md)

---

## 1. 端点

| 方法 | 路径 | 语义 |
|---|---|---|
| `POST` | `/api/v1/me/apps/:id/permissions/import/validate` | 校验预览（只读） |
| `POST` | `/api/v1/me/apps/:id/permissions/import` | 确认导入 |
| `GET` | `/api/v1/me/apps/:id/permissions/export` | 导出 |

**请求头、查询参数、请求体、成功响应体与管理端逐字段相同**（`Content-Type`、`?source=paste|file`、导入文档、`PermissionImportPreview` / `PermissionImportResult` / `PermissionExportDocument`）。差异只在**鉴权与失败语义**，见 §2。

**挂载位置**：`internal/api/router.go` 的 `appAdminGroup`（`authed.Group("/me/apps/:id")` + `middleware.AppAdmin(appAuth, logger)`），与既有 4 条权限 CRUD 路由同组。`.spec` 侧必须注册在 `appAdminGroup` 内，**不得**注册到 `authed` 组（否则非应用管理员可调用）。

---

## 2. 鉴权与失败语义

| 情形 | 管理端 | 门户端 |
|---|---|---|
| 未登录 | `401 UNAUTHENTICATED` | `401 UNAUTHENTICATED` |
| 非平台管理员 | `403 FORBIDDEN` | — |
| 非该应用的应用管理员 | — | `403 FORBIDDEN`（`message` 为通用"无权限执行该操作"，**不区分**"应用不存在 / 已停用 / 不是成员 / 不是管理员"） |
| 应用不存在 | `404 NOT_FOUND` | `403 FORBIDDEN` |
| 应用已停用 | `403 FORBIDDEN`（"应用已停用，请先启用后再导入"） | `403 FORBIDDEN`（同通用文案） |
| 内容非法 | `400 APP_PERMISSION_IMPORT_INVALID`（确认导入）/ `200 ok=false`（预览） | 同管理端 |
| 超限 | `413 APP_PERMISSION_IMPORT_TOO_LARGE` | 同管理端 |
| 并发冲突 | `409 APP_RESOURCE_STALE` | 同管理端 |

**理由**：门户侧的"应用不存在/停用/非管理员"必须在响应上**不可区分**（spec FR-021 / US5-AC2，"原因不泄露 Q 是否存在"）。该折叠已由既有 `middleware.AppAdmin` + `AuthorizeAppAdmin`（`internal/service/app_auth.go:612-631`，各哨兵只进服务端日志）实现，导入端点**不加额外判断**即可继承。因此门户端契约里**没有 404**。

> 一处需要显式确认：admin 端在导入路径上新增了"应用停用"校验（research R7），门户端由中间件天然覆盖——两端对"停用"的实际拒绝行为一致（都拒绝），只是状态码不同（403 vs 403，文案不同），符合"拒绝语义与既有端点统一"（FR-021/FR-022）。

---

## 3. wrapper 形态（照抄既有 18 个 wrapper）

```go
// ValidatePermissionImport POST /api/v1/me/apps/:id/permissions/import/validate
//
// @Summary      校验权限点导入文档（应用管理员）
// @Description  与 admin 端同一实现：只读预览。内容非法时仍返回 200 且 ok=false。
// @Tags         门户应用管理
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "应用 ID"
// @Param        source query string  false  "来源（paste|file），缺省与非法值按 paste"
// @Param        body  body  dto.PermissionImportDocument  true  "导入文档（≤512KB）"
// @Success      200  {object}  response.Body{data=dto.PermissionImportPreview}
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router        /me/apps/{id}/permissions/import/validate [post]
func (h *Handler) AppValidatePermissionImport(c *gin.Context) { h.appMgmt.ValidatePermissionImport(c) }
```

三个 wrapper 均为此形态（`h.appMgmt` 为 `*admin.Handler`，由 `router.go` 注入）。

---

## 4. 两端一致性要求（SC-007）

同一份导入文档在两端必须得到**相同结论**：

| 断言 | 落实方式 |
|---|---|
| `data.ok` 相同 | 同一 service 实现，天然一致 |
| `issues` 的 `path`/`code`/`message` 逐一相同 | 同上；`docs_test` 不断言，靠集成测试 `TestPermissionImportPreviewParity`（同一文档、两个应用） |
| 三态计数与 `movedItems` 相同 | 同上 |
| 导出 `importable` 判定相同 | 同上（同一 `ExportPermissions`） |
| 前端文案相同 | 两端 `PermissionImportDialog.spec.ts` 断言同一组提示（006 的复制实现风险由测试兜住） |

---

## 5. 变更清单（门户侧）

| 位置 | 变更 |
|---|---|
| `internal/api/router.go` | `appAdminGroup` 增 3 条 |
| `internal/api/me/app_admin.go` | 新增 3 个 wrapper（含 swag 注释） |
| `internal/api/router_test.go` | 断言 3 条门户权限导入路由已注册 + 未登录返回 401 |
| `web/portal/src/api/app.ts`、`types.ts` | 3 个调用 + 类型 |
| `web/portal/src/utils/file.ts` | 新增：`readJsonFile` / `downloadJson`（与 admin 各写一份，不跨工程共享） |
| `web/portal/src/views/app-admin/Permissions.vue` | 挂对话框 + 导出按钮 + 示例下载 |
| `web/portal/src/views/app-admin/PermissionImportDialog.vue` | admin 版复制（EP 局部注册） |
| `AGENTS.md` §4.1 | 门户 3 条路由登记 |
