package admin

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// 组织授权（010-app-access-org-grant）。admin 端与门户应用管理员端共用这一套实现，
// 门户侧只在 internal/api/me/app_admin.go 里加薄 wrapper（鉴权由路由分组决定）。
// 本特性是「门户可见性」：不参与 OIDC 授权判定，也不回收既有会话与令牌（spec FR-029）。

// ListOrgGrants GET /api/v1/admin/clients/:id/org-grants
//
// @Summary      组织授权列表
// @Description  返回该应用的全部组织授权（部门名、层级路径、是否含子部门、在职启用账号数、停用覆盖数、撤销影响人数）；不分页，上限 500 条。
// @Tags         应用成员
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=object{list=[]dto.OrgGrantItem,total=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/org-grants [get]
func (h *Handler) ListOrgGrants(c *gin.Context) {
	clientID := c.Param("id")
	items, err := h.appAuth.ListOrgGrants(c.Request.Context(), clientID)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	list := make([]dto.OrgGrantItem, 0, len(items))
	for _, it := range items {
		list = append(list, toOrgGrantItem(it))
	}
	response.Success(c, gin.H{"list": list, "total": len(list)})
}

// AddOrgGrants POST /api/v1/admin/clients/:id/org-grants
//
// @Summary      新增/调整组织授权
// @Description  按部门授予门户可见性（默认含子部门）；同一部门重复提交幂等，仅 includeSubDepts 变化时更新并留审计。整批同一事务，任一条失败全部回滚。
// @Tags         应用成员
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string               true  "应用标识 clientId"
// @Param        body  body  dto.AddOrgGrantsReq  true  "授权列表，1–500 条"
// @Success      200   {object}  response.Body{data=dto.AddOrgGrantsResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / APP_ORG_GRANT_LIMIT"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/org-grants [post]
func (h *Handler) AddOrgGrants(c *gin.Context) {
	var req dto.AddOrgGrantsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")

	inputs := make([]service.OrgGrantInput, 0, len(req.Grants))
	for _, g := range req.Grants {
		deptID := strings.TrimSpace(g.DeptID)
		if deptID == "" {
			response.Fail(c, response.ErrValidation.WithMessage("deptId 不能为空"))
			return
		}
		includeSub := true
		if g.IncludeSubDepts != nil {
			includeSub = *g.IncludeSubDepts
		}
		inputs = append(inputs, service.OrgGrantInput{OrgID: deptID, IncludeSub: includeSub})
	}

	added, updated, unchanged, coveredUsers, changes, err := h.appAuth.AddOrgGrants(
		c.Request.Context(), clientID, currentUserID(c), inputs)
	if err != nil {
		h.emitOrgGrantFailure(c, clientID, err)
		h.failAppAuth(c, err)
		return
	}

	// 审计与回执共用同一份写入后快照（覆盖人数按部门回填）。
	items, listErr := h.appAuth.ListOrgGrants(c.Request.Context(), clientID)
	if listErr != nil {
		h.log.Warn("list org grants after write failed", "err", listErr, "client_id", clientID)
	}
	byOrg := make(map[string]service.OrgGrantItem, len(items))
	for _, it := range items {
		byOrg[it.OrgID] = it
	}
	for _, ch := range changes {
		if ch.Kind == "unchanged" {
			continue // 幂等提交不产生事件（spec FR-003）
		}
		it, ok := byOrg[ch.OrgID]
		fields := map[string]any{"clientId": clientID, "orgId": ch.OrgID, "includeSubDepts": ch.To}
		eventType := model.EventAppOrgGranted
		if ok {
			fields["deptName"] = it.DeptName
			fields["deptPath"] = it.DeptPath
			fields["coveredUsers"] = it.CoveredUsers
		}
		if ch.Kind == "updated" {
			eventType = model.EventAppOrgGrantUpdated
			fields["from"] = ch.From
			fields["to"] = ch.To
		}
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: eventType, ClientID: clientID,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
			Fields: fields,
		})
	}

	response.Success(c, dto.AddOrgGrantsResult{
		Added: added, Updated: updated, Unchanged: unchanged, CoveredUsers: coveredUsers,
	})
}

