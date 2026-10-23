package cookie

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omino/omino-sso/internal/config"
)

// 回归用例：清除必须与写入使用完全一致的 Domain/Path/Secure/SameSite。
// 只写 Name+Path 会生成 host-only 空 cookie，主域上的真 cookie 依然有效，
// 表现为"登出/改密后其它子域仍保持登录"。
func TestClearSessionMatchesSetSessionAttributes(t *testing.T) {
	cfg := &config.Config{
		CookieDomain:   ".example.com",
		CookieSecure:   true,
		CookieSameSite: "Strict",
	}

	set := parseSessionCookie(t, func(rec *httptest.ResponseRecorder) {
		SetSession(rec, cfg, "sid-value", 2*time.Hour)
	})
	clear := parseSessionCookie(t, func(rec *httptest.ResponseRecorder) {
		ClearSession(rec, cfg)
	})

	if clear.Value != "" {
		t.Fatalf("cleared cookie value = %q, want empty", clear.Value)
	}
	if clear.MaxAge != -1 {
		t.Fatalf("cleared cookie MaxAge = %d, want -1", clear.MaxAge)
	}
	if clear.Domain != set.Domain {
		t.Fatalf("cleared Domain = %q, want %q (host-only clear cannot delete the shared cookie)", clear.Domain, set.Domain)
	}
	if clear.Path != set.Path {
		t.Fatalf("cleared Path = %q, want %q", clear.Path, set.Path)
	}
	if clear.Secure != set.Secure || clear.HttpOnly != set.HttpOnly {
		t.Fatalf("cleared Secure/HttpOnly = %v/%v, want %v/%v", clear.Secure, clear.HttpOnly, set.Secure, set.HttpOnly)
	}
	if clear.SameSite != set.SameSite {
		t.Fatalf("cleared SameSite = %v, want %v", clear.SameSite, set.SameSite)
	}
}

func TestSetSessionFields(t *testing.T) {
	cfg := &config.Config{
		CookieDomain:   ".example.com",
		CookieSecure:   true,
		CookieSameSite: "Lax",
	}

	c := parseSessionCookie(t, func(rec *httptest.ResponseRecorder) {
		SetSession(rec, cfg, "sid-value", 90*time.Second)
	})

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Name", c.Name, Name},
		{"Value", c.Value, "sid-value"},
		{"Path", c.Path, "/"},
		// 解析侧会按 RFC 6265 §4.1.2.3 去掉前导点，故这里比对 "example.com"。
		{"Domain", c.Domain, "example.com"},
		{"HttpOnly", c.HttpOnly, true},
		{"Secure", c.Secure, true},
		{"SameSite", c.SameSite, http.SameSiteLaxMode},
		{"MaxAge", c.MaxAge, 90},
	}
	for _, ck := range checks {
		if ck.got != ck.want {
			t.Fatalf("%s = %v, want %v", ck.name, ck.got, ck.want)
		}
	}
}

func parseSessionCookie(t *testing.T, write func(*httptest.ResponseRecorder)) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	write(rec)
	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d Set-Cookie headers, want 1", len(cookies))
	}
	return cookies[0]
}
