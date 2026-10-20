package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/pkg/trace"
)

// Trace 为每个请求生成 traceId，写入响应头与 context。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := c.GetHeader("X-Trace-Id")
		if tid == "" {
			tid = id.New()
		}
		c.Header("X-Trace-Id", tid)
		c.Request = c.Request.WithContext(trace.WithID(c.Request.Context(), tid))
		c.Next()
	}
}
