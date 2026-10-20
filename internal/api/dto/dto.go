package dto

import (
	"time"

	"github.com/omino/omino-sso/internal/store/model"
)

// Time 把 Unix 毫秒转换为 RFC3339（UTC）。
func Time(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// TimePtr 处理可空时间。
func TimePtr(ms *int64) *string {
	if ms == nil || *ms <= 0 {
		return nil
	}
	s := Time(*ms)
	return &s
}

// UserBrief 用户摘要。
type UserBrief struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// UserDetail 用户详情（规范 §4.5）。
type UserDetail struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       *string  `json:"email"`
	Phone       *string  `json:"phone"`
	AvatarURL   *string  `json:"avatarUrl"`
	DeptID      string   `json:"deptId"`
	DeptPath    []string `json:"deptPath"`
	TenantID    string   `json:"tenantId"`
	Roles       []string `json:"roles"`
	MFAEnabled  bool     `json:"mfaEnabled"`
	MFARequired bool     `json:"mfaRequired"`
	LastLoginAt *string  `json:"lastLoginAt"`
	CreatedAt   string   `json:"createdAt"`
}

// UserAdmin 管理端用户视图（规范 §4.4 响应字段）。
type UserAdmin struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       *string  `json:"email"`
	Phone       *string  `json:"phone"`
	DeptID      string   `json:"deptId"`
	TenantID    string   `json:"tenantId"`
	Status      string   `json:"status"`
	MFAEnabled  bool     `json:"mfaEnabled"`
	MFARequired bool     `json:"mfaRequired"`
	Roles       []string `json:"roles"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// StatusText 状态码转文本。
func StatusText(s int8) string {
	switch s {
	case model.UserStatusActive:
		return "active"
	case model.UserStatusDisabled:
		return "disabled"
	case model.UserStatusLocked:
		return "locked"
	default:
		return "unknown"
	}
}

// Brief 构造用户摘要。
func Brief(u *model.User) UserBrief {
	return UserBrief{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName}
}

// Admin 构造管理端用户视图。
func Admin(u *model.User) UserAdmin {
	roles := []string(u.Roles)
	if roles == nil {
		roles = []string{}
	}
	return UserAdmin{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Phone:       u.Phone,
		DeptID:      u.DeptID,
		TenantID:    u.TenantID,
		Status:      StatusText(u.Status),
		MFAEnabled:  u.MFAEnabled,
		MFARequired: u.MFARequired,
		Roles:       roles,
		CreatedAt:   Time(u.CreatedAt),
		UpdatedAt:   Time(u.UpdatedAt),
	}
}

// Detail 构造用户详情。
func Detail(u *model.User, deptPath []string) UserDetail {
	roles := []string(u.Roles)
	if roles == nil {
		roles = []string{}
	}
	if deptPath == nil {
		deptPath = []string{}
	}
	return UserDetail{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Phone:       u.Phone,
		AvatarURL:   u.AvatarURL,
		DeptID:      u.DeptID,
		DeptPath:    deptPath,
		TenantID:    u.TenantID,
		Roles:       roles,
		MFAEnabled:  u.MFAEnabled,
		MFARequired: u.MFARequired,
		LastLoginAt: TimePtr(u.LastLoginAt),
		CreatedAt:   Time(u.CreatedAt),
	}
}

// Client 应用视图。
type Client struct {
	ClientID       string   `json:"clientId"`
	ClientName     string   `json:"clientName"`
	Description    string   `json:"description"`
	LogoURL        string   `json:"logoUrl"`
	IsPublic       bool     `json:"isPublic"`
	RedirectURIs   []string `json:"redirectUris"`
	GrantTypes     []string `json:"grantTypes"`
	Scopes         []string `json:"scopes"`
	PostLogoutURIs []string `json:"postLogoutUris"`
	CreatedAt      string   `json:"createdAt"`
}

// FromClient 构造应用视图。
func FromClient(c *model.Client) Client {
	return Client{
		ClientID:       c.ClientID,
		ClientName:     c.ClientName,
		Description:    deref(c.Description),
		LogoURL:        deref(c.LogoURL),
		IsPublic:       c.IsPublic,
		RedirectURIs:   emptyIfNil(c.RedirectURIs),
		GrantTypes:     emptyIfNil(c.GrantTypes),
		Scopes:         emptyIfNil(c.Scopes),
		PostLogoutURIs: emptyIfNil(c.PostLogoutURIs),
		CreatedAt:      Time(c.CreatedAt),
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func emptyIfNil(s model.StringList) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- 以下类型用于替换历史上的 gin.H 匿名响应 ----
// json tag 与既有的 gin.H 键逐字对应；不得添加 omitempty（mfaTicket、expiresAt
// 等键的 null 是有意的空值语义）。

// LoginMfaChallenge 登录第一步命中 MFA 时的响应载荷。
// 键集只含 mfaRequired / mfaTicket / expiresIn 三项（规范 §4.3）。
type LoginMfaChallenge struct {
	MFARequired bool   `json:"mfaRequired" example:"true"`
	MFATicket   string `json:"mfaTicket" example:"mft_5c1e9a7b3d2f"`
	ExpiresIn   int    `json:"expiresIn" example:"300"`
}

// LoginResult 登录完成时的响应载荷。
// 键集固定为五项，mfaTicket 恒为 null（规范 §4.3）。
type LoginResult struct {
	MFARequired bool      `json:"mfaRequired" example:"false"`
	MFATicket   *string   `json:"mfaTicket"`
	ExpiresIn   int       `json:"expiresIn" example:"0"`
	RedirectTo  string    `json:"redirectTo" example:"/account"`
	User        UserBrief `json:"user"`
}

// MfaResult MFA 二次校验通过后的响应载荷。
type MfaResult struct {
	RedirectTo string    `json:"redirectTo" example:"/account"`
	User       UserBrief `json:"user"`
}

// AppItem 我可访问的应用（GET /api/v1/me/apps 的元素）。
type AppItem struct {
	ClientID     string  `json:"clientId" example:"portal"`
	Name         string  `json:"name" example:"统一门户"`
	Description  string  `json:"description" example:"员工自助门户"`
	LogoURL      string  `json:"logoUrl" example:"https://cdn.example.com/logo/portal.png"`
	HomeURL      string  `json:"homeUrl" example:"https://portal.example.com"`
	LastAccessAt *string `json:"lastAccessAt"`
}

// SessionItem 登录设备（GET /api/v1/me/sessions 的元素）。
type SessionItem struct {
	SID        string  `json:"sid" example:"sess_01J9ZK3A7B5C7D9E1F3G5H7J9K"`
	IP         string  `json:"ip" example:"203.0.113.10"`
	UserAgent  *string `json:"userAgent"`
	CreatedAt  string  `json:"createdAt" example:"2026-10-20T09:00:00Z"`
	LastSeenAt string  `json:"lastSeenAt" example:"2026-10-20T10:00:00Z"`
	Current    bool    `json:"current" example:"true"`
}

// CreatedClient 新建应用后的响应：clientSecret 仅此一次返回，nil 表示公开客户端。
type CreatedClient struct {
	ClientID     string  `json:"clientId" example:"portal"`
	ClientSecret *string `json:"clientSecret"`
}

// RotatedSecret 轮换密钥后的响应。expiresAt 恒为 null，但必须保留键位。
type RotatedSecret struct {
	ClientID     string  `json:"clientId" example:"portal"`
	ClientSecret string  `json:"clientSecret"`
	ExpiresAt    *string `json:"expiresAt"`
}

// AuditItem 审计事件（GET /api/v1/admin/audit-logs 的元素）。
type AuditItem struct {
	ID        string  `json:"id" example:"01J9ZK5B9C1D3E5F7G9H1J3K5M"`
	EventType string  `json:"eventType" example:"login_success"`
	UserID    *string `json:"userId"`
	Username  *string `json:"username"`
	ClientID  *string `json:"clientId"`
	IP        string  `json:"ip" example:"203.0.113.10"`
	UserAgent *string `json:"userAgent"`
	Result    string  `json:"result" example:"success"`
	Detail    *string `json:"detail"`
	CreatedAt string  `json:"createdAt" example:"2026-10-20T10:12:33Z"`
}
