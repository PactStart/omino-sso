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
		raw, err := c.Cookie(SessionCookie)
		if err != nil || raw == "" {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		sess, err := sessions.Get(c.Request.Context(), raw)
		if err != nil {
			if errors.Is(err, store.ErrSessionNotFound) {
				response.Fail(c, response.ErrUnauthenticated)
				return
			}
			logger.Error("load session", "err", err)
			response.Fail(c, response.ErrInternal)
			return
		}
		u, err := st.GetUserByID(c.Request.Context(), sess.UserID)
		if err != nil {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		if u.Status == model.UserStatusDisabled {
			response.Fail(c, response.ErrAccountDisabled)
			return
		}
		c.Set(CtxUser, u)
		c.Set(CtxSession, sess)
		c.Next()
	}
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
