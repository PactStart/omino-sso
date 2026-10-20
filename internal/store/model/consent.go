package model

// Consent 对应 consents 表（AGENTS.md §6.8）。
type Consent struct {
	UserID    string     `gorm:"column:user_id;primaryKey"`
	ClientID  string     `gorm:"column:client_id;primaryKey"`
	Scopes    StringList `gorm:"column:scopes"`
	CreatedAt int64      `gorm:"column:created_at"`
	UpdatedAt int64      `gorm:"column:updated_at"`
}

func (Consent) TableName() string { return "consents" }
