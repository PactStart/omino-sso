package me

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// 门户侧的应用管理端点（006-app-admin-role）。
//
// 这里有三类代码：
//  1. AppManageContext —— 管理页摘要（新端点，字段刻意小于 ClientDetail）；
//  2. 应用信息 —— AppClientDetail / AppUpdateClient / AppSetClientLogo / AppClearClientLogo，
//     门户自有实现：详情复用 dto.ClientDetail，写入走 ClientService.UpdateBasicInfo
//     （只允许名称 / 描述 / Logo / 回调地址），Logo 复用 FileService；
//  3. 一组薄 wrapper（18 个）—— 每个方法只有一行，把请求原样转交给 admin.Handler 的同名方法。
//
// 之所以加 wrapper 而不直接在 router 里挂 admin 的方法：swag 的路径来自注解，
// 直接复用会把文档路径写成 /admin/clients/...，与实际路由不符；业务逻辑仍然只有
// admin.Handler + AppAuthService 一份（spec A7「单一实现来源」）。
// 鉴权由 middleware.AppAdmin 完成，handler 内不做权限判断。

// AppManageContext GET /api/v1/me/apps/:id
//
// @Summary      应用管理摘要
// @Description  返回应用名称、图标、描述、状态与成员数，供门户管理页头部展示。只含管理页需要的字段：不含回调地址、scope、TTL、isPublic 或任何密钥。调用者必须是该应用的应用管理员。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=dto.AppManageContext}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id} [get]
func (h *Handler) AppManageContext(c *gin.Context) {
	clientID := c.Param("id")
	cl, count, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		// AppAdmin 已确认应用存在且启用且调用者是管理员，走到这里属于内部故障。
		h.log.Error("app manage context failed", "err", err, "clientId", clientID)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := dto.AppManageContext{ClientID: cl.ClientID, Name: cl.ClientName, Status: cl.Status, MemberCount: count}
	if cl.Description != nil {
		out.Description = *cl.Description
	}
	if cl.LogoURL != nil {
		out.LogoURL = *cl.LogoURL
	}
	response.Success(c, out)
}

// ---------- 应用信息（详情 / 基础信息编辑 / Logo） ----------

// AppClientDetail GET /api/v1/me/apps/:id/detail
//
// @Summary      应用详情（门户）
// @Description  返回应用的可编辑字段与只读配置（含回调地址、登出地址、授权方式、可申请范围、两类 TTL、状态、成员数），供门户「应用信息」页展示；不含任何密钥。与 admin 端详情字段一致，但门户只允许修改名称、描述、Logo 与回调地址。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=dto.ClientDetail}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/detail [get]
func (h *Handler) AppClientDetail(c *gin.Context) {
	clientID := c.Param("id")
	cl, count, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		h.log.Error("app detail failed", "err", err, "clientId", clientID)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromClientDetail(cl, count))
}

// AppUpdateClientRequest 门户可编辑字段白名单：名称 / 描述 / Logo / 首页地址 / 回调地址。
// 其余字段（登出地址、TTL、状态）不在结构体内，即使请求体携带也会被忽略。
type AppUpdateClientRequest struct {
	ClientName   *string   `json:"clientName"`
	Description  *string   `json:"description"`
	LogoURL      *string   `json:"logoUrl"`
	HomeURL      *string   `json:"homeUrl"`
	RedirectURIs *[]string `json:"redirectUris"`
	// UpdatedAt 为读取时的版本（毫秒），非空时做乐观版本校验。
	UpdatedAt *int64 `json:"updatedAt"`
}

