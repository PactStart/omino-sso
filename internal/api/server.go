package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/omino/omino-sso/internal/config"
)

// NewServer 管理 API 独立 http.Server（:8081），与协议层物理分离。
func NewServer(lc fx.Lifecycle, cfg *config.Config, router *gin.Engine, logger *slog.Logger) *http.Server {
	srv := &http.Server{
		Addr:         cfg.HTTPAPIAddr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("admin api listening", "addr", cfg.HTTPAPIAddr)
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("api server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		},
	})
	return srv
}
