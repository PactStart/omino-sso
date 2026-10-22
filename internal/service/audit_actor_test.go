package service

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/omino/omino-sso/internal/store/model"
)

func newQueueAuditor() *Auditor {
	return &Auditor{
		ch:  make(chan *model.AuditEvent, 4),
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// 006 / FR-014：未显式指定操作者时，Emit 用认证中间件注入的 actor 回填，
// 使门户侧复用 admin handler 的成员/角色/权限操作也能追溯到「谁做的」。
func TestEmitFillsActorFromContext(t *testing.T) {
	a := newQueueAuditor()
	ctx := WithActor(context.Background(), "01M5HMP2RCCP3CGT4ZX3Z3Y6ZW", "admin")

	a.Emit(ctx, Event{
		Type: model.EventAppMemberAdded, ClientID: "demo-app",
		IP: "127.0.0.1", Result: "success",
	})

	select {
	case ev := <-a.ch:
		if ev.UserID == nil || *ev.UserID != "01M5HMP2RCCP3CGT4ZX3Z3Y6ZW" {
			t.Fatalf("user_id = %v, 期望从 ctx 回填", ev.UserID)
		}
		if ev.Username == nil || *ev.Username != "admin" {
			t.Fatalf("username = %v, 期望从 ctx 回填", ev.Username)
		}
	default:
		t.Fatal("事件未入队")
	}
}

// 显式指定的操作者优先于 ctx（例如系统任务写系统账号）。
func TestEmitKeepsExplicitActor(t *testing.T) {
	a := newQueueAuditor()
	ctx := WithActor(context.Background(), "ctx-user", "ctx-name")

	a.Emit(ctx, Event{
		Type: model.EventAppMemberAdded, UserID: "explicit-user", Username: "explicit-name",
		IP: "127.0.0.1", Result: "success",
	})

	ev := <-a.ch
	if ev.UserID == nil || *ev.UserID != "explicit-user" {
		t.Fatalf("user_id = %v, 期望保留显式值", ev.UserID)
	}
	if ev.Username == nil || *ev.Username != "explicit-name" {
		t.Fatalf("username = %v, 期望保留显式值", ev.Username)
	}
}

// 未注入 actor 时不得凭空产生操作者（未登录场景仍为空）。
func TestEmitWithoutActorStaysEmpty(t *testing.T) {
	a := newQueueAuditor()
	a.Emit(context.Background(), Event{Type: model.EventAppMemberAdded, IP: "127.0.0.1", Result: "success"})

	ev := <-a.ch
	if ev.UserID != nil || ev.Username != nil {
		t.Fatalf("user_id/username = %v/%v, 期望为空", ev.UserID, ev.Username)
	}
}