// RemoveOrgGrants POST /api/v1/admin/clients/:id/org-grants/remove
//
// @Summary      撤销组织授权
// @Description  撤销后该授权不再贡献门户可见性；手动成员关系与角色绑定不受影响，既有会话与令牌也不回收。impactUsers 为「撤销将导致多少人不再可见」（删除前计算）。
// @Tags         应用成员
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                   true  "应用标识 clientId"
// @Param        body  body  dto.RemoveOrgGrantsReq   true  "待撤销部门，1–500 条"
// @Success      200   {object}  response.Body{data=dto.RemoveOrgGrantsResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/org-grants/remove [post]
func (h *Handler) RemoveOrgGrants(c *gin.Context) {
	var req dto.RemoveOrgGrantsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	if len(req.OrgIDs) == 0 {
		response.Fail(c, response.ErrValidation.WithMessage("orgIds 不能为空"))
		return
	}

	// 删除后就查不到部门名，先取一次快照用于审计（deptPath 即授权时刻的路径）。
	before, listErr := h.appAuth.ListOrgGrants(c.Request.Context(), clientID)
	if listErr != nil {
		h.log.Warn("list org grants before remove failed", "err", listErr, "client_id", clientID)
	}
	byOrg := make(map[string]service.OrgGrantItem, len(before))
	for _, it := range before {
		byOrg[it.OrgID] = it
	}

	removed, impactUsers, remainingVisibleUsers, err := h.appAuth.RemoveOrgGrants(c.Request.Context(), clientID, req.OrgIDs)
	if err != nil {
		h.emitOrgGrantFailure(c, clientID, err)
		h.failAppAuth(c, err)
		return
	}
	if removed > 0 {
		for _, orgID := range req.OrgIDs {
			it, ok := byOrg[orgID]
			if !ok {
				continue
			}
			h.audit.Emit(c.Request.Context(), service.Event{
				Type: model.EventAppOrgGrantRevoked, ClientID: clientID,
				IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
				Fields: map[string]any{
					"clientId": clientID, "orgId": orgID, "deptName": it.DeptName, "deptPath": it.DeptPath,
					"includeSubDepts": it.IncludeSub, "impactUsers": impactUsers,
				},
			})
		}
	}

	response.Success(c, dto.RemoveOrgGrantsResult{
		Removed: removed, ImpactUsers: impactUsers, RemainingVisibleUsers: remainingVisibleUsers,
	})
}

// emitOrgGrantFailure 只对业务拒绝写 failure 事件（输入格式错误与 401/403 不记，见 AGENTS §6.7）。
func (h *Handler) emitOrgGrantFailure(c *gin.Context, clientID string, err error) {
	reason := ""
	switch {
	case errors.Is(err, service.ErrAppDeptNotFound):
		reason = "dept_not_found"
	case errors.Is(err, service.ErrOrgGrantLimit):
		reason = "limit_exceeded"
	case errors.Is(err, service.ErrAppValidation):
		reason = "validation"
	default:
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppOrgGranted, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "failure",
		Fields: map[string]any{"clientId": clientID, "reason": reason},
	})
}

func toOrgGrantItem(it service.OrgGrantItem) dto.OrgGrantItem {
	grantedBy := ""
	if it.GrantedBy != nil {
		grantedBy = *it.GrantedBy
	}
	return dto.OrgGrantItem{
		OrgID: it.OrgID, DeptName: it.DeptName, DeptPath: it.DeptPath, IncludeSubDepts: it.IncludeSub,
		CoveredUsers: it.CoveredUsers, CoveredDisabledUsers: it.CoveredDisabledUsers, UniqueUsers: it.UniqueUsers,
		GrantedBy: grantedBy, GrantedAt: dto.Time(it.GrantedAt), UpdatedAt: dto.Time(it.UpdatedAt),
	}
}
