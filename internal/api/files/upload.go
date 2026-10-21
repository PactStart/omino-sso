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
