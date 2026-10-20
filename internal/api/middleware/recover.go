package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
)

// Recover 捕获 panic，返回统一 500，不泄露堆栈。
func Recover(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered",
					"panic", r,
					"path", c.Request.URL.Path,
					"stack", string(debug.Stack()),
				)
				response.Fail(c, response.ErrInternal)
			}
		}()
		c.Next()
	}
}
