package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store"
)

// cleanupBatch 单批删除行数，配合循环避免长事务与长锁（决策 8）。
const cleanupBatch = 500

// CleanupService 内置定时清理：每日删除超过保留期的发送记录与审计日志。
// 不引入外部定时任务框架；单实例 IdP 由进程内 ticker 完成即可。
type CleanupService struct {
	st        *store.Store
	log       *slog.Logger
	retention time.Duration
	interval  time.Duration
}

// NewCleanupService 构造清理服务。
func NewCleanupService(st *store.Store, cfg *config.Config, logger *slog.Logger) *CleanupService {
	if logger == nil {
		logger = slog.Default()
	}
	days := 180
	if cfg != nil && cfg.Verify.RecordRetentionDays > 0 {
		days = cfg.Verify.RecordRetentionDays
	}
	return &CleanupService{
		st:        st,
		log:       logger,
		retention: time.Duration(days) * 24 * time.Hour,
		interval:  24 * time.Hour,
	}
}

// Run 阻塞运行直至 ctx 取消：启动后先执行一次，随后每日执行。
func (c *CleanupService) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.Cleanup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Cleanup(ctx)
		}
	}
}

// Cleanup 执行一次清理，逐表记录删除行数与耗时。
func (c *CleanupService) Cleanup(ctx context.Context) {
	before := time.Now().Add(-c.retention).UnixMilli()

	start := time.Now()
	n, err := c.st.DeleteSendRecordsBefore(ctx, before, cleanupBatch)
	c.logResult(ctx, "send_records", n, start, err)

	start = time.Now()
	n, err = c.st.DeleteAuditEventsBefore(ctx, before, cleanupBatch)
	c.logResult(ctx, "audit_events", n, start, err)
}

func (c *CleanupService) logResult(ctx context.Context, table string, rows int64, start time.Time, err error) {
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		c.log.ErrorContext(ctx, "cleanup failed",
			"table", table, "rows", rows, "elapsed_ms", elapsed, "err", err)
		return
	}
	c.log.InfoContext(ctx, "cleanup done",
		"table", table, "rows", rows, "elapsed_ms", elapsed,
		"retention_days", int(c.retention.Hours()/24))
}
