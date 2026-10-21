package admin

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/middleware"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// 应用内成员 / 角色 / 权限 / 授权 的接口（specs/003-app-management-pages）。
// 只依赖 service，不持有 store（宪法原则 III）。

// failAppAuth 把 service 错误映射为统一响应。
func (h *Handler) failAppAuth(c *gin.Context, err error) {
	var hasChildren *service.ErrAppPermissionHasChildren
	switch {
	case errors.As(err, &hasChildren):
		response.Fail(c, response.ErrAppPermissionHasChildren.WithMessage("该权限下仍有子权限，请先删除或移动子权限"))
	case errors.Is(err, service.ErrClientNotFound), errors.Is(err, service.ErrAppRoleNotFound),
		errors.Is(err, service.ErrAppPermissionNotFound):
		response.Fail(c, response.ErrNotFound)
	case errors.Is(err, service.ErrTooManyIDs):
		response.Fail(c, response.ErrValidation.WithMessage("结果过多，请先按部门或关键字缩小范围"))
	case errors.Is(err, service.ErrAppRoleNameExists):
		response.Fail(c, response.ErrAppRoleNameExists)
	case errors.Is(err, service.ErrAppPermissionCodeExists):
		response.Fail(c, response.ErrAppPermissionCodeExists)
	case errors.Is(err, service.ErrAppPermissionInvalidParent):
		response.Fail(c, response.ErrAppPermissionInvalidParent)
	case errors.Is(err, service.ErrAppResourceStale):
		response.Fail(c, response.ErrAppResourceStale)
	case errors.Is(err, service.ErrAppValidation), errors.Is(err, service.ErrAppDeptNotFound):
		response.Fail(c, response.ErrValidation)
	default:
		h.log.Error("app auth operation failed", "err", err, "path", c.FullPath())
		response.Fail(c, response.ErrInternal)
	}
}

// memberFilter 解析成员/候选的公共查询参数。
func memberFilter(c *gin.Context) (service.MemberFilter, bool) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return service.MemberFilter{}, false
	}
	return service.MemberFilter{
		Keyword:  c.Query("keyword"),
		DeptID:   c.Query("deptId"),
		Page:     page,
		PageSize: pageSize,
	}, true
}

// ---------- 应用详情 ----------

// GetClient GET /api/v1/admin/clients/:id
//
// @Summary      应用详情
// @Description  返回应用的可编辑字段、状态与成员数，供子页面刷新直达与回填；不含任何密钥。
// @Tags         应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=dto.ClientDetail}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id} [get]
func (h *Handler) GetClient(c *gin.Context) {
	cl, count, err := h.appAuth.ClientDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	response.Success(c, dto.FromClientDetail(cl, count))
}

// UpdateClientRequest 应用详情可编辑字段（指针=三态，缺失表示不变）。
type UpdateClientRequest struct {
	ClientName      *string   `json:"clientName"`
	Description     *string   `json:"description"`
	LogoURL         *string   `json:"logoUrl"`
	RedirectURIs    *[]string `json:"redirectUris"`
	PostLogoutURIs  *[]string `json:"postLogoutUris"`
	AccessTokenTTL  *int      `json:"accessTokenTtl"`
	RefreshTokenTTL *int      `json:"refreshTokenTtl"`
	Status          *int8     `json:"status"`
	UpdatedAt       *int64    `json:"updatedAt"`
}

