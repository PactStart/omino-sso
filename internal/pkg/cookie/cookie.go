// Package cookie 集中定义会话 cookie 的属性。
//
// 多域名部署（portal / admin / api 分属不同子域）下，会话 cookie 必须显式挂在主域
// （cfg.CookieDomain，形如 .example.com）才能跨子域共享。写入与清除的属性必须逐字一致：
// 若清除时漏了 Domain，浏览器只会新增一个 host-only 空 cookie，而主域上的真 cookie
// 依然有效，表现为"登出/改密后其它子域仍保持登录"。
//
// 因此所有写/清必须走这里，不要在 api（gin）或 oidc（net/http）里手写 http.Cookie。
package cookie

import (
	"net/http"
	"time"

	"github.com/omino/omino-sso/internal/config"
)

// Name 是会话 cookie 名。
const Name = "sid"

// SetSession 写入会话 cookie。
func SetSession(w http.ResponseWriter, cfg *config.Config, sid string, ttl time.Duration) {
	http.SetCookie(w, newSessionCookie(cfg, sid, int(ttl.Seconds())))
}

// ClearSession 清除会话 cookie。
//
// 属性（尤其 Domain / Path / Secure / SameSite）必须与 SetSession 保持一致，否则删不掉。
func ClearSession(w http.ResponseWriter, cfg *config.Config) {
	http.SetCookie(w, newSessionCookie(cfg, "", -1))
}

func newSessionCookie(cfg *config.Config, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     Name,
		Value:    value,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: sameSiteMode(cfg),
		MaxAge:   maxAge,
	}
}

func sameSiteMode(cfg *config.Config) http.SameSite {
	if cfg.CookieSameSite == "Strict" {
		return http.SameSiteStrictMode
	}
	return http.SameSiteLaxMode
}
