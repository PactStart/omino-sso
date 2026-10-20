package model

// Session 对应 sessions 表（AGENTS.md §6.6）。
// 主存 Redis，本表为审计副本。
type Session struct {
	SID        string     `gorm:"column:sid;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	TenantID   string     `gorm:"column:tenant_id"`
	AMR        StringList `gorm:"column:amr"`
	AuthTime   int64      `gorm:"column:auth_time"`
	IP         string     `gorm:"column:ip"`
	UserAgent  *string    `gorm:"column:user_agent"`
	CreatedAt  int64      `gorm:"column:created_at"`
	LastSeenAt int64      `gorm:"column:last_seen_at"`
	ExpiresAt  int64      `gorm:"column:expires_at"`
	Revoked    bool       `gorm:"column:revoked"`
}

func (Session) TableName() string { return "sessions" }
