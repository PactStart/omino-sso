package me

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
)

// Handler 处理 /api/v1/me/*。
type Handler struct {
	users    *service.UserService
	sessions *service.SessionService
	clients  *service.ClientService
	files    *service.FileService
	audit    *service.Auditor
	log      *slog.Logger
}

// NewHandler 构造。
func NewHandler(users *service.UserService, sessions *service.SessionService, clients *service.ClientService, files *service.FileService, audit *service.Auditor, logger *slog.Logger) *Handler {
	return &Handler{users: users, sessions: sessions, clients: clients, files: files, audit: audit, log: logger}
}

// Profile GET /api/v1/me
//
// @Summary      查询当前用户资料
// @Description  返回会话所属用户的完整资料，含部门路径、角色与 MFA 状态。
// @Tags         我的
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body{data=dto.UserDetail}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me [get]
func (h *Handler) Profile(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	path, err := h.users.DeptPath(c.Request.Context(), u.DeptID)
	if err != nil {
		h.log.Warn("load dept path", "err", err)
	}
	response.Success(c, dto.Detail(u, path))
}

// Apps GET /api/v1/me/apps
//
// @Summary      查询我可访问的应用
// @Description  返回该用户有权访问的应用清单；lastAccessAt 为该用户最近一次通过该应用登录的时间，从未访问为 null。
// @Tags         我的
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body{data=[]dto.AppItem}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/apps [get]
func (h *Handler) Apps(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	items, err := h.clients.ListForUser(c.Request.Context(), u.ID)
	if err != nil {
		h.log.Error("list apps", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	list := make([]dto.AppItem, 0, len(items))
	for _, it := range items {
		list = append(list, dto.AppItem{
			ClientID:     it.ClientID,
			Name:         it.Name,
			Description:  it.Description,
			LogoURL:      it.LogoURL,
			HomeURL:      it.HomeURL,
			LastAccessAt: dto.TimePtr(it.LastAccessAt),
		})
	}
	response.Success(c, list)
}

// PasswordRequest POST /api/v1/me/password
type PasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required,min=8,max=128" example:"S3cure!pass"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128" example:"N3w!pass2026"`
}

// ChangePassword 修改密码，成功后吊销全部会话。
//
// @Summary      修改密码
// @Description  校验原密码后更新为新密码，成功后吊销该用户的全部会话并清除 sid cookie（需重新登录），同时写入 password_changed 审计事件。
// @Tags         我的
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  PasswordRequest  true  "改密请求体"
// @Success      200   {object}  response.Body  "已修改，需重新登录"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / WEAK_PASSWORD"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED / AUTH_INVALID_CREDENTIALS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/password [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req PasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	err := h.users.ChangePassword(c.Request.Context(), u.ID, req.OldPassword, req.NewPassword)
	switch {
	case err == nil:
	case err == service.ErrWeakPassword:
		response.Fail(c, response.ErrWeakPassword)
		return
	case err == service.ErrInvalidCredentials:
		response.Fail(c, response.ErrInvalidCredentials.WithMessage("原密码错误"))
		return
	case err == service.ErrSamePassword:
		response.Fail(c, response.ErrValidation.WithMessage("新密码不能与原密码相同"))
		return
	default:
		h.log.Error("change password", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: "password_changed", UserID: u.ID, Username: u.Username,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})
	http.SetCookie(c.Writer, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
	response.Success(c, nil)
}

// Sessions GET /api/v1/me/sessions
//
// @Summary      查询登录设备列表
// @Description  返回该用户全部有效会话；current 标识当前请求所用的会话，供"退出其他设备"场景使用。
// @Tags         我的
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body{data=[]dto.SessionItem}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/sessions [get]
func (h *Handler) Sessions(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	cur, _ := middleware.CurrentSession(c)
	list, err := h.sessions.List(c.Request.Context(), u.ID)
	if err != nil {
		h.log.Error("list sessions", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := make([]dto.SessionItem, 0, len(list))
	for i := range list {
		s := list[i]
		out = append(out, dto.SessionItem{
			SID:        s.SID,
			IP:         s.IP,
			UserAgent:  s.UserAgent,
			CreatedAt:  dto.Time(s.CreatedAt),
			LastSeenAt: dto.Time(s.LastSeenAt),
			Current:    cur != nil && cur.SID == s.SID,
		})
	}
	response.Success(c, out)
}

// RevokeSession DELETE /api/v1/me/sessions/:sid
//
// @Summary      撤销指定会话
// @Description  吊销本人名下指定 sid 的服务端会话，立即失效；该 sid 不属于当前用户时按 NOT_FOUND 处理，不泄露是否存在。
// @Tags         我的
// @Produce      json
// @Security     cookieAuth
// @Param        sid   path  string  true  "会话标识"
// @Success      200   {object}  response.Body  "已撤销"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @Router       /me/sessions/{sid} [delete]
func (h *Handler) RevokeSession(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	sid := c.Param("sid")
	list, err := h.sessions.List(c.Request.Context(), u.ID)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	found := false
	for _, s := range list {
		if s.SID == sid {
			found = true
			break
		}
	}
	if !found {
		response.Fail(c, response.ErrNotFound)
		return
	}
	if err := h.sessions.Revoke(c.Request.Context(), sid); err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, nil)
}
