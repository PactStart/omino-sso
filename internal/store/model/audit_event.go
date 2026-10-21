package model

// AuditEvent 对应 audit_events 表（AGENTS.md §6.7）。
type AuditEvent struct {
	ID        string  `gorm:"column:id;primaryKey"`
	EventType string  `gorm:"column:event_type"`
	UserID    *string `gorm:"column:user_id"`
	Username  *string `gorm:"column:username"`
	ClientID  *string `gorm:"column:client_id"`
	IP        string  `gorm:"column:ip"`
	UserAgent *string `gorm:"column:user_agent"`
	Result    string  `gorm:"column:result"`
	Detail    *string `gorm:"column:detail"`
	TraceID   *string `gorm:"column:trace_id"`
	CreatedAt int64   `gorm:"column:created_at"`
}

func (AuditEvent) TableName() string { return "audit_events" }

// 审计事件类型枚举（AGENTS.md §6.7）。
const (
	EventLoginSuccess        = "login_success"
	EventLoginFailure        = "login_failure"
	EventMFASuccess          = "mfa_success"
	EventMFAFailure          = "mfa_failure"
	EventLogout              = "logout"
	EventPasswordChanged     = "password_changed"
	EventTokenIssued         = "token_issued"
	EventTokenRevoked        = "token_revoked"
	EventCodeReused          = "code_reused"
	EventRefreshReused       = "refresh_reused"
	EventUserCreated         = "user_created"
	EventUserUpdated         = "user_updated"
	EventClientCreated       = "client_created"
	EventClientSecretRotated = "client_secret_rotated"
	EventOrgCreated          = "org_created"
	EventOrgUpdated          = "org_updated"
	EventOrgDeleted          = "org_deleted"

	// 应用内成员 / 角色 / 权限 / 授权（AGENTS.md §6.7，003 特性新增）。
	EventClientUpdated        = "client_updated"
	EventAppMemberAdded       = "app_member_added"
	EventAppMemberRemoved     = "app_member_removed"
	EventAppRoleCreated       = "app_role_created"
	EventAppRoleUpdated       = "app_role_updated"
	EventAppRoleDeleted       = "app_role_deleted"
	EventAppPermissionCreated = "app_permission_created"
	EventAppPermissionUpdated = "app_permission_updated"
	EventAppPermissionDeleted = "app_permission_deleted"
	EventAppGrantUpdated      = "app_grant_updated"
)
