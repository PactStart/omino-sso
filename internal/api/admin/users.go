package admin

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// Handler 处理 /api/v1/admin/*。
// 只依赖 service，不持有 store：读路径同样经 service（宪法原则 III）。
type Handler struct {
	users  *service.UserService
	client *service.ClientService
	orgs   *service.OrgService
	audit  *service.Auditor
	log    *slog.Logger
}

// NewHandler 构造。
func NewHandler(users *service.UserService, clients *service.ClientService, orgs *service.OrgService, audit *service.Auditor, logger *slog.Logger) *Handler {
	return &Handler{users: users, client: clients, orgs: orgs, audit: audit, log: logger}
}

// ListUsers GET /api/v1/admin/users
//
// @Summary      分页查询用户
// @Description  按关键字、部门与状态筛选用户，返回分页结果；keyword 同时匹配 username 与 displayName。
// @Tags         用户管理
// @Produce      json
// @Security     cookieAuth
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 100"
// @Param        keyword   query  string  false  "用户名或显示名关键字"
// @Param        deptId    query  string  false  "部门 ID"
// @Param        status    query  string  false  "状态"  Enums(active, disabled, locked)
// @Success      200       {object}  response.Body{data=response.Page{list=[]dto.UserAdmin,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400       {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401       {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403       {object}  response.Body  "FORBIDDEN"
// @Failure      500       {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503       {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/users [get]
func (h *Handler) ListUsers(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f := service.UserListFilter{
		Keyword:  c.Query("keyword"),
		DeptID:   c.Query("deptId"),
		Page:     page,
		PageSize: pageSize,
	}
	if s := c.Query("status"); s != "" {
		var status int8
		switch s {
		case "active":
			status = model.UserStatusActive
		case "disabled":
			status = model.UserStatusDisabled
		case "locked":
			status = model.UserStatusLocked
		default:
			response.Fail(c, response.ErrValidation)
			return
		}
		f.Status = &status
	}
	list, total, err := h.users.List(c.Request.Context(), f)
	if err != nil {
		h.log.Error("list users", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := make([]dto.UserAdmin, 0, len(list))
	for i := range list {
		out = append(out, dto.Admin(&list[i]))
	}
	response.Success(c, response.Page{List: out, Total: total, Page: page, PageSize: pageSize})
}

// CreateUserRequest 规范 §4.4 请求体。
type CreateUserRequest struct {
	Username    string   `json:"username" binding:"required,min=3,max=64" example:"lisi"`
	DisplayName string   `json:"displayName" binding:"required,min=1,max=64" example:"李四"`
	Email       string   `json:"email" binding:"omitempty,email,max=128" example:"lisi@example.com"`
	Phone       string   `json:"phone" binding:"omitempty,max=20" example:"13900000000"`
	Password    string   `json:"password" binding:"required,min=8,max=128" example:"Init!pass2026"`
	DeptID      string   `json:"deptId" binding:"required,len=26" example:"01J9ZJ7A1B2C3D4E5F6G7H8J9K"`
	TenantID    string   `json:"tenantId" example:"default"`
	RoleIDs     []string `json:"roleIds" example:"user"`
	MFARequired bool     `json:"mfaRequired" example:"false"`
	SendNotify  bool     `json:"sendNotify" example:"false"`
}

// CreateUser POST /api/v1/admin/users
//
// @Summary      创建用户
// @Description  写入用户并绑定组织与角色，同时写入 user_created 审计事件。username 与 email 全局唯一；密码按强度规则校验，弱密码返回 WEAK_PASSWORD；deptId 必须存在。sendNotify 本阶段恒不生效（不发通知）。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  CreateUserRequest  true  "创建用户请求体"
// @Success      201   {object}  response.Body{data=dto.UserAdmin}  "创建成功"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / WEAK_PASSWORD"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      409   {object}  response.Body  "USERNAME_ALREADY_EXISTS / EMAIL_ALREADY_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/users [post]
func (h *Handler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	u, err := h.users.CreateUser(c.Request.Context(), service.CreateUserInput{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		Password:    req.Password,
		DeptID:      req.DeptID,
		TenantID:    req.TenantID,
		Roles:       req.RoleIDs,
		MFARequired: req.MFARequired,
	})
	switch {
	case err == nil:
	case errors.Is(err, service.ErrWeakPassword):
		response.Fail(c, response.ErrWeakPassword)
		return
	case errors.Is(err, service.ErrUsernameExists):
		response.Fail(c, response.ErrUsernameExists)
		return
	case errors.Is(err, service.ErrDeptNotFound):
		response.Fail(c, response.ErrValidation.WithMessage("部门不存在"))
		return
	default:
		h.log.Error("create user", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventUserCreated, UserID: u.ID, Username: u.Username,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})
	response.Created(c, dto.Admin(u))
}

// UpdateUserRequest PATCH /api/v1/admin/users/:id
type UpdateUserRequest struct {
	DisplayName *string   `json:"displayName" example:"李四"`
	Email       *string   `json:"email" example:"lisi@example.com"`
	Phone       *string   `json:"phone" example:"13900000000"`
	DeptID      *string   `json:"deptId" example:"01J9ZJ7A1B2C3D4E5F6G7H8J9K"`
	Status      *string   `json:"status" example:"active"`
	MFARequired *bool     `json:"mfaRequired" example:"false"`
	RoleIDs     *[]string `json:"roleIds" example:"user"`
}

// UpdateUser 更新用户。
//
// @Summary      更新用户
// @Description  局部更新用户资料、组织、状态、角色与 MFA 策略；未传的字段保持不变（指针语义），写入 user_updated 审计事件。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "用户 ID（ULID）"
// @Param        body  body  UpdateUserRequest  true  "更新用户请求体"
// @Success      200   {object}  response.Body{data=dto.UserAdmin}  "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/users/{id} [patch]
func (h *Handler) UpdateUser(c *gin.Context) {
	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	in := service.UpdateUserInput{
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		DeptID:      req.DeptID,
		MFARequired: req.MFARequired,
		Roles:       req.RoleIDs,
	}
	if req.Status != nil {
		var status int8
		switch *req.Status {
		case "active":
			status = model.UserStatusActive
		case "disabled":
			status = model.UserStatusDisabled
		case "locked":
			status = model.UserStatusLocked
		default:
			response.Fail(c, response.ErrValidation)
			return
		}
		in.Status = &status
	}
	u, err := h.users.UpdateUser(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		// deptId 由服务端校验存在性（行为修正：过去静默写入不存在的部门）。
		if errors.Is(err, service.ErrDeptNotFound) {
			response.Fail(c, response.ErrValidation.WithMessage("部门不存在"))
			return
		}
		h.log.Error("update user", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventUserUpdated, UserID: u.ID, Username: u.Username,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})
	response.Success(c, dto.Admin(u))
}

func pagination(c *gin.Context) (int, int, error) {
	page, err := strconv.Atoi(defaultStr(c.Query("page"), "1"))
	if err != nil || page < 1 {
		return 0, 0, errors.New("invalid page")
	}
	size, err := strconv.Atoi(defaultStr(c.Query("pageSize"), "20"))
	if err != nil || size < 1 || size > 100 {
		return 0, 0, errors.New("invalid pageSize")
	}
	return page, size, nil
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
