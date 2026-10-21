package response

import "net/http"

// Error 是登记在规范 §5.3 错误码表中的错误定义。
type Error struct {
	HTTP    int
	Code    string
	Message string
}

var (
	ErrValidation         = &Error{http.StatusBadRequest, "VALIDATION_FAILED", "参数校验失败"}
	ErrUnauthenticated    = &Error{http.StatusUnauthorized, "UNAUTHENTICATED", "未登录或会话已失效"}
	ErrInvalidCredentials = &Error{http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误"}
	ErrCaptchaInvalid     = &Error{http.StatusBadRequest, "AUTH_CAPTCHA_INVALID", "验证码错误或已过期"}
	ErrMFAInvalid         = &Error{http.StatusBadRequest, "AUTH_MFA_INVALID", "动态验证码错误"}
	ErrMFATicketExpired   = &Error{http.StatusBadRequest, "AUTH_MFA_TICKET_EXPIRED", "验证票据已过期"}
	ErrAccountLocked      = &Error{http.StatusLocked, "AUTH_ACCOUNT_LOCKED", "账号已锁定，请稍后再试"}
	ErrAccountDisabled    = &Error{http.StatusForbidden, "AUTH_ACCOUNT_DISABLED", "账号已停用"}
	ErrForbidden          = &Error{http.StatusForbidden, "FORBIDDEN", "无权限执行该操作"}
	ErrNotFound           = &Error{http.StatusNotFound, "NOT_FOUND", "资源不存在"}
	ErrUsernameExists     = &Error{http.StatusConflict, "USERNAME_ALREADY_EXISTS", "用户名已存在"}
	ErrEmailExists        = &Error{http.StatusConflict, "EMAIL_ALREADY_EXISTS", "邮箱已存在"}
	ErrClientExists       = &Error{http.StatusConflict, "CLIENT_ALREADY_EXISTS", "应用标识已存在"}
	ErrWeakPassword       = &Error{http.StatusBadRequest, "WEAK_PASSWORD", "密码强度不足"}

	// 组织架构写操作（规范 §5.3）。
	ErrOrgNameExists    = &Error{http.StatusConflict, "ORG_NAME_ALREADY_EXISTS", "同级已存在同名部门"}
	ErrOrgHasChildren   = &Error{http.StatusConflict, "ORG_HAS_CHILDREN", "部门下仍有子部门"}
	ErrOrgHasUsers      = &Error{http.StatusConflict, "ORG_HAS_USERS", "部门下仍有账号"}
	ErrOrgInvalidParent = &Error{http.StatusBadRequest, "ORG_INVALID_PARENT", "上级部门不存在或不可作为上级"}

	// 应用内成员 / 角色 / 权限 / 授权（规范 §5.3）。
	ErrAppRoleNameExists          = &Error{http.StatusConflict, "APP_ROLE_NAME_EXISTS", "应用内已存在同名角色"}
	ErrAppPermissionCodeExists    = &Error{http.StatusConflict, "APP_PERMISSION_CODE_EXISTS", "应用内已存在同名权限编码"}
	ErrAppPermissionHasChildren   = &Error{http.StatusConflict, "APP_PERMISSION_HAS_CHILDREN", "权限下仍有子权限"}
	ErrAppPermissionInvalidParent = &Error{http.StatusBadRequest, "APP_PERMISSION_INVALID_PARENT", "上级权限不存在或不可作为上级"}
	ErrAppResourceStale           = &Error{http.StatusConflict, "APP_RESOURCE_STALE", "已被他人修改，请刷新后重试"}

	ErrRateLimited    = &Error{http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁"}
	ErrInternal       = &Error{http.StatusInternalServerError, "INTERNAL_ERROR", "服务内部错误"}
	ErrDependencyDown = &Error{http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "依赖服务不可用"}
)

// WithMessage 返回带自定义提示的副本（code 与 HTTP 状态不变）。
func (e *Error) WithMessage(msg string) *Error {
	cp := *e
	cp.Message = msg
	return &cp
}
