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
	// 仅信任配置声明的反代网段；为空则不信任任何代理（ClientIP 退化为 RemoteAddr）。
	// 多域名部署下 XFF 有多跳，网段漏配会让审计 IP 变成上游容器地址（FR-014）。
	if err := r.SetTrustedProxies(cfg.TrustedProxyCIDRs()); err != nil {
		// 配置已在启动时按 CIDR 校验过，这里只可能是 gin 侧的极端输入，直接退化。
		logger.Error("set trusted proxies failed, fallback to no proxy", "err", err)
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(middleware.Recover(logger))
	r.Use(middleware.Trace())
	r.Use(middleware.RateLimit(st.Redis(), "api", cfg.RateLimitAPIPerMin, time.Minute))

	// 运维探针（规范 §4.0）：挂在根路径、不套统一响应、不进 swagger、不参与限流。
	// 必须在 /api/v1 分组之外注册，否则会落进业务契约。
	r.GET("/healthz", healthzHandler())
	r.GET("/readyz", readyzHandler(st))

	authH := auth.NewHandler(cfg, users, sessions, notif, captcha, audit, logger)
	adminH := admin.NewHandler(users, clients, orgs, appAuth, fileSvc, notif, audit, logger)
	// 门户的成员/角色/权限端点转交给 adminH（业务逻辑单一实现，006-app-admin-role）。
	meH := me.NewHandler(cfg, users, sessions, clients, fileSvc, mfaSvc, appAuth, adminH, audit, logger)
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
		// 只读组织树：门户应用管理页的部门筛选与「按部门加入」需要（006）。
		// 组织写操作仍只挂在 adminGroup 下，门户不注册任何写端点。
		authed.GET("/me/orgs/tree", meH.AppOrgTree)
		authed.POST("/files/upload-credential", filesH.CreateUploadCredential)
		authed.POST("/files/confirm", filesH.ConfirmUpload)
	}

	// 门户应用管理（006）：只认「该应用的应用管理员」，不挂 Admin()。
	// 平台管理员不因平台身份获得门户侧权限（spec A2），其路径是上面的 adminGroup。
	// 应用信息只开放「名称 / 描述 / Logo / 回调地址」四个可编辑字段（白名单在
	// ClientService.UpdateBasicInfo）；密钥、设为管理员、登出地址、TTL、启停仍只在 admin 端，
	// 未注册的路径（密钥、设为管理员）一律落入 NoRoute。
	// 摘要与详情端点单独注册（路径不得为空字面量，否则 scripts/check-registry.sh 无法解析）。
	authed.GET("/me/apps/:id", middleware.AppAdmin(appAuth, logger), meH.AppManageContext)
	authed.GET("/me/apps/:id/detail", middleware.AppAdmin(appAuth, logger), meH.AppClientDetail)
	authed.PATCH("/me/apps/:id", middleware.AppAdmin(appAuth, logger), meH.AppUpdateClient)

	appAdminGroup := authed.Group("/me/apps/:id")
	appAdminGroup.Use(middleware.AppAdmin(appAuth, logger))
	{
		appAdminGroup.POST("/logo", meH.AppSetClientLogo)
		appAdminGroup.DELETE("/logo", meH.AppClearClientLogo)
		appAdminGroup.GET("/members", meH.AppListMembers)
		appAdminGroup.GET("/members/candidates", meH.AppListCandidates)
		appAdminGroup.POST("/members", meH.AppAddMembers)
		appAdminGroup.POST("/members/remove", meH.AppRemoveMembers)
		appAdminGroup.GET("/roles", meH.AppListRoles)
		appAdminGroup.POST("/roles", meH.AppCreateRole)
		appAdminGroup.GET("/roles/:rid", meH.AppGetRole)
		appAdminGroup.PATCH("/roles/:rid", meH.AppUpdateRole)
		appAdminGroup.DELETE("/roles/:rid", meH.AppDeleteRole)
		appAdminGroup.PUT("/roles/:rid/permissions", meH.AppPutRolePermissions)
		appAdminGroup.PUT("/roles/:rid/members", meH.AppPutRoleMembers)
		appAdminGroup.GET("/permissions", meH.AppListPermissions)
		appAdminGroup.POST("/permissions", meH.AppCreatePermission)
		appAdminGroup.PATCH("/permissions/:pid", meH.AppUpdatePermission)
		appAdminGroup.DELETE("/permissions/:pid", meH.AppDeletePermission)
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
		// 授予/取消应用管理员（006）：入口只在 admin 端，门户不注册该路径。
		adminGroup.PUT("/clients/:id/members/:uid/admin", adminH.SetMemberAdmin)
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