// UpdateClient PATCH /api/v1/admin/clients/:id
//
// @Summary      更新应用详情
// @Description  就地修改名称、描述、图标、地址、两类 TTL 与启用状态；携带 updatedAt 时做乐观版本校验。写入 client_updated 审计（地址类字段记前后数组）。
// @Tags         应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string               true  "应用标识 clientId"
// @Param        body  body  UpdateClientRequest  true  "可编辑字段（缺失=不变）"
// @Success      200   {object}  response.Body{data=dto.ClientDetail}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id} [patch]
func (h *Handler) UpdateClient(c *gin.Context) {
	var req UpdateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	before, _, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	updated, err := h.client.Update(c.Request.Context(), clientID, service.UpdateClientInput{
		ClientName:      req.ClientName,
		Description:     req.Description,
		LogoURL:         req.LogoURL,
		RedirectURIs:    req.RedirectURIs,
		PostLogoutURIs:  req.PostLogoutURIs,
		AccessTokenTTL:  req.AccessTokenTTL,
		RefreshTokenTTL: req.RefreshTokenTTL,
		Status:          req.Status,
		UpdatedAt:       req.UpdatedAt,
	})
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	fields := map[string]any{"clientId": clientID, "changed": changedFields(before, updated)}
	if !equalList(before.RedirectURIs, updated.RedirectURIs) {
		fields["redirectUris"] = map[string]any{"before": strs(before.RedirectURIs), "after": strs(updated.RedirectURIs)}
	}
	if !equalList(before.PostLogoutURIs, updated.PostLogoutURIs) {
		fields["postLogoutUris"] = map[string]any{"before": strs(before.PostLogoutURIs), "after": strs(updated.PostLogoutURIs)}
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventClientUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success", Fields: fields,
	})
	_, memberCount, err := h.appAuth.ClientDetail(c.Request.Context(), clientID)
	if err != nil {
		memberCount = 0
	}
	response.Success(c, dto.FromClientDetail(updated, memberCount))
}

// ---------- 成员 ----------

// ListMembers GET /api/v1/admin/clients/:id/members
//
// @Summary      应用成员列表
// @Description  分页返回成员；all=true 时返回当前筛选下的全部 userId（上限 5000，超出返回 VALIDATION_FAILED）。
// @Tags         应用成员
// @Produce      json
// @Security     cookieAuth
// @Param        id        path   string  true   "应用标识 clientId"
// @Param        keyword   query  string  false  "用户名前缀或姓名关键字"
// @Param        deptId    query  string  false  "部门 ID（含子部门）"
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 200"
// @Param        all       query  string  false  "为 true 时返回全部 userId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.AppMemberItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/members [get]
func (h *Handler) ListMembers(c *gin.Context) {
	f, ok := memberFilter(c)
	if !ok {
		return
	}
	clientID := c.Param("id")
	if c.Query("all") == "true" {
		ids, err := h.appAuth.AllMemberIDs(c.Request.Context(), clientID, f)
		if err != nil {
			h.failAppAuth(c, err)
			return
		}
		response.Success(c, gin.H{"userIds": ids})
		return
	}
	items, total, err := h.appAuth.ListMembers(c.Request.Context(), clientID, f)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	list := make([]dto.AppMemberItem, 0, len(items))
	for _, it := range items {
		list = append(list, dto.AppMemberItem{
			UserID: it.UserID, Username: it.Username, DisplayName: it.DisplayName,
			DeptID: it.DeptID, DeptName: it.DeptName, Status: it.Status,
			JoinedAt: dto.Time(it.JoinedAt), RoleCount: it.RoleCount,
		})
	}
	response.Success(c, response.Page{List: list, Total: total, Page: f.Page, PageSize: f.PageSize})
}

