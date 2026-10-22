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
	// ErrAppMemberNotFound 用于「把非成员设为应用管理员」等成员维度操作（006）。
	ErrAppMemberNotFound = &Error{http.StatusNotFound, "APP_MEMBER_NOT_FOUND", "该用户不是本应用成员，请先添加成员"}

	// 多通道验证与账号安全（005-verification-code-auth，规范 §5.3）。
	ErrAuthCodeInvalid  = &Error{http.StatusBadRequest, "AUTH_CODE_INVALID", "验证码错误或已过期"}
	ErrVerifyTargetUsed = &Error{http.StatusConflict, "VERIFY_TARGET_IN_USE", "该手机号或邮箱已被其他账号使用"}
	ErrVerifySendFailed = &Error{http.StatusBadGateway, "VERIFY_SEND_FAILED", "验证码发送失败，请稍后重试"}
	// ErrVerifyTargetNotFound 用于登录场景：目标联系方式未绑定任何账号。
	// 该场景放弃防枚举（2026-10-22 行为修正，见规范 §4.1），换取可诊断的提示；
	// find_password 场景仍保持静默成功。
	ErrVerifyTargetNotFound = &Error{http.StatusNotFound, "VERIFY_TARGET_NOT_FOUND", "该手机号或邮箱未绑定任何账号，请改用密码登录或联系管理员"}
	ErrMFAAlreadyBound      = &Error{http.StatusConflict, "MFA_ALREADY_BOUND", "已绑定多因素认证"}
	ErrMFANotBound          = &Error{http.StatusBadRequest, "MFA_NOT_BOUND", "尚未绑定多因素认证"}

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
