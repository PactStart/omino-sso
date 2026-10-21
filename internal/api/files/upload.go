package files

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// Handler 处理 /api/v1/files/*。
type Handler struct {
	files *service.FileService
	log   *slog.Logger
}

// NewHandler 构造。
func NewHandler(files *service.FileService, logger *slog.Logger) *Handler {
	return &Handler{files: files, log: logger}
}

// UploadCredentialRequest POST /files/upload-credential 请求体。
type UploadCredentialRequest struct {
	Scene  string `json:"scene" binding:"required" example:"user_avatar"`
	Format string `json:"format" binding:"required" example:"png"`
	Size   int64  `json:"size" binding:"required,min=1" example:"245760"`
}

// CreateUploadCredential 签发直传凭证。
//
// @Summary      签发上传直传凭证
// @Description  返回预签名直传凭证，前端据此直传对象存储；scene=app_logo 需 admin。
// @Tags         文件
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  UploadCredentialRequest  true  "凭证请求体"
// @Success      200   {object}  response.Body{data=dto.UploadCredential}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /files/upload-credential [post]
func (h *Handler) CreateUploadCredential(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req UploadCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	if req.Scene == model.FileSceneAppLogo && !hasAdminRole(u) {
		response.Fail(c, response.ErrForbidden)
		return
	}
	cred, err := h.files.CreateUploadCredential(c.Request.Context(), req.Scene, req.Format, req.Size)
	if err != nil {
		if err == service.ErrFileValidation {
			response.Fail(c, response.ErrValidation)
			return
		}
		h.log.Error("create upload credential", "err", err)
		response.Fail(c, response.ErrDependencyDown)
		return
	}
	response.Success(c, dto.UploadCredential{
		Key:           cred.Key,
		UploadURL:     cred.UploadURL,
		AccessKeyID:   cred.AccessKeyID,
		SecurityToken: cred.SecurityToken,
		Policy:        cred.Policy,
		Signature:     cred.Signature,
		ExpiresAt:     dto.Time(cred.ExpiresAt),
	})
}

func hasAdminRole(u *model.User) bool {
	for _, r := range u.Roles {
		if r == "admin" {
			return true
		}
	}
	return false
}

// ConfirmUploadRequest 通用上传确认请求体；targetId 为空表示先落文件、后绑定。
type ConfirmUploadRequest struct {
	Scene    string `json:"scene" binding:"required" example:"app_logo"`
	Key      string `json:"key" binding:"required" example:"app_logo/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png"`
	Format   string `json:"format" binding:"required" example:"png"`
	Size     int64  `json:"size" binding:"required,min=1" example:"245760"`
	TargetID string `json:"targetId" example:"01J9ZK9M1N3P5Q7R9S1T3V5W7X"`
}

// ConfirmUpload 确认上传并写入文件记录（仅 app_logo 场景，注册应用前「先存文件、后绑定」）。
// targetId 为空时仅落记录，业务字段由创建应用时回填绑定。
//
// @Summary      确认上传
// @Description  直传完成后调用，写入 files 记录（仅 app_logo）；targetId 为空时仅落记录，创建应用成功后回填绑定。需 admin。
// @Tags         文件
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  ConfirmUploadRequest  true  "确认请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /files/confirm [post]
func (h *Handler) ConfirmUpload(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req ConfirmUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	// 仅支持 app_logo 的「先存后绑」；user_avatar 一律走 /me/avatar 或 /admin/users/:id/avatar。
	if req.Scene != model.FileSceneAppLogo {
		response.Fail(c, response.ErrValidation)
		return
	}
	if !hasAdminRole(u) {
		response.Fail(c, response.ErrForbidden)
		return
	}
	f, err := h.files.ConfirmUpload(c.Request.Context(), service.ConfirmUploadInput{
		Scene:    req.Scene,
		Key:      req.Key,
		Format:   req.Format,
		Size:     req.Size,
		TargetID: req.TargetID,
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
		h.log.Error("confirm upload", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}