// AppUpdateClient PATCH /api/v1/me/apps/:id
//
// @Summary      更新应用基础信息（门户）
// @Description  应用管理员只能修改名称、描述、Logo、首页地址（homeUrl）与回调地址；登出地址、两类令牌有效期与启用状态为只读（即使请求体携带也不会生效）。首页地址为必填字段，传空串返回 VALIDATION_FAILED。携带 updatedAt 时做乐观版本校验，冲突返回 409。写入 client_updated 审计（首页地址记前后值、回调地址记前后数组）。
// @Tags         门户应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                 true  "应用标识 clientId"
// @Param        body  body  AppUpdateClientRequest true  "可编辑字段（缺失=不变）"
// @Success      200   {object}  response.Body{data=dto.ClientDetail}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id} [patch]
func (h *Handler) AppUpdateClient(c *gin.Context) {
	clientID := c.Param("id")
	var req AppUpdateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	before, _, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		h.log.Error("app detail before update failed", "err", err, "clientId", clientID)
		response.Fail(c, response.ErrInternal)
		return
	}
	updated, err := h.clients.UpdateBasicInfo(c.Request.Context(), clientID, service.UpdateClientInput{
		ClientName:   req.ClientName,
		Description:  req.Description,
		LogoURL:      req.LogoURL,
		HomeURL:      req.HomeURL,
		RedirectURIs: req.RedirectURIs,
		UpdatedAt:    req.UpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAppValidation):
			response.Fail(c, response.ErrValidation)
		case errors.Is(err, service.ErrClientNotFound):
			response.Fail(c, response.ErrNotFound)
		case errors.Is(err, service.ErrAppResourceStale):
			response.Fail(c, response.ErrAppResourceStale)
		default:
			h.log.Error("update app basic info", "err", err, "clientId", clientID)
			response.Fail(c, response.ErrInternal)
		}
		return
	}
	fields := map[string]any{"clientId": clientID, "changed": changedBasicFields(before, updated)}
	if derefString(before.HomeURL) != derefString(updated.HomeURL) {
		fields["homeUrl"] = map[string]any{
			"before": derefString(before.HomeURL),
			"after":  derefString(updated.HomeURL),
		}
	}
	if !equalStringList(before.RedirectURIs, updated.RedirectURIs) {
		fields["redirectUris"] = map[string]any{
			"before": []string(before.RedirectURIs),
			"after":  []string(updated.RedirectURIs),
		}
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventClientUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success", Fields: fields,
	})
	_, count, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		count = 0
	}
	response.Success(c, dto.FromClientDetail(updated, count))
}

// AppLogoRequest POST /api/v1/me/apps/:id/logo 请求体。
type AppLogoRequest struct {
	Key    string `json:"key" binding:"required" example:"app_logo/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png"`
	Format string `json:"format" binding:"required" example:"png"`
	Size   int64  `json:"size" binding:"required,min=1" example:"245760"`
}

// AppSetClientLogo POST /api/v1/me/apps/:id/logo
//
// @Summary      设置应用 Logo（门户）
// @Description  直传完成后调用，写入文件记录并回写该应用的 logo_url。与应用基础信息同一份数据，仅应用管理员可操作。
// @Tags         门户应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string          true  "应用标识 clientId"
// @Param        body  body  AppLogoRequest  true  "Logo 确认请求体"
// @Success      200   {object}  response.Body{data=dto.FileItem}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/logo [post]
func (h *Handler) AppSetClientLogo(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	var req AppLogoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f, err := h.files.ConfirmUpload(c.Request.Context(), service.ConfirmUploadInput{
		Scene:    model.FileSceneAppLogo,
		Key:      req.Key,
		Format:   req.Format,
		Size:     req.Size,
		TargetID: c.Param("id"),
		ActorID:  u.ID,
	})
	switch {
	case err == nil:
	case errors.Is(err, service.ErrFileValidation):
		response.Fail(c, response.ErrValidation)
		return
	case errors.Is(err, service.ErrTargetNotFound):
		response.Fail(c, response.ErrNotFound)
		return
	default:
		h.log.Error("set app logo", "err", err, "clientId", c.Param("id"))
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, dto.FromFile(f))
}

// AppClearClientLogo DELETE /api/v1/me/apps/:id/logo
//
// @Summary      清除应用 Logo（门户）
// @Description  删除 Logo 对象并清空该应用的 logo_url；无活跃 Logo 时幂等成功。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/logo [delete]
func (h *Handler) AppClearClientLogo(c *gin.Context) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}
	if err := h.files.ClearByTarget(c.Request.Context(), model.FileSceneAppLogo, c.Param("id"), u.ID); err != nil {
		h.log.Error("clear app logo", "err", err, "clientId", c.Param("id"))
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, nil)
}

