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

// OrgNode 组织树节点（GET /api/v1/admin/orgs/tree 的元素）。
// 报文结构为既有契约，不得随本特性的新增端点变化（spec FR-015）；
// userCount 的既有语义是**直属人数**（不含子部门），写操作详情响应中的
// userCount 才是子树汇总，两者不可混淆。
type OrgNode struct {
	ID        string    `json:"id" example:"01J9ZJ8Q2M4N6P8R0T2V4X6Z8A"`
	Name      string    `json:"name" example:"总公司"`
	ParentID  *string   `json:"parentId"`
	Path      string    `json:"path" example:"/01J9ZJ8Q2M4N6P8R0T2V4X6Z8A/"`
	UserCount int64     `json:"userCount" example:"3"`
	Children  []OrgNode `json:"children"`
}

// OrgTree GET /api/v1/admin/orgs/tree
//
// @Summary      查询组织架构树
// @Description  返回完整组织树（根节点为 parentId=null）。userCount 为该部门的**直属用户数**（不含子部门），此语义为既有契约，保持不变；写操作详情响应中的 userCount 才是子树汇总。
// @Tags         组织架构
// @Produce      json
// @Security     cookieAuth
// @Success      200  {object}  response.Body{data=[]OrgNode}  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/orgs/tree [get]
func (h *Handler) OrgTree(c *gin.Context) {
	orgs, counts, err := h.orgs.Tree(c.Request.Context(), "")
	if err != nil {
		h.log.Error("load org tree", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	response.Success(c, buildTree(orgs, counts))
}

func buildTree(orgs []model.Organization, counts map[string]int64) []OrgNode {
	children := make(map[string][]model.Organization)
	for _, o := range orgs {
		if o.ParentID != nil {
			children[*o.ParentID] = append(children[*o.ParentID], o)
		}
	}
	var walk func(o model.Organization) OrgNode
	walk = func(o model.Organization) OrgNode {
		n := OrgNode{
			ID: o.ID, Name: o.Name, ParentID: o.ParentID, Path: o.Path,
			UserCount: counts[o.ID], Children: []OrgNode{},
		}
		for _, ch := range children[o.ID] {
			n.Children = append(n.Children, walk(ch))
		}
		return n
	}
	roots := make([]OrgNode, 0)
	for _, o := range orgs {
		if o.ParentID == nil {
			roots = append(roots, walk(o))
		}
	}
	return roots
}

// optionalString 区分 JSON 的三种状态：字段缺失（Set=false）、显式 null
// （Set=true 且 Value=nil）、字符串值（Set=true 且 Value 非空）。
// 普通 *string 无法区分"未提交"与"显式清空"，而 parentId 的 null 语义是
// "移到根级"（contracts §2）。
type optionalString struct {
	Set   bool
	Value *string
}

// UnmarshalJSON 只要被调用即说明字段出现（Set=true）。
func (o *optionalString) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if string(raw) == "null" {
		o.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// CreateOrgRequest 规范 §4.6 / contracts §1 请求体。
// tenantId 不属于契约：结构体无该字段，提交后被绑定忽略。
type CreateOrgRequest struct {
	ParentID *string `json:"parentId" example:"01J9ZJ7A1B2C3D4E5F6G7H8J9K"`
	Name     string  `json:"name" binding:"required" example:"平台工程组"`
	Sort     *int    `json:"sort" example:"3"`
}

// CreateOrg POST /api/v1/admin/orgs
//
// @Summary      创建组织节点
// @Description  在指定上级部门下（parentId 缺失或 null 表示根级）创建部门；名称去首尾空白后 1–64 字符且同级唯一（区分大小写），层级最多 10 级。租户由父链派生（根级为 default），请求中的 tenantId 被忽略。成功写入 org_created 审计事件。
// @Tags         组织架构
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  CreateOrgRequest  true  "创建组织节点请求体"
// @Success      201   {object}  response.Body{data=dto.OrgDetail}  "创建成功"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / ORG_INVALID_PARENT"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      409   {object}  response.Body  "ORG_NAME_ALREADY_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/orgs [post]
func (h *Handler) CreateOrg(c *gin.Context) {
	var req CreateOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	o, err := h.orgs.Create(c.Request.Context(), service.CreateOrgInput{
		ParentID: req.ParentID,
		Name:     req.Name,
		Sort:     req.Sort,
	})
	if err != nil {
		h.failOrg(c, model.EventOrgCreated, map[string]any{"name": service.NormalizeOrgName(req.Name)}, err)
		return
	}
	h.emitOrg(c, model.EventOrgCreated, "success", map[string]any{
		"orgId": o.ID, "name": o.Name, "parentId": o.ParentID,
	})
	response.Created(c, dto.FromOrganization(o, 0))
}

// UpdateOrgRequest 规范 §4.6 / contracts §2 请求体（全部可选）。
// name / sort 缺失表示不变；parentId 三态见 optionalString。
type UpdateOrgRequest struct {
	Name     *string        `json:"name" example:"平台工程组"`
	Sort     *int           `json:"sort" example:"1"`
	ParentID optionalString `json:"parentId" swaggertype:"string" extensions:"x-nullable" example:"01J9ZJ7A1B2C3D4E5F6G7H8J9K"`
}

// UpdateOrg PATCH /api/v1/admin/orgs/:id
//
// @Summary      更新组织节点
// @Description  局部更新名称、排序值与上级部门。name / sort 缺失表示不变；parentId 为三态：缺失=不变、null=移到根级、值=移到该节点下（须存在、非自身或自身后代、同租户），移动时整棵子树的路径同步搬迁且排序值不被改写。层级最多 10 级；名称与上级同时非法时整体失败。成功写入 org_updated 审计事件。
// @Tags         组织架构
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "组织节点 ID（ULID）"
// @Param        body  body  UpdateOrgRequest  true  "更新组织节点请求体"
// @Success      200   {object}  response.Body{data=dto.OrgDetail}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / ORG_INVALID_PARENT"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      409   {object}  response.Body  "ORG_NAME_ALREADY_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/orgs/{id} [patch]
func (h *Handler) UpdateOrg(c *gin.Context) {
	var req UpdateOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	orgID := c.Param("id")
	in := service.UpdateOrgInput{Name: req.Name, Sort: req.Sort}
	if req.ParentID.Set {
		in.ParentIDSet = true
		in.ParentID = req.ParentID.Value
	}
	o, err := h.orgs.Update(c.Request.Context(), orgID, in)
	if err != nil {
		h.failOrg(c, model.EventOrgUpdated, map[string]any{"orgId": orgID}, err)
		return
	}
	count, err := h.orgs.UserCountInSubtree(c.Request.Context(), o.ID)
	if err != nil {
		h.log.Error("count users in subtree", "err", err, "org_id", o.ID)
		count = 0
	}
	changed := make([]string, 0, 3)
	if req.Name != nil {
		changed = append(changed, "name")
	}
	if req.Sort != nil {
		changed = append(changed, "sort")
	}
	if req.ParentID.Set {
		changed = append(changed, "parentId")
	}
	if len(changed) > 0 {
		h.emitOrg(c, model.EventOrgUpdated, "success", map[string]any{
			"orgId": o.ID, "changed": changed,
		})
	}
	response.Success(c, dto.FromOrganization(o, count))
}

// DeleteOrg DELETE /api/v1/admin/orgs/:id
//
// @Summary      删除组织节点
// @Description  物理删除组织节点；仅当该部门既无子部门也无账号归属时允许。有子部门返回 ORG_HAS_CHILDREN，有账号返回 ORG_HAS_USERS；目标不存在（含重复删除）返回 NOT_FOUND。成功与业务拒绝均写入 org_deleted 审计事件。
// @Tags         组织架构
// @Produce      json
// @Security     cookieAuth
// @Param        id  path  string  true  "组织节点 ID（ULID）"
// @Success      200  {object}  response.Body  "success"
// @Failure      401  {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403  {object}  response.Body  "FORBIDDEN"
// @Failure      404  {object}  response.Body  "NOT_FOUND"
// @Failure      409  {object}  response.Body  "ORG_HAS_CHILDREN / ORG_HAS_USERS"
// @Failure      500  {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503  {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/orgs/{id} [delete]
func (h *Handler) DeleteOrg(c *gin.Context) {
	orgID := c.Param("id")
	// 删除后无法再读出名称，审计摘要用的 name 在删除前取一次。
	fields := map[string]any{"orgId": orgID}
	if name := h.orgs.Name(c.Request.Context(), orgID); name != "" {
		fields["name"] = name
	}
	if err := h.orgs.Delete(c.Request.Context(), orgID); err != nil {
		h.failOrg(c, model.EventOrgDeleted, fields, err)
		return
	}
	h.emitOrg(c, model.EventOrgDeleted, "success", fields)
	response.Success(c, nil)
}

// emitOrg 投递组织写操作的审计事件。操作者取当前管理员会话（规范 §6.7）。
func (h *Handler) emitOrg(c *gin.Context, eventType, result string, fields map[string]any) {
	ev := service.Event{
		Type:      eventType,
		Result:    result,
		Fields:    fields,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if u, ok := middleware.CurrentUser(c); ok {
		ev.UserID = u.ID
		ev.Username = u.Username
	}
	h.audit.Emit(c.Request.Context(), ev)
}

// failOrg 把 service 哨兵错误映射为登记的错误码。
// 审计划分以条件为准而非 HTTP 状态码（FR-014）：同级重名、存在子部门、
// 存在账号、目标不存在、非法上级、层级超限都属业务拒绝，写 failure；
// 名称/排序值等输入格式错误只记应用日志，不写审计。
func (h *Handler) failOrg(c *gin.Context, eventType string, fields map[string]any, err error) {
	apiErr, businessReject := mapOrgError(err)
	if apiErr == nil {
		h.log.Error("organization write failed", "event", eventType, "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	if businessReject {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["message"] = apiErr.Message
		h.emitOrg(c, eventType, "failure", fields)
	}
	response.Fail(c, apiErr)
}

// mapOrgError 返回对外的错误定义，以及该错误是否属于需要留痕的业务拒绝。
func mapOrgError(err error) (*response.Error, bool) {
	switch {
	case errors.Is(err, service.ErrOrgNotFound):
		return response.ErrNotFound, true
	case errors.Is(err, service.ErrOrgNameExists):
		return response.ErrOrgNameExists, true
	case errors.Is(err, service.ErrOrgInvalidParent):
		return response.ErrOrgInvalidParent, true
	case errors.Is(err, service.ErrOrgHasChildren):
		return response.ErrOrgHasChildren, true
	case errors.Is(err, service.ErrOrgHasUsers):
		return response.ErrOrgHasUsers, true
	case errors.Is(err, service.ErrOrgDepthExceeded):
		return response.ErrValidation.WithMessage(service.OrgDepthExceededMessage), true
	case errors.Is(err, service.ErrOrgInvalidName), errors.Is(err, service.ErrOrgInvalidSort):
		return response.ErrValidation, false
	default:
		return nil, false
	}
}
