package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/api/response"
)

// RateLimit 固定窗口限流：key 维度由 scope + 客户端标识组成。
// Redis 不可用时放行（限流是保护手段，不应成为单点故障）。
func RateLimit(rdb *redis.Client, scope string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}
		key := "rl:" + scope + ":" + clientKey(c)
		bucket := time.Now().Unix() / int64(window.Seconds())
		full := key + ":" + strconv.FormatInt(bucket, 10)

		cnt, err := rdb.Incr(c.Request.Context(), full).Result()
		if err != nil {
			c.Next()
			return
		}
		if cnt == 1 {
			rdb.Expire(c.Request.Context(), full, window)
		}
		if cnt > int64(limit) {
			response.Fail(c, response.ErrRateLimited)
			return
		}
		c.Next()
	}
}

func clientKey(c *gin.Context) string {
	if sid, err := c.Cookie(SessionCookie); err == nil && sid != "" {
		return sid
	}
	return c.ClientIP()
}
