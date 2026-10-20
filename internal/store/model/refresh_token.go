package model

// RefreshToken 对应 refresh_tokens 表（AGENTS.md §6.5）。
type RefreshToken struct {
	ID          string     `gorm:"column:id;primaryKey"`
	TokenHash   []byte     `gorm:"column:token_hash"`
	FamilyID    string     `gorm:"column:family_id"`
	UserID      string     `gorm:"column:user_id"`
	ClientID    string     `gorm:"column:client_id"`
	Scope       string     `gorm:"column:scope"`
	AMR         StringList `gorm:"column:amr"`
	AuthTime    int64      `gorm:"column:auth_time"`
	ExpiresAt   int64      `gorm:"column:expires_at"`
	Revoked     bool       `gorm:"column:revoked"`
	RevokedAt   *int64     `gorm:"column:revoked_at"`
	RotatedFrom *string    `gorm:"column:rotated_from"`
	CreatedAt   int64      `gorm:"column:created_at"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
