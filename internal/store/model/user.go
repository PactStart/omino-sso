package model

// User 对应 users 表（AGENTS.md §6.1）。本表不使用软删除。
type User struct {
	ID                string     `gorm:"column:id;primaryKey"`
	Username          string     `gorm:"column:username"`
	DisplayName       string     `gorm:"column:display_name"`
	Email             *string    `gorm:"column:email"`
	Phone             *string    `gorm:"column:phone"`
	AvatarURL         *string    `gorm:"column:avatar_url"`
	PasswordHash      string     `gorm:"column:password_hash"`
	DeptID            string     `gorm:"column:dept_id"`
	TenantID          string     `gorm:"column:tenant_id"`
	Roles             StringList `gorm:"column:roles"`
	Status            int8       `gorm:"column:status"`
	MFAEnabled        bool       `gorm:"column:mfa_enabled"`
	MFARequired       bool       `gorm:"column:mfa_required"`
	MFASecret         []byte     `gorm:"column:mfa_secret"`
	FailedAttempts    int32      `gorm:"column:failed_attempts"`
	LockedUntil       *int64     `gorm:"column:locked_until"`
	LastLoginAt       *int64     `gorm:"column:last_login_at"`
	PasswordUpdatedAt int64      `gorm:"column:password_updated_at"`
	CreatedAt         int64      `gorm:"column:created_at"`
	UpdatedAt         int64      `gorm:"column:updated_at"`
}

func (User) TableName() string { return "users" }

// 用户状态枚举。
const (
	UserStatusDisabled int8 = 0
	UserStatusActive   int8 = 1
	UserStatusLocked   int8 = 2
)
