package oidc

// 模板渲染的回归用例：三条渲染路径（普通内置页 / 降级态 / 过期提示页）互不串味。
//
// 为什么单测渲染而不是打整条 HTTP 链：handler 依赖 Storage（内存 pending）、
// SessionService（Redis）与 UserService（MySQL），在单测里搭这些假件成本高且脆弱；
// 整链路由 scripts/e2e 的 A18/A19 与四域 run.sh 覆盖。这里只锁「渲染出来的 HTML
// 到底承诺了什么」，这是用户可见行为的最后一道闸。

import (
	"strings"
	"testing"
)

const degradeHint = "登录状态未能在跳转中延续"

func renderLogin(t *testing.T, data loginData) string {
	t.Helper()
	var sb strings.Builder
	if err := renderLoginTemplate(&sb, data); err != nil {
		t.Fatalf("renderLoginTemplate: %v", err)
	}
	return sb.String()
}

func renderExpired(t *testing.T, data loginData) string {
	t.Helper()
	var sb strings.Builder
	if err := renderExpiredTemplate(&sb, data); err != nil {
		t.Fatalf("renderExpiredTemplate: %v", err)
	}
	return sb.String()
}

// C3：未配置门户地址 → 普通内置登录页，不能出现降级说明。
func TestLoginTemplateNormal(t *testing.T) {
	html := renderLogin(t, loginData{ID: "01ABC", ClientName: "合规平台"})
	if !strings.Contains(html, `<form`) || !strings.Contains(html, `name="username"`) {
		t.Fatalf("normal login page must keep the password form:\n%s", html)
	}
	if strings.Contains(html, degradeHint) {
		t.Fatal("normal login page must not show the degrade hint")
	}
}

// C4：带 portal=1 回跳但无会话 → 内置页 + 降级说明（FR-020）。
func TestLoginTemplateDegraded(t *testing.T) {
	html := renderLogin(t, loginData{ID: "01ABC", ClientName: "合规平台", Degraded: true})
	if !strings.Contains(html, degradeHint) {
		t.Fatalf("degraded page must show the hint:\n%s", html)
	}
	if !strings.Contains(html, `<form`) {
		t.Fatal("degraded page must still be able to log in (form missing)")
	}
}

// C1b：请求过期 + 从门户回跳 → 过期提示页，带「重新登录」入口，且不再自动跳门户。
func TestExpiredTemplate(t *testing.T) {
	html := renderExpired(t, loginData{PortalURL: "https://portal.example.com/login"})
	if !strings.Contains(html, "重新发起登录") {
		t.Fatalf("expired page must tell the user to re-initiate from the app:\n%s", html)
	}
	if !strings.Contains(html, `href="https://portal.example.com/login"`) {
		t.Fatalf("expired page must offer a re-login link when portal is configured:\n%s", html)
	}
	for _, banned := range []string{"pending", "TTL", "SameSite", "cookie"} {
		if strings.Contains(html, banned) {
			t.Fatalf("expired page leaks internal term %q", banned)
		}
	}

	// 未配置门户时不该出现空 href 的按钮。
	html = renderExpired(t, loginData{})
	if strings.Contains(html, `class="btn"`) {
		t.Fatalf("no re-login button expected without portal url:\n%s", html)
	}
}

// SC-008：三种输出都不得把 /login 这类技术路径暴露给用户。
func TestLoginTemplatesHideTechnicalPath(t *testing.T) {
	outputs := map[string]string{
		"normal":   renderLogin(t, loginData{ID: "01ABC", ClientName: "合规平台"}),
		"degraded": renderLogin(t, loginData{ID: "01ABC", ClientName: "合规平台", Degraded: true}),
		"expired":  renderExpired(t, loginData{PortalURL: "https://portal.example.com/login"}),
	}
	for name, html := range outputs {
		if strings.Contains(html, "/login?") || strings.Contains(html, "登录后返回 /login") {
			t.Fatalf("%s page exposes the technical path:\n%s", name, html)
		}
	}
}
