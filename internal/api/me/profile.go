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
	audit    *service.Auditor
	log      *slog.Logger
}

// NewHandler 构造。
func NewHandler(users *service.UserService, sessions *service.SessionService, clients *service.ClientService, audit *service.Auditor, logger *slog.Logger) *Handler {
	return &Handler{users: users, sessions: sessions, clients: clients, audit: audit, log: logger}
}

// Profile GET /api/v1/me
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
	OldPassword string `json:"oldPassword" binding:"required,min=8,max=128"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128"`
}

// ChangePassword 修改密码，成功后吊销全部会话。
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
