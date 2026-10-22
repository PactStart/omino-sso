package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/pkg/trace"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// Auditor 异步写入审计事件（规范 §9 AUDIT_ASYNC_BUFFER）。
// 队列满时丢弃并告警，绝不阻塞认证主流程。
type Auditor struct {
	st  *store.Store
	ch  chan *model.AuditEvent
	log *slog.Logger
}

// NewAuditor 构造审计器。
func NewAuditor(st *store.Store, buffer int, logger *slog.Logger) *Auditor {
	if buffer <= 0 {
		buffer = 1024
	}
	return &Auditor{st: st, ch: make(chan *model.AuditEvent, buffer), log: logger}
}

// Event 描述一次待记录的审计事件。
type Event struct {
	Type      string
	UserID    string
	Username  string
	ClientID  string
	IP        string
	UserAgent string
	Result    string
	// Detail 为简单文本原因，写入 detail 列时包成 {"message": "..."}。
	Detail string
	// Fields 为结构化变更摘要（如组织写操作的 orgId/name/parentId），
	// 非空时优先于 Detail，直接序列化为 detail 列（规范 §6.7）。
	Fields map[string]any
}

// actorKey 是 context 中操作者的键（006-app-admin-role）。
type actorKey struct{}

// WithActor 把当前操作者写入 ctx。由认证中间件在解析出会话用户后调用，
// Auditor.Emit 在事件未显式指定操作者时据此补齐 —— 这样门户侧复用 admin
// 端 handler 的成员/角色/权限操作也能追溯到「谁做的」（FR-014 / SC-006）。
func WithActor(ctx context.Context, userID, username string) context.Context {
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, [2]string{userID, username})
}

// actorFrom 读取 ctx 中的操作者；缺失时返回空串。
func actorFrom(ctx context.Context) (userID, username string) {
	if v, ok := ctx.Value(actorKey{}).([2]string); ok {
		return v[0], v[1]
	}
	return "", ""
}

// Emit 非阻塞投递。
func (a *Auditor) Emit(ctx context.Context, e Event) {
	// 事件未指定操作者时，回填认证中间件注入的当前用户（显式值优先）。
	if e.UserID == "" {
		if uid, name := actorFrom(ctx); uid != "" {
			e.UserID, e.Username = uid, name
		}
	}
	ev := &model.AuditEvent{
		ID:        id.New(),
		EventType: e.Type,
		IP:        e.IP,
		Result:    e.Result,
		CreatedAt: nowMillis(),
	}
	if e.UserID != "" {
		ev.UserID = &e.UserID
	}
	if e.Username != "" {
		ev.Username = &e.Username
	}
	if e.ClientID != "" {
		ev.ClientID = &e.ClientID
	}
	if e.UserAgent != "" {
		ev.UserAgent = &e.UserAgent
	}
	if len(e.Fields) > 0 {
		if raw, err := json.Marshal(e.Fields); err == nil {
			v := string(raw)
			ev.Detail = &v
		}
	} else if e.Detail != "" {
		// detail 列为 JSON，字符串必须序列化为合法 JSON 文本。
		raw, err := json.Marshal(map[string]string{"message": e.Detail})
		if err == nil {
			v := string(raw)
			ev.Detail = &v
		}
	}
	if tid := trace.FromContext(ctx); tid != "" {
		ev.TraceID = &tid
	}
	select {
	case a.ch <- ev:
	default:
		a.log.Warn("audit queue full, event dropped", "event_type", e.Type)
	}
}

// VerificationCodeSent 实现 notification.AuditSink：验证码发送审计（005）。
func (a *Auditor) VerificationCodeSent(ctx context.Context, e notification.VerificationSentEvent) {
	a.Emit(ctx, Event{
		Type:      model.EventVerificationCodeSent,
		UserID:    e.UserID,
		IP:        e.IP,
		UserAgent: e.UserAgent,
		Result:    e.Result,
		Fields: map[string]any{
			"channel":  e.Channel,
			"scene":    e.Scene,
			"target":   e.Target,
			"provider": e.Provider,
		},
	})
}

// AuditFilter 审计日志筛选条件。
// api 层只构造该结构并调用 service，不得直接持有 store（宪法原则 III）。
type AuditFilter struct {
	UserID    string
	ClientID  string
	EventType string
	Result    string
	From      int64
	To        int64
	Page      int
	PageSize  int
}

// List 分页查询审计事件，返回当页数据与总数。
func (a *Auditor) List(ctx context.Context, f AuditFilter) ([]model.AuditEvent, int64, error) {
	return a.st.ListAuditEvents(ctx, store.AuditFilter{
		UserID:    f.UserID,
		ClientID:  f.ClientID,
		EventType: f.EventType,
		Result:    f.Result,
		From:      f.From,
		To:        f.To,
		Page:      f.Page,
		PageSize:  f.PageSize,
	})
}

// Run 消费队列，直到 ctx 取消。
func (a *Auditor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-a.ch:
			if err := a.st.InsertAuditEvent(context.WithoutCancel(ctx), ev); err != nil {
				a.log.Error("write audit event failed", "err", err, "event_type", ev.EventType)
			}
		}
	}
}
