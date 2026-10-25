package oidc

import (
	"net/url"
	"strings"
	"testing"
)

// decideStep 的 16 种输入组合逐一锁定。重点：
//   - 请求不存在 + 标记 → 过期提示页；无标记 → 既有 400；
//   - 有会话时，无论有没有标记、有没有配置门户，都必须 StepComplete（FR-009）；
//   - 未配置门户时永远是内置页（含带标记的情况，FR-005）；
//   - 配置了门户 + 带标记 + 无会话 → 降级（FR-006，防死循环）。
func TestDecideStep(t *testing.T) {
	cases := []struct {
		name                                  string
		reqExists, hasSession, portal, marker bool
		want                                  LoginStep
	}{
		{"请求不存在/无标记", false, false, false, false, StepBadRequest},
		{"请求不存在/无标记/已配置", false, false, true, false, StepBadRequest},
		{"请求不存在/有会话", false, true, true, false, StepBadRequest},
		{"请求不存在/带标记", false, false, true, true, StepRequestExpired},
		{"请求不存在/带标记/未配置", false, false, false, true, StepRequestExpired},
		{"请求不存在/带标记/有会话", false, true, true, true, StepRequestExpired},

		{"有会话/未配置/无标记", true, true, false, false, StepComplete},
		{"有会话/未配置/带标记", true, true, false, true, StepComplete},
		{"有会话/已配置/无标记", true, true, true, false, StepComplete},
		{"有会话/已配置/带标记", true, true, true, true, StepComplete},

		{"无会话/未配置/无标记", true, false, false, false, StepTemplate},
		{"无会话/未配置/带标记", true, false, false, true, StepTemplate},

		{"无会话/已配置/无标记", true, false, true, false, StepPortal},
		{"无会话/已配置/带标记", true, false, true, true, StepTemplateDegraded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideStep(c.reqExists, c.hasSession, c.portal, c.marker); got != c.want {
				t.Fatalf("decideStep(%v,%v,%v,%v) = %v, want %v",
					c.reqExists, c.hasSession, c.portal, c.marker, got, c.want)
			}
		})
	}
}

func TestPortalRedirectURL(t *testing.T) {
	const issuer = "https://sso.example.com"

	t.Run("基本形态", func(t *testing.T) {
		got, err := portalRedirectURL("https://portal.example.com/login", issuer, "01ABC", "CRM")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("result not a url: %v", err)
		}
		if u.Scheme != "https" || u.Host != "portal.example.com" || u.Path != "/login" {
			t.Fatalf("base changed: %q", got)
		}
		if app := u.Query().Get("app"); app != "CRM" {
			t.Fatalf("app = %q, want CRM", app)
		}
		// next 解码一次后必须是一个可直接整页跳转的绝对地址，且带标记。
		next, err := url.Parse(u.Query().Get("next"))
		if err != nil {
			t.Fatalf("next not a url: %v", err)
		}
		if next.Scheme != "https" || next.Host != "sso.example.com" || next.Path != "/login" {
			t.Fatalf("next = %q, want %s/login", next.String(), issuer)
		}
		if id := next.Query().Get("id"); id != "01ABC" {
			t.Fatalf("next id = %q, want 01ABC (double encoding would break this)", id)
		}
		if m := next.Query().Get(portalMarkerParam); m != portalMarkerValue {
			t.Fatalf("next marker = %q, want %q", m, portalMarkerValue)
		}
	})

	t.Run("保留原有查询串", func(t *testing.T) {
		got, err := portalRedirectURL("https://portal.example.com/login?from=sso", issuer, "X", "CRM")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, _ := url.Parse(got)
		if v := u.Query().Get("from"); v != "sso" {
			t.Fatalf("from = %q, want sso (base query must be preserved)", v)
		}
	})

	t.Run("应用名为空则不下发 app", func(t *testing.T) {
		got, err := portalRedirectURL("https://portal.example.com/login", issuer, "X", "  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, _ := url.Parse(got)
		if _, ok := u.Query()["app"]; ok {
			t.Fatalf("app should be absent, got %q", u.RawQuery)
		}
	})

	t.Run("请求 ID 含特殊字符只编码一次", func(t *testing.T) {
		got, err := portalRedirectURL("https://portal.example.com/login", issuer, "a b&c=d", "CRM")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, _ := url.Parse(got)
		next, err := url.Parse(u.Query().Get("next"))
		if err != nil {
			t.Fatalf("next not a url: %v", err)
		}
		if id := next.Query().Get("id"); id != "a b&c=d" {
			t.Fatalf("next id = %q, want round-trip of %q", id, "a b&c=d")
		}
	})

	t.Run("非法 base 报错", func(t *testing.T) {
		for _, base := range []string{
			"",
			"   ",
			"portal.example.com/login",
			"https:///login",
			"https://portal.example.com/login#frag",
			"ftp://portal.example.com/login",
		} {
			if _, err := portalRedirectURL(base, issuer, "X", "CRM"); err == nil {
				t.Fatalf("base %q should be rejected", base)
			}
		}
	})

	t.Run("issuer 为空报错", func(t *testing.T) {
		if _, err := portalRedirectURL("https://portal.example.com/login", "", "X", "CRM"); err == nil {
			t.Fatal("empty issuer should be rejected")
		}
	})

	t.Run("issuer 结尾斜杠不产生双斜杠", func(t *testing.T) {
		got, err := portalRedirectURL("https://portal.example.com/login", issuer+"/", "X", "CRM")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(got, "//login?id=") {
			t.Fatalf("double slash in %q", got)
		}
	})
}