// ListCandidates GET /api/v1/admin/clients/:id/members/candidates
//
// @Summary      候选成员
// @Description  返回尚未加入该应用且未停用的用户；pageSize 上限 50；all=true 返回全部 userId（上限 5000）。
// @Tags         应用成员
// @Produce      json
// @Security     cookieAuth
// @Param        id        path   string  true   "应用标识 clientId"
// @Param        keyword   query  string  false  "用户名前缀或姓名关键字"
// @Param        deptId    query  string  false  "部门 ID（含子部门）"
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 50"
// @Param        all       query  string  false  "为 true 时返回全部 userId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.CandidateItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400  {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/members/candidates [get]
func (h *Handler) ListCandidates(c *gin.Context) {
	f, ok := memberFilter(c)
	if !ok {
		return
	}
	clientID := c.Param("id")
	if c.Query("all") == "true" {
		ids, err := h.appAuth.AllCandidateIDs(c.Request.Context(), clientID, f)
		if err != nil {
			h.failAppAuth(c, err)
			return
		}
		response.Success(c, gin.H{"userIds": ids})
		return
	}
	items, total, err := h.appAuth.ListCandidates(c.Request.Context(), clientID, f)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	list := make([]dto.CandidateItem, 0, len(items))
	for _, it := range items {
		list = append(list, dto.CandidateItem{
			UserID: it.UserID, Username: it.Username, DisplayName: it.DisplayName,
			DeptID: it.DeptID, DeptName: it.DeptName,
		})
	}
	response.Success(c, response.Page{List: list, Total: total, Page: f.Page, PageSize: f.PageSize})
}

// AddMembersRequest 批量添加成员：userIds 与 deptId 二选一。
type AddMembersRequest struct {
	UserIDs         []string `json:"userIds"`
	DeptID          string   `json:"deptId"`
	IncludeSubDepts *bool    `json:"includeSubDepts"`
}

// AddMembers POST /api/v1/admin/clients/:id/members
//
// @Summary      批量添加成员
// @Description  幂等添加；userIds 单次 1–500（超出需分片），deptId 形态按部门整批加入并返回新增/已存在/已停用计数。停用账号跳过。写入 app_member_added 审计。
// @Tags         应用成员
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string             true  "应用标识 clientId"
// @Param        body  body  AddMembersRequest  true  "userIds 或 deptId"
// @Success      200   {object}  response.Body  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/members [post]
func (h *Handler) AddMembers(c *gin.Context) {
	var req AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	if req.DeptID != "" && len(req.UserIDs) > 0 {
		response.Fail(c, response.ErrValidation.WithMessage("userIds 与 deptId 只能二选一"))
		return
	}
	if req.DeptID != "" {
		includeSub := true
		if req.IncludeSubDepts != nil {
			includeSub = *req.IncludeSubDepts
		}
		added, existing, disabled, err := h.appAuth.AddMembersByDept(c.Request.Context(), clientID, req.DeptID, includeSub)
		if err != nil {
			h.failAppAuth(c, err)
			return
		}
		h.audit.Emit(c.Request.Context(), service.Event{
			Type: model.EventAppMemberAdded, ClientID: clientID,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
			Fields: map[string]any{"clientId": clientID, "deptId": req.DeptID, "added": added, "existing": existing, "disabled": disabled},
		})
		response.Success(c, gin.H{"added": added, "existing": existing, "disabled": disabled})
		return
	}
	added, skipped, err := h.appAuth.AddMembers(c.Request.Context(), clientID, req.UserIDs, currentUserID(c))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppMemberAdded, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "added": added, "skipped": skipped},
	})
	response.Success(c, gin.H{"added": added, "skipped": skipped})
}

// RemoveMembersRequest 批量移除成员。
type RemoveMembersRequest struct {
	UserIDs []string `json:"userIds"`
}

// RemoveMembers POST /api/v1/admin/clients/:id/members/remove
//
// @Summary      批量移除成员
// @Description  同一事务内解除角色绑定并删除成员关系；userIds 单次 1–500。写入 app_member_removed 审计。
// @Tags         应用成员
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                true  "应用标识 clientId"
// @Param        body  body  RemoveMembersRequest  true  "待移除 userId 列表"
// @Success      200   {object}  response.Body  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/members/remove [post]
func (h *Handler) RemoveMembers(c *gin.Context) {
	var req RemoveMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	removed, bindings, notMember, err := h.appAuth.RemoveMembers(c.Request.Context(), clientID, req.UserIDs)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppMemberRemoved, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "removed": removed, "roleBindingsRemoved": bindings},
	})
	response.Success(c, gin.H{"removed": removed, "roleBindingsRemoved": bindings, "notMember": notMember})
}

