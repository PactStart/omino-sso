package me

import (
	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
)

// AppOrgTree GET /api/v1/me/orgs/tree
//
// @Summary      组织架构树（门户只读）
// @Description  门户应用管理页的部门筛选与「按部门加入」所需的只读组织树，报文与 admin 端完全一致（userCount 为直属人数）。任何已登录用户可读；门户不提供任何组织写操作。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "session"
// @Router       /me/orgs/tree [get]
func (h *Handler) AppOrgTree(c *gin.Context) {
	if _, ok := middleware.CurrentUser(c); !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	// 复用 admin 端同一读路径；这层包装只为让文档路径与实际路由一致。
	h.appMgmt.OrgTree(c)
}
