package admin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// 权限点批量导入 / 导出（specs/011-app-permission-import）。
//
// 响应分工（research R4，不要"改回去"）：
//   - 校验预览：只要读到了并完成判断就 200；内容非法也是 200 + ok=false + issues；
//   - 确认导入：内容非法 400、超限 413、并发冲突 409，失败响应 data 恒为 null。
//
// 审计（spec FR-023 / FR-025）：
//   - 成功且 added+updated>0 才写 1 条；全量无变化不写；
//   - 失败只在"业务拒绝"时写；内容非法与 401/403 不写；预览永不写。

// readImportBody 读取导入文档（512KB 上限，仅作用于导入端点）。
func readImportBody(c *gin.Context) ([]byte, bool) {
	if ct := c.ContentType(); ct != "" && !strings.HasPrefix(ct, "application/json") {
		response.Fail(c, response.ErrValidation.WithMessage("Content-Type 必须是 application/json"))
		return nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxImportBodyBytes)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			// 请求体读不完，无从知道条数：只给上限与成因提示（FR-013）
			response.Fail(c, importTooLargeResponse(&service.ImportTooLargeError{}))
			return nil, false
		}
		response.Fail(c, response.ErrValidation.WithMessage("请求体读取失败"))
		return nil, false
	}
	return data, true
}

// importSource 读取来源 query（白名单外一律 paste，仅供审计统计）。
func importSource(c *gin.Context) string {
	if c.Query("source") == service.ImportSourceFile {
		return service.ImportSourceFile
	}
	return service.ImportSourcePaste
}

// importFailureReason 把 service 错误映射为（失败原因，响应错误，是否写失败审计）。
// 只有"业务拒绝"写失败审计；内容非法与内部错误不写（spec FR-025）。
func importFailureReason(err error) (string, *response.Error, bool) {
	switch {
	case errors.Is(err, service.ErrAppPermissionImportInvalid):
		return "", response.ErrAppPermissionImportInvalid, false
	case errors.Is(err, service.ErrAppPermissionImportTooLarge):
		return "too_large", importTooLargeResponse(err), true
	case errors.Is(err, service.ErrAppResourceStale):
		return "stale", response.ErrAppResourceStale, true
	case errors.Is(err, service.ErrClientNotFound):
		return "client_not_found", response.ErrNotFound, true
	case errors.Is(err, service.ErrAppClientDisabled):
		return "client_disabled", response.ErrForbidden.WithMessage("应用已停用，请先启用后再导入"), true
	default:
		return "", nil, false
	}
}

// importAuditFields 生成成功导入的审计字段；第二个返回值为 false 表示不写事件
// （新增与更新都为 0 的全量无变化导入，spec FR-023 / SC-003）。
func importAuditFields(clientID string, result *dto.PermissionImportResult, outcome *service.ImportOutcome) (map[string]any, bool) {
	if result.Added == 0 && result.Updated == 0 {
		return nil, false
	}
	addedIDs := outcome.AddedIDs
	if len(addedIDs) > 20 {
		addedIDs = addedIDs[:20]
	}
	return map[string]any{
		"clientId": clientID, "added": result.Added, "updated": result.Updated,
		"unchanged": result.Unchanged, "moved": result.Moved, "total": result.Total,
		"source": result.Source, "addedIds": addedIDs, "addedCount": outcome.AddedCount,
		"docSha256": outcome.DocSha256,
	}, true
}

// importTooLargeResponse 按超限成因给 413 文案（FR-013）：
//   - 条数超限：必须含本次实际条数，便于裁剪；
//   - 体积超限：请求体不完整，不得声称知道条数，只给上限并提示成因。
func importTooLargeResponse(err error) *response.Error {
	var too *service.ImportTooLargeError
	if errors.As(err, &too) && too.Count > 0 {
		return response.ErrAppPermissionImportTooLarge.WithMessage(fmt.Sprintf(
			"本次共 %d 条，超过单次上限 %d 条，请裁剪后重试", too.Count, service.MaxImportPermissions))
	}
	return response.ErrAppPermissionImportTooLarge.WithMessage(
		"导入内容超过 512KB 上限，可能是描述或名称过长所致，请裁剪后重试")
}

// failPermissionPreview 预览专用失败映射：**不写任何审计**（FR-002 / FR-025）。
// 预览是只读动作，应用不存在/停用这类拒绝也不留痕，否则连续点校验会把审计页淹没。
func (h *Handler) failPermissionPreview(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAppPermissionImportInvalid):
		response.Fail(c, response.ErrAppPermissionImportInvalid)
	case errors.Is(err, service.ErrAppPermissionImportTooLarge):
		response.Fail(c, importTooLargeResponse(err))
	case errors.Is(err, service.ErrClientNotFound):
		response.Fail(c, response.ErrNotFound)
	case errors.Is(err, service.ErrAppClientDisabled):
		response.Fail(c, response.ErrForbidden.WithMessage("应用已停用"))
	default:
		h.log.Error("permission import preview failed", "err", err, "path", c.FullPath())
		response.Fail(c, response.ErrInternal)
	}
}

