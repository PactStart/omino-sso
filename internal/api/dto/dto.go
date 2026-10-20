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
