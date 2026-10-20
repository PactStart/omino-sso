package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/pkg/trace"
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
	Detail    string
}

// Emit 非阻塞投递。
func (a *Auditor) Emit(ctx context.Context, e Event) {
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
	if e.Detail != "" {
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