// failPermissionImport 把 service 错误映射为统一响应；仅在业务拒绝时补一条失败审计。
func (h *Handler) failPermissionImport(c *gin.Context, clientID, source string, err error) {
	reason, respErr, audit := importFailureReason(err)
	if respErr == nil {
		h.log.Error("permission import failed", "err", err, "path", c.FullPath())
		response.Fail(c, response.ErrInternal)
		return
	}
	if audit {
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventAppPermissionsImported, ClientID: clientID,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "failure",
			Fields: map[string]any{"clientId": clientID, "reason": reason, "source": source},
		})
	}
	response.Fail(c, respErr)
}

// ValidatePermissionImport POST /api/v1/admin/clients/:id/permissions/import/validate
//
// @Summary      校验权限点导入文档
// @Description  只读预览：返回将新增/更新/无变化条数、上级变更明细与可分位置的问题清单。
// @Description  内容非法时仍返回 200 且 ok=false；只有无法解析（超 512KB）才返回 413。不写权限点、不写审计。
// @Tags         应用权限
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id     path   string  true   "应用标识 clientId"
// @Param        source query  string  false  "来源（paste|file），缺省与非法值按 paste"
// @Param        body   body   dto.PermissionImportDocument  true  "导入文档（≤512KB）"
// @Success      200  {object}  response.Body{data=dto.PermissionImportPreview}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions/import/validate [post]
func (h *Handler) ValidatePermissionImport(c *gin.Context) {
	data, ok := readImportBody(c)
	if !ok {
		return
	}
	preview, err := h.appAuth.ValidateImport(c.Request.Context(), c.Param("id"), data)
	if err != nil {
		// 预览失败一律不写审计（FR-002 / FR-025）
		h.failPermissionPreview(c, err)
		return
	}
	response.Success(c, preview)
}

// ConfirmPermissionImport POST /api/v1/admin/clients/:id/permissions/import
//
// @Summary      确认导入权限点
// @Description  单事务内按 code 幂等合并（新增 + 更新，不删除）；有实际写入时写 1 条聚合审计事件。
// @Tags         应用权限
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id     path   string  true   "应用标识 clientId"
// @Param        source query  string  false  "来源（paste|file），缺省与非法值按 paste"
// @Param        body   body   dto.PermissionImportDocument  true  "导入文档（≤512KB）"
// @Success      200  {object}  response.Body{data=dto.PermissionImportResult}  "success"
// @Failure      400  {object}  response.Body  "APP_PERMISSION_IMPORT_INVALID"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions/import [post]
func (h *Handler) ConfirmPermissionImport(c *gin.Context) {
	clientID := c.Param("id")
	source := importSource(c)
	data, ok := readImportBody(c)
	if !ok {
		return
	}
	result, outcome, err := h.appAuth.ConfirmImport(c.Request.Context(), clientID, data, source)
	if err != nil {
		h.failPermissionImport(c, clientID, source, err)
		return
	}
	// 全量无变化不写成功事件（spec FR-023 / SC-003），判定收敛在 importAuditFields。
	if fields, ok := importAuditFields(clientID, result, outcome); ok {
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventAppPermissionsImported, ClientID: clientID,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
			Fields: fields,
		})
	}
	response.Success(c, result)
}

// ExportPermissions GET /api/v1/admin/clients/:id/permissions/export
//
// @Summary      导出权限点
// @Description  产出与导入完全同构的 JSON（version/mode/嵌套 children，不含内部标识）。
// @Description  importable 自证"能否回导"（empty/too_many/too_large/too_deep），仅用于提示、不写入下载文件。
// @Tags         应用权限
// @Produce      json
// @Security     cookieAuth
// @Param        id  path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=dto.PermissionExportDocument}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions/export [get]
func (h *Handler) ExportPermissions(c *gin.Context) {
	doc, err := h.appAuth.ExportPermissions(c.Request.Context(), c.Param("id"))
	if err != nil {
		// 导出是只读动作，失败不写审计。
		switch {
		case errors.Is(err, service.ErrClientNotFound):
			response.Fail(c, response.ErrNotFound)
		case errors.Is(err, service.ErrAppClientDisabled):
			response.Fail(c, response.ErrForbidden.WithMessage("应用已停用"))
		default:
			h.log.Error("permission export failed", "err", err, "path", c.FullPath())
			response.Fail(c, response.ErrInternal)
		}
		return
	}
	response.Success(c, doc)
}
