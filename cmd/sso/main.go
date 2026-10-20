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