// ---------- 角色 ----------

// ListRoles GET /api/v1/admin/clients/:id/roles
//
// @Summary      角色列表
// @Description  返回应用内角色及其权限数、成员数。
// @Tags         应用角色
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.AppRoleItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles [get]
func (h *Handler) ListRoles(c *gin.Context) {
	items, err := h.appAuth.ListRoles(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	list := make([]dto.AppRoleItem, 0, len(items))
	for _, it := range items {
		list = append(list, dto.AppRoleItem{
			ID: it.ID, ClientID: it.ClientID, Name: it.Name, Description: deref(it.Description),
			PermissionCount: it.PermissionCount, MemberCount: it.MemberCount,
			CreatedAt: dto.Time(it.CreatedAt), UpdatedAt: it.UpdatedAt,
		})
	}
	response.Success(c, response.Page{List: list, Total: int64(len(list)), Page: 1, PageSize: len(list)})
}

// RoleCreateRequest 新建角色。
type RoleCreateRequest struct {
	Name        string  `json:"name" binding:"required" example:"opsAdmin"`
	Description *string `json:"description"`
}

// CreateRole POST /api/v1/admin/clients/:id/roles
//
// @Summary      新建角色
// @Description  角色名 `^[A-Za-z0-9]{1,32}$`，应用内唯一且大小写敏感；描述 ≤100。写入 app_role_created 审计。
// @Tags         应用角色
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string             true  "应用标识 clientId"
// @Param        body  body  RoleCreateRequest  true  "角色名与描述"
// @Success      201   {object}  response.Body{data=dto.AppRoleItem}  "created"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_ROLE_NAME_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles [post]
func (h *Handler) CreateRole(c *gin.Context) {
	var req RoleCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	r, err := h.appAuth.CreateRole(c.Request.Context(), clientID, req.Name, req.Description)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppRoleCreated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "roleId": r.ID, "name": r.Name},
	})
	response.Created(c, dto.AppRoleItem{
		ID: r.ID, ClientID: r.ClientID, Name: r.Name, Description: deref(r.Description),
		CreatedAt: dto.Time(r.CreatedAt), UpdatedAt: r.UpdatedAt,
	})
}

// GetRole GET /api/v1/admin/clients/:id/roles/:rid
//
// @Summary      角色详情
// @Description  返回角色及其 permissionIds / memberIds。
// @Tags         应用角色
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Param        rid  path  string  true  "角色 ID"
// @Success      200  {object}  response.Body{data=dto.RoleDetail}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles/{rid} [get]
func (h *Handler) GetRole(c *gin.Context) {
	r, permIDs, memberIDs, err := h.appAuth.GetRole(c.Request.Context(), c.Param("id"), c.Param("rid"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	response.Success(c, dto.RoleDetail{
		ID: r.ID, ClientID: r.ClientID, Name: r.Name, Description: deref(r.Description),
		PermissionIDs: permIDs, MemberIDs: memberIDs,
		CreatedAt: dto.Time(r.CreatedAt), UpdatedAt: r.UpdatedAt,
	})
}

// RoleUpdateRequest 修改角色（缺失=不变）。
type RoleUpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	UpdatedAt   *int64  `json:"updatedAt"`
}

// UpdateRole PATCH /api/v1/admin/clients/:id/roles/:rid
//
// @Summary      修改角色
// @Description  改名或改描述；携带 updatedAt 时做乐观版本校验（409 APP_RESOURCE_STALE）。写入 app_role_updated 审计。
// @Tags         应用角色
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string             true  "应用标识 clientId"
// @Param        rid   path  string             true  "角色 ID"
// @Param        body  body  RoleUpdateRequest  true  "待改字段"
// @Success      200   {object}  response.Body{data=dto.RoleDetail}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_ROLE_NAME_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles/{rid} [patch]
func (h *Handler) UpdateRole(c *gin.Context) {
	var req RoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	r, err := h.appAuth.UpdateRole(c.Request.Context(), clientID, c.Param("rid"), req.Name, req.Description, req.UpdatedAt)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	changed := []string{}
	if req.Name != nil {
		changed = append(changed, "name")
	}
	if req.Description != nil {
		changed = append(changed, "description")
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppRoleUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "roleId": r.ID, "name": r.Name, "changed": changed},
	})
	_, permIDs, memberIDs, _ := h.appAuth.GetRole(c.Request.Context(), clientID, r.ID)
	response.Success(c, dto.RoleDetail{
		ID: r.ID, ClientID: r.ClientID, Name: r.Name, Description: deref(r.Description),
		PermissionIDs: permIDs, MemberIDs: memberIDs,
		CreatedAt: dto.Time(r.CreatedAt), UpdatedAt: r.UpdatedAt,
	})
}