// changedBasicFields 列出白名单字段中实际变化的字段名（审计 detail 用）。
func changedBasicFields(before, after *model.Client) []string {
	changed := make([]string, 0, 4)
	if before.ClientName != after.ClientName {
		changed = append(changed, "clientName")
	}
	if derefString(before.Description) != derefString(after.Description) {
		changed = append(changed, "description")
	}
	if derefString(before.LogoURL) != derefString(after.LogoURL) {
		changed = append(changed, "logoUrl")
	}
	if derefString(before.HomeURL) != derefString(after.HomeURL) {
		changed = append(changed, "homeUrl")
	}
	if !equalStringList(before.RedirectURIs, after.RedirectURIs) {
		changed = append(changed, "redirectUris")
	}
	return changed
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func equalStringList(a, b model.StringList) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------- 成员 ----------

// AppListMembers GET /api/v1/me/apps/:id/members
//
// @Summary      应用成员列表（门户）
// @Description  与 admin 端同一实现：keyword 对用户名前缀/姓名包含匹配，deptId 含子部门；all=true 返回当前筛选下的全部 userId（上限 5000）。响应每项含 isAdmin。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id        path   string  true   "应用标识 clientId"
// @Param        keyword   query  string  false  "用户名前缀或姓名关键字"
// @Param        deptId    query  string  false  "部门 ID（含子部门）"
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 100"
// @Param        all       query  bool    false  "true 时只返回 userId 集合"
// @Success      200  {object}  response.Body{data=dto.MemberPage}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/members [get]
func (h *Handler) AppListMembers(c *gin.Context) { h.appMgmt.ListMembers(c) }

// AppListCandidates GET /api/v1/me/apps/:id/members/candidates
//
// @Summary      候选成员（门户）
// @Description  与 admin 端同一实现：返回尚未加入该应用且未停用的用户；pageSize 上限 50。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id        path   string  true   "应用标识 clientId"
// @Param        keyword   query  string  false  "用户名前缀或姓名关键字"
// @Param        deptId    query  string  false  "部门 ID（含子部门）"
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 50"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.CandidateItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/members/candidates [get]
func (h *Handler) AppListCandidates(c *gin.Context) { h.appMgmt.ListCandidates(c) }

// AppAddMembers POST /api/v1/me/apps/:id/members
//
// @Summary      批量添加成员（门户）
// @Description  与 admin 端同一实现：userIds 与 deptId 二选一；userIds 单次 1–500，停用账号跳过且幂等。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        body  body  object  true  "{userIds:[...]}；{deptId,includeSubDepts} 已由组织授权取代（010），仅兼容保留"
// @Success      200  {object}  response.Body  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/members [post]
func (h *Handler) AppAddMembers(c *gin.Context) { h.appMgmt.AddMembers(c) }

// AppRemoveMembers POST /api/v1/me/apps/:id/members/remove
//
// @Summary      批量移除成员（门户）
// @Description  与 admin 端同一实现：同一事务内解除角色绑定并删除成员关系，管理员身份随之失效；userIds 单次 1–500。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        body  body  object  true  "{userIds:[...]}"
// @Success      200  {object}  response.Body  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/members/remove [post]
func (h *Handler) AppRemoveMembers(c *gin.Context) { h.appMgmt.RemoveMembers(c) }

// ---------- 组织授权（010） ----------

// AppListOrgGrants GET /api/v1/me/apps/:id/org-grants
//
// @Summary      组织授权列表（门户）
// @Description  与 admin 端同一实现：返回该应用的组织授权及在职启用账号数、停用覆盖数、撤销影响人数。门户可见性由「手动成员 ∪ 组织授权覆盖」决定，不参与 OIDC 授权判定（010 FR-029）。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=object{list=[]dto.OrgGrantItem,total=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/org-grants [get]
func (h *Handler) AppListOrgGrants(c *gin.Context) { h.appMgmt.ListOrgGrants(c) }

// AppAddOrgGrants POST /api/v1/me/apps/:id/org-grants
//
// @Summary      新增/调整组织授权（门户）
// @Description  与 admin 端同一实现：按部门授予门户可见性（默认含子部门），同部门重复提交幂等，仅 includeSubDepts 变化时更新并留审计。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string               true  "应用标识 clientId"
// @Param        body  body  dto.AddOrgGrantsReq  true  "授权列表，1–500 条"
// @Success      200   {object}  response.Body{data=dto.AddOrgGrantsResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / APP_ORG_GRANT_LIMIT"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/org-grants [post]
func (h *Handler) AppAddOrgGrants(c *gin.Context) { h.appMgmt.AddOrgGrants(c) }

// AppRemoveOrgGrants POST /api/v1/me/apps/:id/org-grants/remove
//
// @Summary      撤销组织授权（门户）
// @Description  与 admin 端同一实现：撤销后不再贡献门户可见性，手动成员与角色绑定不变，既有会话与令牌不回收；impactUsers 为撤销前计算的影响人数。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                   true  "应用标识 clientId"
// @Param        body  body  dto.RemoveOrgGrantsReq   true  "待撤销部门，1–500 条"
// @Success      200   {object}  response.Body{data=dto.RemoveOrgGrantsResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/org-grants/remove [post]
func (h *Handler) AppRemoveOrgGrants(c *gin.Context) { h.appMgmt.RemoveOrgGrants(c) }

// ---------- 角色 ----------

// AppListRoles GET /api/v1/me/apps/:id/roles
//
// @Summary      角色列表（门户）
// @Description  与 admin 端同一实现：返回应用内角色及其权限数、成员数。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.AppRoleItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles [get]
func (h *Handler) AppListRoles(c *gin.Context) { h.appMgmt.ListRoles(c) }

// AppCreateRole POST /api/v1/me/apps/:id/roles
//
// @Summary      新建角色（门户）
// @Description  与 admin 端同一实现：角色名 ^[A-Za-z0-9]{1,32}$，应用内唯一且大小写敏感；描述 ≤100。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        body  body  object  true  "{name, description}"
// @Success      201  {object}  response.Body{data=dto.AppRoleItem}  "created"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      409  {object}  response.Body  "APP_ROLE_NAME_EXISTS"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles [post]
func (h *Handler) AppCreateRole(c *gin.Context) { h.appMgmt.CreateRole(c) }

// AppGetRole GET /api/v1/me/apps/:id/roles/:rid
//
// @Summary      角色详情（门户）
// @Description  与 admin 端同一实现：返回角色及其已绑定的权限 ID 与成员 ID。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Param        rid  path  string  true  "角色 ID"
// @Success      200  {object}  response.Body{data=dto.RoleDetail}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles/{rid} [get]
func (h *Handler) AppGetRole(c *gin.Context) { h.appMgmt.GetRole(c) }

// AppUpdateRole PATCH /api/v1/me/apps/:id/roles/:rid
//
// @Summary      修改角色（门户）
// @Description  与 admin 端同一实现：携带 updatedAt 时做乐观版本校验，冲突返回 409 APP_RESOURCE_STALE。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        rid   path  string  true  "角色 ID"
// @Param        body  body  object  true  "{name?, description?, updatedAt?}"
// @Success      200  {object}  response.Body{data=dto.RoleDetail}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_ROLE_NAME_EXISTS"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles/{rid} [patch]
func (h *Handler) AppUpdateRole(c *gin.Context) { h.appMgmt.UpdateRole(c) }

// AppDeleteRole DELETE /api/v1/me/apps/:id/roles/:rid
//
// @Summary      删除角色（门户）
// @Description  与 admin 端同一实现：删除角色并解除其对权限与成员的绑定。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Param        rid  path  string  true  "角色 ID"
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles/{rid} [delete]
func (h *Handler) AppDeleteRole(c *gin.Context) { h.appMgmt.DeleteRole(c) }

// AppPutRolePermissions PUT /api/v1/me/apps/:id/roles/:rid/permissions
//
// @Summary      设置角色权限（门户，覆盖式）
// @Description  与 admin 端同一实现：提交完整 permissionIds（≤1000）；携带 updatedAt 时做乐观版本校验。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        rid   path  string  true  "角色 ID"
// @Param        body  body  object  true  "{permissionIds:[...], updatedAt?}"
// @Success      200  {object}  response.Body{data=dto.PermissionGrantResult}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles/{rid}/permissions [put]
func (h *Handler) AppPutRolePermissions(c *gin.Context) { h.appMgmt.PutRolePermissions(c) }

// AppPutRoleMembers PUT /api/v1/me/apps/:id/roles/:rid/members
//
// @Summary      设置角色成员（门户，覆盖式）
// @Description  与 admin 端同一实现：提交完整 userIds（≤5000），缺失的成员自动补入应用；携带 updatedAt 时做乐观版本校验。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        rid   path  string  true  "角色 ID"
// @Param        body  body  object  true  "{userIds:[...], updatedAt?}"
// @Success      200  {object}  response.Body{data=dto.MemberGrantResult}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/roles/{rid}/members [put]
func (h *Handler) AppPutRoleMembers(c *gin.Context) { h.appMgmt.PutRoleMembers(c) }

// ---------- 权限 ----------

// AppListPermissions GET /api/v1/me/apps/:id/permissions
//
// @Summary      权限列表（门户）
// @Description  与 admin 端同一实现：返回扁平权限列表（含父级与子节点数），由前端自行组树。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.AppPermissionItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions [get]
func (h *Handler) AppListPermissions(c *gin.Context) { h.appMgmt.ListPermissions(c) }

// AppCreatePermission POST /api/v1/me/apps/:id/permissions
//
// @Summary      新建权限（门户）
// @Description  与 admin 端同一实现：编码 ^[A-Za-z0-9_.:-]{1,64}$，应用内唯一；父级须属于同一应用。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        body  body  object  true  "{code, name, description?, parentId?, sort}"
// @Success      201  {object}  response.Body{data=dto.AppPermissionItem}  "created"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      409  {object}  response.Body  "APP_PERMISSION_CODE_EXISTS"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions [post]
func (h *Handler) AppCreatePermission(c *gin.Context) { h.appMgmt.CreatePermission(c) }

// AppUpdatePermission PATCH /api/v1/me/apps/:id/permissions/:pid
//
// @Summary      修改权限（门户）
// @Description  与 admin 端同一实现：parentId 可显式置 null 移到根级；父级为自身或后代返回 400。
// @Tags         门户应用管理
// @Accept        json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        pid   path  string  true  "权限 ID"
// @Param        body  body  object  true  "{code?, name?, description?, parentId?, sort?, updatedAt?}"
// @Success      200  {object}  response.Body{data=dto.AppPermissionItem}  "success"
// @Failure      400  {object}  response.Body  "APP_PERMISSION_INVALID_PARENT"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_PERMISSION_CODE_EXISTS"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions/{pid} [patch]
func (h *Handler) AppUpdatePermission(c *gin.Context) { h.appMgmt.UpdatePermission(c) }

// AppDeletePermission DELETE /api/v1/me/apps/:id/permissions/:pid
//
// @Summary      删除权限（门户）
// @Description  与 admin 端同一实现：仍有子权限时返回 409 APP_PERMISSION_HAS_CHILDREN；删除同时解除角色绑定。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Param        pid  path  string  true  "权限 ID"
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "APP_PERMISSION_HAS_CHILDREN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions/{pid} [delete]
func (h *Handler) AppDeletePermission(c *gin.Context) { h.appMgmt.DeletePermission(c) }

// ---------- 权限点批量导入 / 导出（011） ----------

// AppValidatePermissionImport POST /api/v1/me/apps/:id/permissions/import/validate
//
// @Summary      校验权限点导入文档（门户）
// @Description  与 admin 端同一实现：只读预览，内容非法时仍返回 200 且 ok=false，不写权限点、不写审计。
// @Tags         门户应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id     path   string  true   "应用标识 clientId"
// @Param        source query  string  false  "来源（paste|file），缺省与非法值按 paste"
// @Param        body   body   dto.PermissionImportDocument  true  "导入文档（≤512KB）"
// @Success      200  {object}  response.Body{data=dto.PermissionImportPreview}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions/import/validate [post]
func (h *Handler) AppValidatePermissionImport(c *gin.Context) { h.appMgmt.ValidatePermissionImport(c) }

// AppConfirmPermissionImport POST /api/v1/me/apps/:id/permissions/import
//
// @Summary      确认导入权限点（门户）
// @Description  与 admin 端同一实现：单事务内按 code 幂等合并，有实际写入时写 1 条聚合审计事件。
// @Tags         门户应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id     path   string  true   "应用标识 clientId"
// @Param        source query  string  false  "来源（paste|file），缺省与非法值按 paste"
// @Param        body   body   dto.PermissionImportDocument  true  "导入文档（≤512KB）"
// @Success      200  {object}  response.Body{data=dto.PermissionImportResult}  "success"
// @Failure      400  {object}  response.Body  "APP_PERMISSION_IMPORT_INVALID"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      409  {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      413  {object}  response.Body  "APP_PERMISSION_IMPORT_TOO_LARGE"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions/import [post]
//
// 注意：这条路由**不挂** middleware.AppAdmin（见 router.go），鉴权在本方法内完成。
// 原因：中间件会在进入 handler 前拒绝「应用不存在 / 已停用」，门户侧就永远写不出
// FR-025 要求的失败审计事件；而对外仍统一返回通用 403，不泄露应用是否存在（FR-021）。
func (h *Handler) AppConfirmPermissionImport(c *gin.Context) {
	clientID := c.Param("id")
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, response.ErrUnauthenticated)
		return
	}

	ctx := c.Request.Context()
	if err := h.appAuth.AuthorizeImportConfirm(ctx, clientID, u.ID); err != nil {
		if reason, audit := importRejectionReason(err); audit {
			h.audit.Emit(ctx, service.Event{
				Type: model.EventAppPermissionsImported, ClientID: clientID,
				IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "failure",
				Fields: map[string]any{
					"clientId": clientID, "reason": reason, "source": portalImportSource(c),
				},
			})
		} else if !errors.Is(err, service.ErrAppNotAdmin) && !errors.Is(err, service.ErrAppMemberNotFound) {
			h.log.Error("app permission import authorize failed", "err", err, "clientId", clientID)
		}
		// 门户统一通用文案：不存在 / 非管理员 / 已停用三者响应完全一致（FR-021）
		response.Fail(c, response.ErrForbidden)
		return
	}
	h.appMgmt.ConfirmPermissionImport(c)
}

// importRejectionReason 把门户导入的鉴权失败分类为审计 reason；只有业务拒绝才写事件。
func importRejectionReason(err error) (string, bool) {
	switch {
	case errors.Is(err, service.ErrClientNotFound):
		return "client_not_found", true
	case errors.Is(err, service.ErrAppClientDisabled):
		return "client_disabled", true
	default:
		return "", false
	}
}

// portalImportSource 读取来源 query（白名单外一律 paste，仅供审计统计）。
func portalImportSource(c *gin.Context) string {
	if c.Query("source") == service.ImportSourceFile {
		return service.ImportSourceFile
	}
	return service.ImportSourcePaste
}

// AppExportPermissions GET /api/v1/me/apps/:id/permissions/export
//
// @Summary      导出权限点（门户）
// @Description  与 admin 端同一实现：产出与导入同构的 JSON，并自证 importable（是否可回导）。
// @Tags         门户应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id  path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=dto.PermissionExportDocument}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "appAdmin"
// @Router       /me/apps/{id}/permissions/export [get]
func (h *Handler) AppExportPermissions(c *gin.Context) { h.appMgmt.ExportPermissions(c) }
