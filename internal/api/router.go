package api

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/admin"
	"github.com/omino/omino-sso/internal/api/auth"
	"github.com/omino/omino-sso/internal/api/docs"
	"github.com/omino/omino-sso/internal/api/me"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
)

// NewRouter 构造管理 API 路由。
// 中间件顺序固定：recover → trace → 限流 → 认证（规范 §3）。
func NewRouter(
	cfg *config.Config,
	st *store.Store,
	users *service.UserService,
	sessions *service.SessionService,
	clients *service.ClientService,
	orgs *service.OrgService,
	audit *service.Auditor,
	logger *slog.Logger,
) *gin.Engine {
	if cfg.AppEnv == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.HandleMethodNotAllowed = true
	r.Use(middleware.Recover(logger))
	r.Use(middleware.Trace())
	r.Use(middleware.RateLimit(st.Redis(), "api", cfg.RateLimitAPIPerMin, time.Minute))

	authH := auth.NewHandler(cfg, users, sessions, audit, logger)
	meH := me.NewHandler(users, sessions, clients, audit, logger)
	adminH := admin.NewHandler(users, clients, orgs, audit, logger)

	// 接口文档（规范 §4.0）：仅非 prod 注册，且不套统一响应、不挂限流。
	// prod 下该分支不执行，请求落入 NoRoute，响应与任意不存在路径完全一致。
	// 环境判定只此一处（FR-017）。
	if cfg.AppEnv != "prod" {
		r.GET(docs.RoutePrefix+"/*any", docs.Handler(cfg.AppEnv))
	}

	v1 := r.Group("/api/v1")

	// 公开端点
	v1.POST("/auth/login", middleware.RateLimit(st.Redis(), "login", cfg.RateLimitLoginPerMin, time.Minute), authH.Login)
	v1.POST("/auth/mfa/verify", middleware.RateLimit(st.Redis(), "mfa", cfg.RateLimitLoginPerMin, time.Minute), authH.VerifyMFA)

	// 需登录
	authed := v1.Group("")
	authed.Use(middleware.Auth(sessions, st, logger))
	{
		authed.POST("/auth/logout", authH.Logout)
		authed.GET("/me", meH.Profile)
		authed.GET("/me/apps", meH.Apps)
		authed.POST("/me/password", meH.ChangePassword)
		authed.GET("/me/sessions", meH.Sessions)
		authed.DELETE("/me/sessions/:sid", meH.RevokeSession)
	}

	// 管理端点
	adminGroup := v1.Group("/admin")
	adminGroup.Use(middleware.Auth(sessions, st, logger), middleware.Admin())
	{
		adminGroup.GET("/users", adminH.ListUsers)
		adminGroup.POST("/users", adminH.CreateUser)
		adminGroup.PATCH("/users/:id", adminH.UpdateUser)
		adminGroup.GET("/orgs/tree", adminH.OrgTree)
		adminGroup.POST("/orgs", adminH.CreateOrg)
		adminGroup.PATCH("/orgs/:id", adminH.UpdateOrg)
		adminGroup.DELETE("/orgs/:id", adminH.DeleteOrg)
		adminGroup.GET("/clients", adminH.ListClients)
		adminGroup.POST("/clients", adminH.CreateClient)
		adminGroup.POST("/clients/:id/secret", adminH.RotateSecret)
		adminGroup.GET("/audit-logs", adminH.ListAuditLogs)
	}

	// 未匹配路由与方法也必须返回统一格式（规范 §5）。
	r.NoRoute(func(c *gin.Context) { response.Fail(c, response.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) { response.Fail(c, response.ErrNotFound) })

	return r
}