// DeleteRole DELETE /api/v1/admin/clients/:id/roles/:rid
//
// @Summary      删除角色
// @Description  连带解除角色↔权限与角色↔成员绑定（成员本身保留）。审计记录被解除绑定的快照。
// @Tags         应用角色
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Param        rid  path  string  true  "角色 ID"
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles/{rid} [delete]
func (h *Handler) DeleteRole(c *gin.Context) {
	clientID := c.Param("id")
	r, permIDs, memberIDs, err := h.appAuth.DeleteRole(c.Request.Context(), clientID, c.Param("rid"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppRoleDeleted, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{
			"clientId": clientID, "roleId": r.ID, "name": r.Name,
			"removedPermissionIds": snapshotIDs(permIDs), "removedUserIds": snapshotIDs(memberIDs),
		},
	})
	response.Success(c, nil)
}

// ---------- 权限 ----------

// ListPermissions GET /api/v1/admin/clients/:id/permissions
//
// @Summary      权限列表
// @Description  返回扁平权限列表（含 childCount/roleCount，上限 1000），由前端组树。
// @Tags         应用权限
// @Produce      json
// @Security     cookieAuth
// @Param        id   path  string  true  "应用标识 clientId"
// @Success      200  {object}  response.Body{data=response.Page{list=[]dto.AppPermissionItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions [get]
func (h *Handler) ListPermissions(c *gin.Context) {
	items, err := h.appAuth.ListPermissions(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	list := make([]dto.AppPermissionItem, 0, len(items))
	for _, it := range items {
		list = append(list, toPermissionItem(it))
	}
	response.Success(c, response.Page{List: list, Total: int64(len(list)), Page: 1, PageSize: len(list)})
}

// PermissionCreateRequest 新建权限。
type PermissionCreateRequest struct {
	Code        string  `json:"code" binding:"required" example:"order:read"`
	Name        string  `json:"name" binding:"required" example:"订单读取"`
	Description *string `json:"description"`
	ParentID    *string `json:"parentId"`
	Sort        int     `json:"sort"`
}

// CreatePermission POST /api/v1/admin/clients/:id/permissions
//
// @Summary      新建权限
// @Description  编码 `^[A-Za-z0-9_.:-]{1,64}$`，应用内唯一且大小写敏感；父级须属于同一应用。写入 app_permission_created 审计。
// @Tags         应用权限
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                   true  "应用标识 clientId"
// @Param        body  body  PermissionCreateRequest  true  "权限定义"
// @Success      201   {object}  response.Body{data=dto.AppPermissionItem}  "created"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_PERMISSION_CODE_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions [post]
func (h *Handler) CreatePermission(c *gin.Context) {
	var req PermissionCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	p, err := h.appAuth.CreatePermission(c.Request.Context(), clientID, service.PermissionInput{
		Code: req.Code, Name: req.Name, Description: req.Description, ParentID: req.ParentID, Sort: req.Sort,
	})
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppPermissionCreated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "permissionId": p.ID, "code": p.Code},
	})
	response.Created(c, toPermissionItem(service.PermissionItem{ClientPermission: *p}))
}

