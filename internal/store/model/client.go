package model

// Client 对应 clients 表（AGENTS.md §6.3）。
type Client struct {
	ClientID         string     `gorm:"column:client_id;primaryKey"`
	ClientName       string     `gorm:"column:client_name"`
	Description      *string    `gorm:"column:description"`
	LogoURL          *string    `gorm:"column:logo_url"`
	ClientSecretHash []byte     `gorm:"column:client_secret_hash"`
	IsPublic         bool       `gorm:"column:is_public"`
	RedirectURIs     StringList `gorm:"column:redirect_uris"`
	PostLogoutURIs   StringList `gorm:"column:post_logout_uris"`
	GrantTypes       StringList `gorm:"column:grant_types"`
	Scopes           StringList `gorm:"column:scopes"`
	AccessTokenTTL   int        `gorm:"column:access_token_ttl"`
	RefreshTokenTTL  int        `gorm:"column:refresh_token_ttl"`
	TenantID         string     `gorm:"column:tenant_id"`
	Status           int8       `gorm:"column:status"`
	CreatedAt        int64      `gorm:"column:created_at"`
	UpdatedAt        int64      `gorm:"column:updated_at"`
}

func (Client) TableName() string { return "clients" }
