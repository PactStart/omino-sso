package api

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/admin"
	"github.com/omino/omino-sso/internal/api/auth"
	"github.com/omino/omino-sso/internal/api/docs"
	"github.com/omino/omino-sso/internal/api/files"
	"github.com/omino/omino-sso/internal/api/me"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
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
	appAuth *service.AppAuthService,
	fileSvc *service.FileService,
	notif *notification.Service,
	captcha *service.CaptchaService,
	mfaSvc *service.MFAService,
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

	authH := auth.NewHandler(cfg, users, sessions, notif, captcha, audit, logger)
	meH := me.NewHandler(users, sessions, clients, fileSvc, mfaSvc, audit, logger)
	adminH := admin.NewHandler(users, clients, orgs, appAuth, fileSvc, notif, audit, logger)
	filesH := files.NewHandler(fileSvc, logger)

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
	v1.POST("/auth/password/reset", middleware.RateLimit(st.Redis(), "resetpwd", cfg.RateLimitLoginPerMin, time.Minute), authH.ResetPassword)
	// 图形验证码：未登录的验证码登录与找回密码必须先取图形码（005 边界用例，防刷/防枚举）。
	v1.GET("/auth/captcha", middleware.RateLimit(st.Redis(), "captcha", cfg.RateLimitLoginPerMin, time.Minute), authH.Captcha)
	// 验证码发送：未登录可调，但 bind_* / mfa_unbind 场景在处理器内要求会话（OptionalAuth 只做尽力解析）。
	v1.POST("/auth/code", middleware.RateLimit(st.Redis(), "verify", cfg.RateLimitLoginPerMin, time.Minute),
		middleware.OptionalAuth(sessions, st, logger), authH.SendCode)

	// 需登录
	authed := v1.Group("")
	authed.Use(middleware.Auth(sessions, st, logger))
	{
		authed.POST("/auth/logout", authH.Logout)
		authed.GET("/me", meH.Profile)
		authed.GET("/me/apps", meH.Apps)
		authed.POST("/me/password", meH.ChangePassword)
		authed.POST("/me/password/reset", meH.ResetPassword)
		// 联系方式绑定/修改与 MFA 管理（005）。
		authed.POST("/me/phone", meH.BindPhone)
		authed.POST("/me/email", meH.BindEmail)
		authed.POST("/me/mfa/bind", meH.StartMFABind)
		authed.POST("/me/mfa/bind/confirm", meH.ConfirmMFABind)
		authed.POST("/me/mfa/unbind", meH.UnbindMFA)
		authed.GET("/me/sessions", meH.Sessions)
		authed.DELETE("/me/sessions/:sid", meH.RevokeSession)
		authed.POST("/me/avatar", meH.UpdateAvatar)
		authed.DELETE("/me/avatar", meH.ClearAvatar)
		authed.POST("/files/upload-credential", filesH.CreateUploadCredential)
		authed.POST("/files/confirm", filesH.ConfirmUpload)
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
		adminGroup.GET("/clients/:id", adminH.GetClient)
		adminGroup.PATCH("/clients/:id", adminH.UpdateClient)

		// 应用内成员 / 角色 / 权限 / 授权（003）。
		adminGroup.GET("/clients/:id/members", adminH.ListMembers)
		adminGroup.POST("/clients/:id/members", adminH.AddMembers)
		adminGroup.POST("/clients/:id/members/remove", adminH.RemoveMembers)
		adminGroup.GET("/clients/:id/members/candidates", adminH.ListCandidates)
		adminGroup.GET("/clients/:id/roles", adminH.ListRoles)
		adminGroup.POST("/clients/:id/roles", adminH.CreateRole)
		adminGroup.GET("/clients/:id/roles/:rid", adminH.GetRole)
		adminGroup.PATCH("/clients/:id/roles/:rid", adminH.UpdateRole)
		adminGroup.DELETE("/clients/:id/roles/:rid", adminH.DeleteRole)
		adminGroup.PUT("/clients/:id/roles/:rid/permissions", adminH.PutRolePermissions)
		adminGroup.PUT("/clients/:id/roles/:rid/members", adminH.PutRoleMembers)
		adminGroup.GET("/clients/:id/permissions", adminH.ListPermissions)
		adminGroup.POST("/clients/:id/permissions", adminH.CreatePermission)
		adminGroup.PATCH("/clients/:id/permissions/:pid", adminH.UpdatePermission)
		adminGroup.DELETE("/clients/:id/permissions/:pid", adminH.DeletePermission)

		adminGroup.GET("/audit-logs", adminH.ListAuditLogs)
		adminGroup.GET("/send-records", adminH.ListSendRecords)

		// 图片上传（004）。
		adminGroup.POST("/clients/:id/logo", adminH.SetClientLogo)
		adminGroup.DELETE("/clients/:id/logo", adminH.ClearClientLogo)
		adminGroup.POST("/users/:id/avatar", adminH.SetUserAvatar)
		adminGroup.DELETE("/users/:id/avatar", adminH.ClearUserAvatar)
		adminGroup.GET("/files", adminH.ListFiles)
		adminGroup.PATCH("/files/:id", adminH.UpdateFileStatus)
	}

	// 未匹配路由与方法也必须返回统一格式（规范 §5）。
	r.NoRoute(func(c *gin.Context) { response.Fail(c, response.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) { response.Fail(c, response.ErrNotFound) })

	return r
}