// PermissionUpdateRequest 修改权限（缺失=不变；parentId 显式 null 表示移到根级）。
type PermissionUpdateRequest struct {
	Code        *string `json:"code"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	ParentID    *string `json:"parentId"`
	Sort        *int    `json:"sort"`
	UpdatedAt   *int64  `json:"updatedAt"`
}

// UpdatePermission PATCH /api/v1/admin/clients/:id/permissions/:pid
//
// @Summary      修改权限
// @Description  改名/编码/描述/排序/父级；父级为自身或后代返回 400。携带 updatedAt 时做乐观版本校验。写入 app_permission_updated 审计。
// @Tags         应用权限
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                   true  "应用标识 clientId"
// @Param        pid   path  string                   true  "权限 ID"
// @Param        body  body  PermissionUpdateRequest  true  "待改字段"
// @Success      200   {object}  response.Body{data=dto.AppPermissionItem}  "success"
// @Failure      400   {object}  response.Body  "APP_PERMISSION_INVALID_PARENT"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_PERMISSION_CODE_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions/{pid} [patch]
func (h *Handler) UpdatePermission(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	var req PermissionUpdateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	_, setParent := keys["parentId"]

	clientID := c.Param("id")
	patch := service.PermissionPatch{
		Code: req.Code, Name: req.Name, Description: req.Description,
		ParentID: req.ParentID, SetParent: setParent, Sort: req.Sort, UpdatedAt: req.UpdatedAt,
	}
	p, err := h.appAuth.UpdatePermission(c.Request.Context(), clientID, c.Param("pid"), patch)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	changed := []string{}
	if req.Code != nil {
		changed = append(changed, "code")
	}
	if req.Name != nil {
		changed = append(changed, "name")
	}
	if req.Description != nil {
		changed = append(changed, "description")
	}
	if req.Sort != nil {
		changed = append(changed, "sort")
	}
	if setParent {
		changed = append(changed, "parentId")
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppPermissionUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "permissionId": p.ID, "code": p.Code, "changed": changed},
	})
	response.Success(c, toPermissionItem(service.PermissionItem{ClientPermission: *p}))
}

// DeletePermission DELETE /api/v1/admin/clients/:id/permissions/:pid
//
// @Summary      删除权限
// @Description  有子权限返回 409；被角色引用时在同一事务内解除引用后删除。审计记录被解除的角色快照。
// @Tags         应用权限
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
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/permissions/{pid} [delete]
func (h *Handler) DeletePermission(c *gin.Context) {
	clientID := c.Param("id")
	p, roleIDs, err := h.appAuth.DeletePermission(c.Request.Context(), clientID, c.Param("pid"))
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppPermissionDeleted, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{
			"clientId": clientID, "permissionId": p.ID, "code": p.Code,
			"removedRoleIds": snapshotIDs(roleIDs),
		},
	})
	response.Success(c, nil)
}

// ---------- 授权 ----------

// PutPermissionsRequest 覆盖式设置角色权限。
type PutPermissionsRequest struct {
	PermissionIDs []string `json:"permissionIds"`
	UpdatedAt     *int64   `json:"updatedAt"`
}

// PutRolePermissions PUT /api/v1/admin/clients/:id/roles/:rid/permissions
//
// @Summary      设置角色权限（覆盖式）
// @Description  提交完整 permissionIds（≤1000，不分片）；返回新增/移除数量。携带 updatedAt 时做乐观版本校验。写入 app_grant_updated 审计。
// @Tags         应用授权
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                 true  "应用标识 clientId"
// @Param        rid   path  string                 true  "角色 ID"
// @Param        body  body  PutPermissionsRequest  true  "完整权限 ID 集合"
// @Success      200   {object}  response.Body{data=dto.PermissionGrantResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles/{rid}/permissions [put]
func (h *Handler) PutRolePermissions(c *gin.Context) {
	var req PutPermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	roleID := c.Param("rid")
	final, added, removed, err := h.appAuth.PutRolePermissions(c.Request.Context(), clientID, roleID, req.PermissionIDs, req.UpdatedAt)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppGrantUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{"clientId": clientID, "roleId": roleID, "permissionsAdded": added, "permissionsRemoved": removed},
	})
	response.Success(c, dto.PermissionGrantResult{PermissionIDs: final, Added: added, Removed: removed})
}

// PutRoleMembersRequest 覆盖式设置角色成员。
type PutRoleMembersRequest struct {
	UserIDs   []string `json:"userIds"`
	UpdatedAt *int64   `json:"updatedAt"`
}

// PutRoleMembers PUT /api/v1/admin/clients/:id/roles/:rid/members
//
// @Summary      设置角色成员（覆盖式）
// @Description  提交完整 userIds（≤5000，不分片）；缺失的成员自动补入应用。返回新增/移除/自动补入数量。携带 updatedAt 时做乐观版本校验。
// @Tags         应用授权
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string                true  "应用标识 clientId"
// @Param        rid   path  string                true  "角色 ID"
// @Param        body  body  PutRoleMembersRequest  true  "完整成员 ID 集合"
// @Success      200   {object}  response.Body{data=dto.MemberGrantResult}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "APP_RESOURCE_STALE"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/roles/{rid}/members [put]
func (h *Handler) PutRoleMembers(c *gin.Context) {
	var req PutRoleMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	clientID := c.Param("id")
	roleID := c.Param("rid")
	final, added, removed, autoJoined, err := h.appAuth.PutRoleMembers(c.Request.Context(), clientID, roleID, req.UserIDs, req.UpdatedAt)
	if err != nil {
		h.failAppAuth(c, err)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventAppGrantUpdated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
		Fields: map[string]any{
			"clientId": clientID, "roleId": roleID,
			"membersAdded": added, "membersRemoved": removed, "autoJoined": autoJoined,
		},
	})
	response.Success(c, dto.MemberGrantResult{
		MemberIDs: final, Added: added, Removed: removed, AutoJoinedMembers: autoJoined,
	})
}

// ---------- 工具 ----------

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func strs(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func equalList(a, b []string) bool {
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

func changedFields(before, after *model.Client) []string {
	out := []string{}
	if before.ClientName != after.ClientName {
		out = append(out, "clientName")
	}
	if deref(before.Description) != deref(after.Description) {
		out = append(out, "description")
	}
	if deref(before.LogoURL) != deref(after.LogoURL) {
		out = append(out, "logoUrl")
	}
	if before.AccessTokenTTL != after.AccessTokenTTL {
		out = append(out, "accessTokenTtl")
	}
	if before.RefreshTokenTTL != after.RefreshTokenTTL {
		out = append(out, "refreshTokenTtl")
	}
	if before.Status != after.Status {
		out = append(out, "status")
	}
	return out
}

// snapshotIDs 生成审计快照：≤20 项直接给清单，否则记数量 + 前 20 项。
func snapshotIDs(ids []string) map[string]any {
	if len(ids) <= 20 {
		return map[string]any{"ids": strs(ids), "count": len(ids)}
	}
	return map[string]any{"count": len(ids), "sample": ids[:20]}
}

func toPermissionItem(it service.PermissionItem) dto.AppPermissionItem {
	return dto.AppPermissionItem{
		ID: it.ID, ClientID: it.ClientID, ParentID: it.ParentID, Code: it.Code, Name: it.Name,
		Description: deref(it.Description), Sort: it.Sort, ChildCount: it.ChildCount, RoleCount: it.RoleCount,
		CreatedAt: dto.Time(it.CreatedAt), UpdatedAt: it.UpdatedAt,
	}
}

func currentUserID(c *gin.Context) string {
	if v, ok := c.Get(middleware.CtxUser); ok {
		if u, ok := v.(*model.User); ok {
			return u.ID
		}
	}
	return ""
}
