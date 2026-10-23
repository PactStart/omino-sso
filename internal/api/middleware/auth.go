package middleware

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/pkg/cookie"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// ContextKey 是 gin.Context 中存放认证结果的键。
const (
	CtxUser       = "auth.user"
	CtxSession    = "auth.session"
	SessionCookie = cookie.Name
	// CtxClientID 门户应用管理请求的目标应用（006，由 AppAdmin 写入）。
	CtxClientID = "auth.clientId"
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
	// 把操作者写进 request context：service.Auditor 在事件未显式指定操作者时据此回填，
	// 使复用 admin handler 的门户侧操作也带 user_id / username（006 / FR-014）。
	c.Request = c.Request.WithContext(service.WithActor(c.Request.Context(), u.ID, u.Username))
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

// AppAdminChecker 是 AppAdmin 所需的判定能力，由 *service.AppAuthService 实现。
// 抽成接口只为让中间件可用假实现做单测（不需要数据库）。
type AppAdminChecker interface {
	AuthorizeAppAdmin(ctx context.Context, clientID, userID string) error
}

// AppAdmin 要求当前用户是该应用的应用管理员（006）。
//
// 拒绝语义统一为 403：应用不存在 / 非成员 / 非管理员 / 应用已停用一律返回同一响应，
// 具体原因只写应用日志，避免泄露应用是否存在（FR-010）。
// 身份判定每请求实时查库、不做任何缓存，因此授予/取消在下次请求即生效（FR-011）；
// 平台 admin 角色不自动放行（spec A2），门户与 admin 是两条独立的授权路径。
func AppAdmin(checker AppAdminChecker, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			response.Fail(c, response.ErrUnauthenticated)
			return
		}
		clientID := c.Param("id")
		if err := checker.AuthorizeAppAdmin(c.Request.Context(), clientID, u.ID); err != nil {
			logger.Warn("app admin denied",
				"clientId", clientID, "userId", u.ID, "reason", err.Error(), "path", c.FullPath())
			response.Fail(c, response.ErrForbidden)
			return
		}
		c.Set(CtxClientID, clientID)
		c.Next()
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
