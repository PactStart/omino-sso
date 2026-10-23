package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
)

// 007 FR-023 / plan D10：登出必须清掉**带 Domain** 的会话 cookie。
//
// 只写 Name+Path 的清 cookie 只会生成一个 host-only 空 cookie，主域上的真 cookie 依旧有效，
// 表现为"门户登出了、后台还登着"。属性一致性的单元级回归在
// internal/pkg/cookie/cookie_test.go；本文件锁的是 handler 是否真的走了那条统一实现。
func TestLogoutClearsSharedSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := sharedCookieConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 场景一：没有 sid（幂等调用）——也必须清 cookie，且不得触达会话存储
	rec := runLogout(t, &Handler{cfg: cfg, users: nil, sessions: nil, log: logger}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("登出状态码 = %d，期望 200", rec.Code)
	}
	assertClearedSessionCookie(t, rec)

	// 场景二：带了 sid 但会话存储不可用（Redis 挂掉）——吊销失败也要清 cookie，
	// 否则用户会"点了登出还登着"，比吊销失败本身更糟。
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  0,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	sessions := service.NewSessionService(store.New(nil, rdb), cfg)

	rec = runLogout(t, &Handler{cfg: cfg, sessions: sessions, log: logger}, "any-sid")
	if rec.Code != http.StatusOK {
		t.Fatalf("会话存储不可用时的登出状态码 = %d，期望 200（幂等）", rec.Code)
	}
	assertClearedSessionCookie(t, rec)
}

func sharedCookieConfig() *config.Config {
	return &config.Config{
		// 与部署模板一致：主域形态，写/清必须同一个值
		CookieDomain:   ".example.com",
		CookieSecure:   true,
		CookieSameSite: "Lax",
		SessionTTL:     time.Hour,
	}
}

func runLogout(t *testing.T, h *Handler, sid string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	if sid != "" {
		c.Request.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: sid})
	}
	h.Logout(c)
	return rec
}

func assertClearedSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	raw := rec.Header().Get("Set-Cookie")
	if raw == "" {
		t.Fatal("登出未下发 Set-Cookie：浏览器上的 sid 不会被清除")
	}
	// Max-Age=0 是"立即过期"的线上形态，必须在原始头里出现
	if !strings.Contains(raw, "Max-Age=0") {
		t.Fatalf("Set-Cookie 缺少 Max-Age=0：%q", raw)
	}
	// Go 序列化时会省略 Domain 的前导点（配置 .example.com → 输出 example.com），
	// 判等要按输出形态比，避免"看起来没配 Domain"的误判。
	if !strings.Contains(raw, "Domain=example.com") {
		t.Fatalf("Set-Cookie 缺少共享域 Domain=example.com（host-only 清不掉主域 cookie）：%q", raw)
	}

	cookies := rec.Result().Cookies()
	var sid *http.Cookie
	for _, ck := range cookies {
		if ck.Name == middleware.SessionCookie {
			sid = ck
		}
	}
	if sid == nil {
		t.Fatalf("响应里没有 sid cookie：%v", cookies)
	}
	if sid.Domain != "example.com" {
		t.Fatalf("sid cookie Domain = %q，期望 example.com", sid.Domain)
	}
	if sid.MaxAge != -1 {
		t.Fatalf("sid cookie MaxAge = %d，期望 -1", sid.MaxAge)
	}
	if sid.Value != "" {
		t.Fatalf("清除用的 sid cookie 仍带值 %q", sid.Value)
	}
	if !sid.HttpOnly || !sid.Secure || sid.SameSite != http.SameSiteLaxMode {
		t.Fatalf("sid cookie 属性漂移：HttpOnly=%v Secure=%v SameSite=%v", sid.HttpOnly, sid.Secure, sid.SameSite)
	}
}
