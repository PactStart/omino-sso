package model

// ClientMember 对应 client_members 表（AGENTS.md §6.8）。
// 复合主键 (client_id, user_id)：同一用户在同一应用内只有一条成员关系。
type ClientMember struct {
	ClientID string  `gorm:"column:client_id;primaryKey"`
	UserID   string  `gorm:"column:user_id;primaryKey"`
	AddedBy  *string `gorm:"column:added_by"`
	JoinedAt int64   `gorm:"column:joined_at"`
	// IsAdmin 标记该成员是否为该应用的应用管理员（006-app-admin-role）。
	// 授予与取消只在 admin 端由平台管理员执行；随成员行删除自动失效。
	IsAdmin bool `gorm:"column:is_admin"`
}

func (ClientMember) TableName() string { return "client_members" }

// ClientRole 对应 client_roles 表：应用内角色，name 在应用内唯一且大小写敏感。
type ClientRole struct {
	ID          string  `gorm:"column:id;primaryKey"`
	ClientID    string  `gorm:"column:client_id"`
	Name        string  `gorm:"column:name"`
	Description *string `gorm:"column:description"`
	CreatedAt   int64   `gorm:"column:created_at"`
	UpdatedAt   int64   `gorm:"column:updated_at"`
}

func (ClientRole) TableName() string { return "client_roles" }

// ClientPermission 对应 client_permissions 表：应用内权限点，code 在应用内唯一且大小写敏感。
type ClientPermission struct {
	ID          string  `gorm:"column:id;primaryKey"`
	ClientID    string  `gorm:"column:client_id"`
	ParentID    *string `gorm:"column:parent_id"`
	Code        string  `gorm:"column:code"`
	Name        string  `gorm:"column:name"`
	Description *string `gorm:"column:description"`
	Sort        int     `gorm:"column:sort"`
	CreatedAt   int64   `gorm:"column:created_at"`
	UpdatedAt   int64   `gorm:"column:updated_at"`
}

func (ClientPermission) TableName() string { return "client_permissions" }

// ClientRolePermission 对应 client_role_permissions 表。
type ClientRolePermission struct {
	RoleID       string `gorm:"column:role_id;primaryKey"`
	PermissionID string `gorm:"column:permission_id;primaryKey"`
	ClientID     string `gorm:"column:client_id"`
	CreatedAt    int64  `gorm:"column:created_at"`
}

func (ClientRolePermission) TableName() string { return "client_role_permissions" }

// ClientRoleMember 对应 client_role_members 表。
type ClientRoleMember struct {
	RoleID    string `gorm:"column:role_id;primaryKey"`
	UserID    string `gorm:"column:user_id;primaryKey"`
	ClientID  string `gorm:"column:client_id"`
	CreatedAt int64  `gorm:"column:created_at"`
}

func (ClientRoleMember) TableName() string { return "client_role_members" }

// AccessToken 对应 access_tokens 表：令牌 → 应用的上下文映射（research R2）。
// 不存令牌明文，不承担吊销职责。
type AccessToken struct {
	TokenID   string `gorm:"column:token_id;primaryKey"`
	Sub       string `gorm:"column:sub"`
	ClientID  string `gorm:"column:client_id"`
	ExpiresAt int64  `gorm:"column:expires_at"`
	CreatedAt int64  `gorm:"column:created_at"`
}

func (AccessToken) TableName() string { return "access_tokens" }
