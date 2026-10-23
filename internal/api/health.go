package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/pkg/probe"
	"github.com/omino/omino-sso/internal/store"
)

// healthzHandler 存活探针：不探测依赖，进程能响应即 200。
//
// 不套 response.Body、不进 swagger、不参与限流（见 middleware.RateLimit 的短路）。
func healthzHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !probe.IsGet(c.Request) {
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		probe.WriteHealth(c.Writer)
	}
}

// readyzHandler 就绪探针：探测 MySQL 与 Redis，任一不通即 503。
func readyzHandler(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !probe.IsGet(c.Request) {
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		mysqlErr, redisErr := st.PingDetail(c.Request.Context())
		probe.WriteReady(c.Writer, mysqlErr, redisErr)
	}
}
