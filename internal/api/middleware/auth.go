package middleware

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// ContextKey 是 gin.Context 中存放认证结果的键。
const (
	CtxUser       = "auth.user"
	CtxSession    = "auth.session"
	SessionCookie = "sid"
)

// Auth 从 HttpOnly cookie 解析会话并加载用户。
// 浏览器永不持有 access_token / refresh_token（规范 §7 约束 4）。
func Auth(sessions *service.SessionService, st *store.Store, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !loadSession(c, sessions, st, logger, true) {
			return
		}
		c.Next()
	}
}

// OptionalAuth 尽力解析会话：无 cookie 或会话失效时不拦截。
// 用于 /auth/code 这类「未登录可调、但绑定类场景要求会话」的端点。
func OptionalAuth(sessions *service.SessionService, st *store.Store, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		loadSession(c, sessions, st, logger, false)
		c.Next()
	}
}

// loadSession 解析会话并注入当前用户；required 为 true 时失败即写响应并返回 false。
func loadSession(c *gin.Context, sessions *service.SessionService, st *store.Store, logger *slog.Logger, required bool) bool {
	deny := func(err *response.Error) bool {
		if required {
			response.Fail(c, err)
			return false
		}
		return true
	}
	raw, err := c.Cookie(SessionCookie)
	if err != nil || raw == "" {
		return deny(response.ErrUnauthenticated)
	}
	sess, err := sessions.Get(c.Request.Context(), raw)
	if err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			return deny(response.ErrUnauthenticated)
		}
		logger.Error("load session", "err", err)
		return deny(response.ErrInternal)
	}
	u, err := st.GetUserByID(c.Request.Context(), sess.UserID)
	if err != nil {
		return deny(response.ErrUnauthenticated)
	}
	if u.Status == model.UserStatusDisabled {
		return deny(response.ErrAccountDisabled)
	}
	c.Set(CtxUser, u)
	c.Set(CtxSession, sess)
	return true
}

// Admin 要求当前用户具备 admin 角色。
func Admin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		for _, r := range u.Roles {
			if r == "admin" {
				c.Next()
				return
			}
		}
		response.Fail(c, response.ErrForbidden)
	}
}

// CurrentUser 读取当前登录用户。
func CurrentUser(c *gin.Context) (*model.User, bool) {
	v, exists := c.Get(CtxUser)
	if !exists {
		return nil, false
	}
	u, ok := v.(*model.User)
	return u, ok
}

// CurrentSession 读取当前会话。
func CurrentSession(c *gin.Context) (*model.Session, bool) {
	v, exists := c.Get(CtxSession)
	if !exists {
		return nil, false
	}
	s, ok := v.(*model.Session)
	return s, ok
}
