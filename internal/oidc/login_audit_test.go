package oidc

// 「凭既有会话直接完成授权」审计事件的构造口径（FR-010 / FR-011）。
//
// 只测构造（ssoLoginEvent），不测 Auditor 的异步落库：后者由 store 层与 005 已交付的
// 队列语义保证，整条链路（含 management API 查询）由 examples.sh 的 A4 后置断言覆盖。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/omino/omino-sso/internal/store/model"
)

func TestSSOLoginEventPortalEntry(t *testing.T) {
	ev := ssoLoginEvent("u1", "alice", "client-1", "203.0.113.7", "curl/8", []string{"pwd", "otp"}, true)

	if ev.Type != model.EventSSOLoginSuccess {
		t.Fatalf("Type = %q, want %q (%q would pollute login_success stats)",
			ev.Type, model.EventSSOLoginSuccess, model.EventLoginSuccess)
	}
	if ev.Result != "success" {
		t.Fatalf("Result = %q, want success", ev.Result)
	}
	if ev.UserID != "u1" || ev.Username != "alice" || ev.ClientID != "client-1" {
		t.Fatalf("identity fields wrong: %+v", ev)
	}
	if ev.IP != "203.0.113.7" || ev.UserAgent != "curl/8" {
		t.Fatalf("source fields wrong: %+v", ev)
	}
	if got := ev.Fields["entry"]; got != "portal" {
		t.Fatalf("entry = %v, want portal", got)
	}
	amr, ok := ev.Fields["amr"].([]string)
	if !ok || len(amr) != 2 || amr[0] != "pwd" || amr[1] != "otp" {
		t.Fatalf("amr = %#v, want [pwd otp]", ev.Fields["amr"])
	}
}

func TestSSOLoginEventTemplateEntry(t *testing.T) {
	ev := ssoLoginEvent("u1", "", "client-1", "", "", nil, false)
	if got := ev.Fields["entry"]; got != "template" {
		t.Fatalf("entry = %v, want template", got)
	}
	// amr 为 nil 时必须序列化成 []，而不是 null（前端表格按数组渲染）。
	raw, err := json.Marshal(ev.Fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"amr":null`) {
		t.Fatalf("amr must not be null: %s", raw)
	}
}

// 事件 detail 里不得出现任何凭据类字段（宪法 IV）。
func TestSSOLoginEventCarriesNoSecrets(t *testing.T) {
	ev := ssoLoginEvent("u1", "alice", "client-1", "203.0.113.7", "curl/8", []string{"pwd"}, true)
	raw, err := json.Marshal(ev.Fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	lowered := strings.ToLower(string(raw))
	for _, banned := range []string{"sid", "token", "password", "secret", "code"} {
		if strings.Contains(lowered, banned) {
			t.Fatalf("detail leaks %q: %s", banned, raw)
		}
	}
}
