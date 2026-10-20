// @title           omino-sso 管理 API
// @version         1.0
// @description     企业统一身份提供方的管理接口。会话通过 HttpOnly cookie `sid` 携带；响应统一为 {code,message,data,traceId}。
// @description     协议端点（/oauth2/*、/.well-known/*）不在本文档范围内，见 /.well-known/openid-configuration
// @description     通用失败路径（不属任何单个端点，故统一记录在此）：未匹配的路由或方法返回 404 {"code":"NOT_FOUND","message":"资源不存在","data":null,"traceId":"<ULID>"}；超出限流返回 429 {"code":"RATE_LIMITED","message":"请求过于频繁","data":null,"traceId":"<ULID>"}——429 仅在 /auth/login 与 /auth/mfa/verify 上做端点级声明，其余端点虽受全局限流，但为避免逐条重复不再列出。
// @description     /docs/* 属文档面，返回原始 HTML/JSON，不套统一响应；APP_ENV=prod 时该前缀不注册，响应与任意不存在路径完全一致。
// @BasePath        /api/v1
// @tag.name        认证
// @tag.description 登录、登出与 MFA 二次校验
// @tag.name        我的
// @tag.description 当前用户的资料、密码、可访问应用与登录设备
// @tag.name        用户管理
// @tag.description 管理端用户的查询、创建与更新
// @tag.name        组织架构
// @tag.description 组织树与各部门人数
// @tag.name        应用管理
// @tag.description OAuth 客户端注册与密钥轮换
// @tag.name        审计
// @tag.description 认证审计日志检索
// @securityDefinitions.apikey cookieAuth
// @in              cookie
// @name            sid
// @description     登录成功后由服务端下发的会话 cookie
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zitadel/oidc/v3/pkg/op"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/api"
	"github.com/omino/omino-sso/internal/bootstrap"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/oidc"
	"github.com/omino/omino-sso/internal/pkg/log"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		println("config error:", err.Error())
		os.Exit(1)
	}
	logger := log.New(cfg.LogLevel)

	app := fx.New(
		fx.Supply(cfg, logger),
		fx.Provide(
			newDB,
			newRedis,
			store.New,
			newAuditor,
			service.NewUserService,
			service.NewSessionService,
			service.NewClientService,
			newKeyManager,
			oidc.NewStorage,
			oidc.NewProvider,
		),
		fx.Invoke(runAuditor),
		fx.Invoke(runServers),
		fx.Invoke(seedData),
	)
	app.Run()
}

func newAuditor(st *store.Store, cfg *config.Config, logger *slog.Logger) *service.Auditor {
	return service.NewAuditor(st, cfg.AuditAsyncBuffer, logger)
}

func newDB(cfg *config.Config) (*gorm.DB, error) {
	return store.Open(&cfg.DB)
}

func newRedis(cfg *config.Config) (*redis.Client, error) {
	return store.NewRedis(&cfg.Redis)
}

func newKeyManager(cfg *config.Config) (*oidc.KeyManager, error) {
	return oidc.LoadKeyManager(cfg.SigningKeyPath, cfg.SigningKeyID)
}

// runAuditor 启动审计消费者。
func runAuditor(lc fx.Lifecycle, a *service.Auditor) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go a.Run(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

// runServers 同时启动协议层（:8080）与管理 API（:8081），两者物理分离。
func runServers(
	lc fx.Lifecycle,
	cfg *config.Config,
	logger *slog.Logger,
	provider *op.Provider,
	oidcStorage *oidc.Storage,
	st *store.Store,
	users *service.UserService,
	sessions *service.SessionService,
	clients *service.ClientService,
	audit *service.Auditor,
) {
	oidc.NewServer(lc, cfg, provider, oidcStorage, st, users, sessions, clients, audit, logger)

	router := api.NewRouter(cfg, st, users, sessions, clients, audit, logger)
	api.NewServer(lc, cfg, router, logger)
}

func seedData(lc fx.Lifecycle, cfg *config.Config, st *store.Store, logger *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return bootstrap.Seed(ctx, cfg, st, logger)
		},
	})
}

var (
	_ = http.StatusOK
)
