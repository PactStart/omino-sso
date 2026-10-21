package admin

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// FileUploadRequest 上传确认请求体（logo 与头像共用）。
type FileUploadRequest struct {
	Key    string `json:"key" binding:"required" example:"app_logo/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png"`
	Format string `json:"format" binding:"required" example:"png"`
	Size   int64  `json:"size" binding:"required,min=1" example:"245760"`
}

// SetClientLogo 上传确认并回写应用 logo。
//
// @Summary      设置应用 logo
// @Description  直传完成后调用，写入文件记录并回写 clients.logo_url。
// @Tags         应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用 ID（client_id）"
// @Param        body  body  FileUploadRequest  true  "logo 确认请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/logo [post]
func (h *Handler) SetClientLogo(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req FileUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f, err := h.files.ConfirmUpload(c.Request.Context(), service.ConfirmUploadInput{
		Scene:    model.FileSceneAppLogo,
		Key:      req.Key,
		Format:   req.Format,
		Size:     req.Size,
		TargetID: c.Param("id"),
		ActorID:  u.ID,
	})
	switch {
	case err == nil:
	case err == service.ErrFileValidation:
		response.Fail(c, response.ErrValidation)
		return
	case err == service.ErrTargetNotFound:
		response.Fail(c, response.ErrNotFound)
		return
	default:
		h.log.Error("set client logo", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}

// SetUserAvatar 管理员代管用户头像。
//
// @Summary      设置用户头像（管理员代管）
// @Description  直传完成后调用，写入文件记录并回写目标用户 users.avatar_url。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "用户 ID（ULID）"
// @Param        body  body  FileUploadRequest  true  "头像确认请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/users/{id}/avatar [post]
func (h *Handler) SetUserAvatar(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req FileUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f, err := h.files.ConfirmUpload(c.Request.Context(), service.ConfirmUploadInput{
		Scene:    model.FileSceneUserAvatar,
		Key:      req.Key,
		Format:   req.Format,
		Size:     req.Size,
		TargetID: c.Param("id"),
		ActorID:  u.ID,
	})
	switch {
	case err == nil:
	case err == service.ErrFileValidation:
		response.Fail(c, response.ErrValidation)
		return
	case err == service.ErrTargetNotFound:
		response.Fail(c, response.ErrNotFound)
		return
	default:
		h.log.Error("set user avatar", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}

// ListFiles 分页查询文件记录。
//
// @Summary      分页查询上传文件记录
// @Description  按 scene/status/keyword 筛选，返回分页结果。
// @Tags         文件
// @Produce      json
// @Security     cookieAuth
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 100"
// @Param        scene     query  string  false  "场景"  Enums(app_logo, user_avatar)
// @Param        status    query  string  false  "状态"  Enums(active, disabled, deleted)
// @Param        keyword   query  string  false  "url 或对象键关键字"
// @Success      200       {object}  response.Body{data=response.Page{list=[]dto.FileItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400       {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401       {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403       {object}  response.Body  "FORBIDDEN"
// @Failure      500       {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503       {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/files [get]
func (h *Handler) ListFiles(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	scene := c.Query("scene")
	if scene != "" && scene != model.FileSceneAppLogo && scene != model.FileSceneUserAvatar {
		response.Fail(c, response.ErrValidation)
		return
	}
	status := c.Query("status")
	if status != "" && status != model.FileStatusActive && status != model.FileStatusDisabled && status != model.FileStatusDeleted {
		response.Fail(c, response.ErrValidation)
		return
	}
	list, total, err := h.files.ListFiles(c.Request.Context(), store.FileFilter{
		Scene:    scene,
		Status:   status,
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		h.log.Error("list files", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := make([]dto.FileItem, 0, len(list))
	for i := range list {
		out = append(out, dto.FromFile(&list[i]))
	}
	response.Success(c, response.Page{List: out, Total: total, Page: page, PageSize: pageSize})
}

// UpdateFileStatusRequest PATCH /admin/files/:id 请求体。
type UpdateFileStatusRequest struct {
	Status string `json:"status" binding:"required" example:"disabled"`
}

// UpdateFileStatus 更新文件记录状态。
//
// @Summary      更新文件记录状态
// @Description  disabled 清空业务字段保留对象；deleted 清空字段并删除对象；active 恢复写回。
// @Tags         文件
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "文件记录 ID（ULID）"
// @Param        body  body  UpdateFileStatusRequest  true  "状态请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/files/{id} [patch]
func (h *Handler) UpdateFileStatus(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req UpdateFileStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f, err := h.files.UpdateFileStatus(c.Request.Context(), c.Param("id"), req.Status, u.ID)
	switch {
	case err == nil:
	case err == service.ErrFileStatusInvalid || err == service.ErrFileValidation:
		response.Fail(c, response.ErrValidation)
		return
	case errors.Is(err, service.ErrFileNotFound):
		response.Fail(c, response.ErrNotFound)
		return
	case errors.Is(err, service.ErrTargetNotFound):
		response.Fail(c, response.ErrNotFound)
		return
	default:
		h.log.Error("update file status", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}
