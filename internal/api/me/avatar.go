package me

import (
	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// AvatarRequest POST /me/avatar 请求体。
type AvatarRequest struct {
	Key    string `json:"key" binding:"required" example:"user_avatar/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png"`
	Format string `json:"format" binding:"required" example:"png"`
	Size   int64  `json:"size" binding:"required,min=1" example:"245760"`
}

// UpdateAvatar 上传确认并回写本人头像。
//
// @Summary      更新本人头像
// @Description  直传完成后调用，写入文件记录并回写 users.avatar_url。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  AvatarRequest  true  "头像确认请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/avatar [post]
func (h *Handler) UpdateAvatar(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req AvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f, err := h.files.ConfirmUpload(c.Request.Context(), service.ConfirmUploadInput{
		Scene:    model.FileSceneUserAvatar,
		Key:      req.Key,
		Format:   req.Format,
		Size:     req.Size,
		TargetID: u.ID,
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
		h.log.Error("update avatar", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}
