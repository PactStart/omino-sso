package model

// AuthCode 对应 auth_codes 表（AGENTS.md §6.4）。
// 授权码只存 SHA-256，禁止软删除。
type AuthCode struct {
	CodeHash            []byte     `gorm:"column:code_hash;primaryKey"`
	ClientID            string     `gorm:"column:client_id"`
	UserID              string     `gorm:"column:user_id"`
	RedirectURI         string     `gorm:"column:redirect_uri"`
	Scope               string     `gorm:"column:scope"`
	Nonce               *string    `gorm:"column:nonce"`
	CodeChallenge       string     `gorm:"column:code_challenge"`
	CodeChallengeMethod string     `gorm:"column:code_challenge_method"`
	AuthTime            int64      `gorm:"column:auth_time"`
	AMR                 StringList `gorm:"column:amr"`
	ExpiresAt           int64      `gorm:"column:expires_at"`
	Used                bool       `gorm:"column:used"`
	CreatedAt           int64      `gorm:"column:created_at"`
}

func (AuthCode) TableName() string { return "auth_codes" }
