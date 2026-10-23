package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/cookie"
)

// SetSessionCookie 写入会话 cookie。
//
// 属性定义在 internal/pkg/cookie，api（gin）与 oidc（net/http）共用同一实现，
// 避免各处手写 http.Cookie 导致 Domain/SameSite 漂移。
func SetSessionCookie(c *gin.Context, cfg *config.Config, sid string, ttl time.Duration) {
	cookie.SetSession(c.Writer, cfg, sid, ttl)
}

// ClearSessionCookie 清除会话 cookie。
//
// 必须复用写入时的 Domain/SameSite：只写 Name+Path 会生成一个 host-only 空 cookie，
// 而 .example.com 上的真 cookie 依然有效 —— 表现为"登出/改密后四个域都还登着"。
func ClearSessionCookie(c *gin.Context, cfg *config.Config) {
	cookie.ClearSession(c.Writer, cfg)
}
