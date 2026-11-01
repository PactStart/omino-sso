package model

// Client 对应 clients 表（AGENTS.md §6.3）。
//
// HomeURL 是应用首页地址（门户「我的应用」入口），注册与编辑均必填；
// 存量行的值由 migrations/0008 回填为首个回调地址（redirect_uris[0]）的 origin。
type Client struct {
	ClientID         string     `gorm:"column:client_id;primaryKey"`
	ClientName       string     `gorm:"column:client_name"`
	Description      *string    `gorm:"column:description"`
	LogoURL          *string    `gorm:"column:logo_url"`
	HomeURL          *string    `gorm:"column:home_url"`
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
