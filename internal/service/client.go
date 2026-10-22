package service

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// ErrClientNotFound client 不存在。
var ErrClientNotFound = errors.New("client not found")

// AppItem 是"我的应用"列表项。
type AppItem struct {
	ClientID     string
	Name         string
	Description  string
	LogoURL      string
	HomeURL      string
	LastAccessAt *int64
	// IsAppAdmin 表示当前用户是「该应用的应用管理员」（门户「管理」按钮的唯一判据）。
	// 只来自 client_members.is_admin，平台 admin 角色不会置真（spec A2）。
	IsAppAdmin bool
}

// ClientService 应用（OAuth client）管理。
type ClientService struct {
	st *store.Store
}

// NewClientService 构造。
func NewClientService(st *store.Store) *ClientService {
	return &ClientService{st: st}
}

// CreateClientInput 注册应用入参。
type CreateClientInput struct {
	ClientName   string
	Description  string
	LogoURL      string
	RedirectURIs []string
	GrantTypes   []string
	Scopes       []string
	IsPublic     bool
	// FileID 为已上传（target_id 为空）的 logo 文件记录 ID；非空时在创建应用后回填 target_id 并写回 logo_url。
	FileID string
}

// Create 注册应用，返回客户端明文密钥（仅此一次）。
func (s *ClientService) Create(ctx context.Context, in CreateClientInput) (*model.Client, string, error) {
	now := nowMillis()
	c := &model.Client{
		ClientID:        id.New()[10:], // 16 位可读标识
		ClientName:      in.ClientName,
		IsPublic:        in.IsPublic,
		RedirectURIs:    in.RedirectURIs,
		PostLogoutURIs:  []string{},
		GrantTypes:      in.GrantTypes,
		Scopes:          in.Scopes,
		AccessTokenTTL:  900,
		RefreshTokenTTL: 2592000,
		TenantID:        "default",
		Status:          1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if len(c.GrantTypes) == 0 {
		c.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if in.Description != "" {
		c.Description = &in.Description
	}
	if in.LogoURL != "" {
		c.LogoURL = &in.LogoURL
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "profile", "email", "offline_access"}
	}
	secret := ""
	if !in.IsPublic {
		secret = crypto.RandomToken(32)
		c.ClientSecretHash = crypto.SHA256(secret)
	}
	if err := s.st.Transaction(ctx, func(ctx context.Context) error {
		if err := s.st.CreateClient(ctx, c); err != nil {
			return err
		}
		if in.FileID != "" {
			return s.bindLogo(ctx, c.ClientID, in.FileID)
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	return c, secret, nil
}

// bindLogo 把已上传的 logo 文件记录绑定到新建应用：回填 target_id 并写回 clients.logo_url。
func (s *ClientService) bindLogo(ctx context.Context, clientID, fileID string) error {
	f, err := s.st.GetFileByID(ctx, fileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFileNotFound
		}
		return err
	}
	if f.Scene != model.FileSceneAppLogo {
		return ErrFileValidation
	}
	if err := s.st.UpdateFileTargetID(ctx, fileID, clientID); err != nil {
		return err
	}
	if err := s.st.UpdateClientFields(ctx, clientID, map[string]any{"logo_url": f.URL}, nil, nowMillis()); err != nil {
		return err
	}
	return nil
}

// RotateSecret 轮换密钥，返回新明文。
func (s *ClientService) RotateSecret(ctx context.Context, clientID string) (string, error) {
	if _, err := s.st.GetClient(ctx, clientID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrClientNotFound
		}
		return "", err
	}
	secret := crypto.RandomToken(32)
	if err := s.st.UpdateClientSecret(ctx, clientID, crypto.SHA256(secret), nowMillis()); err != nil {
		return "", err
	}
	return secret, nil
}

// List 分页列出应用（含成员数）；hasMembers 为 nil 时不做成员数过滤（FR-032）。
func (s *ClientService) List(ctx context.Context, keyword string, hasMembers *bool, page, pageSize int) ([]store.ClientWithCount, int64, error) {
	return s.st.ListClientsWithCount(ctx, keyword, hasMembers, page, pageSize)
}

// UpdateClientInput 应用详情可编辑字段（指针=三态：缺失表示不变）。
type UpdateClientInput struct {
	ClientName      *string
	Description     *string
	LogoURL         *string
	RedirectURIs    *[]string
	PostLogoutURIs  *[]string
	AccessTokenTTL  *int
	RefreshTokenTTL *int
	Status          *int8
	// UpdatedAt 为读取时的版本（毫秒），非空时做乐观版本校验（FR-034）。
	UpdatedAt *int64
}

// Update 更新应用可编辑字段，返回更新后的 client。
func (s *ClientService) Update(ctx context.Context, clientID string, in UpdateClientInput) (*model.Client, error) {
	cur, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrClientNotFound
		}
		return nil, err
	}
	updates := map[string]any{}
	if in.ClientName != nil {
		v := strings.TrimSpace(*in.ClientName)
		if v == "" || len(v) > 128 {
			return nil, ErrAppValidation
		}
		updates["client_name"] = v
	}
	if in.Description != nil {
		if len(*in.Description) > 255 {
			return nil, ErrAppValidation
		}
		updates["description"] = *in.Description
	}
	if in.LogoURL != nil {
		if err := validateOptionalURL(*in.LogoURL, 512); err != nil {
			return nil, err
		}
		updates["logo_url"] = *in.LogoURL
	}
	if in.RedirectURIs != nil {
		uris, err := validateURIs(*in.RedirectURIs, 1, 10)
		if err != nil {
			return nil, err
		}
		updates["redirect_uris"] = model.StringList(uris)
	}
	if in.PostLogoutURIs != nil {
		uris, err := validateURIs(*in.PostLogoutURIs, 0, 10)
		if err != nil {
			return nil, err
		}
		updates["post_logout_uris"] = model.StringList(uris)
	}
	if in.AccessTokenTTL != nil {
		if *in.AccessTokenTTL < 60 || *in.AccessTokenTTL > 86400 {
			return nil, ErrAppValidation
		}
		updates["access_token_ttl"] = *in.AccessTokenTTL
	}
	if in.RefreshTokenTTL != nil {
		if *in.RefreshTokenTTL < 300 || *in.RefreshTokenTTL > 31536000 {
			return nil, ErrAppValidation
		}
		updates["refresh_token_ttl"] = *in.RefreshTokenTTL
	}
	if in.Status != nil {
		if *in.Status != 0 && *in.Status != 1 {
			return nil, ErrAppValidation
		}
		updates["status"] = *in.Status
	}
	if len(updates) == 0 {
		return cur, nil
	}
	if err := s.st.UpdateClientFields(ctx, clientID, updates, in.UpdatedAt, nowMillis()); err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, ErrClientNotFound
		case errors.Is(err, store.ErrStaleResource):
			return nil, ErrAppResourceStale
		}
		return nil, err
	}
	return s.st.GetClient(ctx, clientID)
}

// UpdateBasicInfo 门户应用管理员可改的基础信息：仅名称 / 描述 / Logo / 回调地址。
// 其余字段（登出地址、两类 TTL、启用状态）在此统一清空，即使调用方误传也不会落库，
// 保证「可编辑字段白名单」只有 service 一处定义（006 口径）。
func (s *ClientService) UpdateBasicInfo(ctx context.Context, clientID string, in UpdateClientInput) (*model.Client, error) {
	in.PostLogoutURIs = nil
	in.AccessTokenTTL = nil
	in.RefreshTokenTTL = nil
	in.Status = nil
	return s.Update(ctx, clientID, in)
}

func validateOptionalURL(v string, maxLen int) error {
	if v == "" {
		return nil
	}
	if len(v) > maxLen {
		return ErrAppValidation
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrAppValidation
	}
	return nil
}

func validateURIs(in []string, min, max int) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if err := validateOptionalURL(v, 512); err != nil {
			return nil, err
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) < min || len(out) > max {
		return nil, ErrAppValidation
	}
	return out, nil
}

// Get 查询单个应用。
func (s *ClientService) Get(ctx context.Context, clientID string) (*model.Client, error) {
	return s.st.GetClient(ctx, clientID)
}

// ListForUser 返回用户以成员身份可访问的启用应用（FR-012）。
// 门户可见性只由 client_members 与 clients.status 决定；不存在 org → 应用的授权模型。
// homeUrl 取首个 redirect_uri 的 origin。
func (s *ClientService) ListForUser(ctx context.Context, userID string) ([]AppItem, error) {
	clients, err := s.st.ListClientsForMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	last, err := s.st.LastAccessByClient(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]AppItem, 0, len(clients))
	for _, row := range clients {
		c := row.Client
		if c.Status != 1 {
			continue
		}
		item := AppItem{ClientID: c.ClientID, Name: c.ClientName, IsAppAdmin: row.IsAdmin}
		if c.Description != nil {
			item.Description = *c.Description
		}
		if c.LogoURL != nil {
			item.LogoURL = *c.LogoURL
		}
		if len(c.RedirectURIs) > 0 {
			if u, err := url.Parse(c.RedirectURIs[0]); err == nil && u.Scheme != "" {
				item.HomeURL = u.Scheme + "://" + u.Host
			}
		}
		if item.HomeURL == "" {
			item.HomeURL = strings.TrimSpace("")
		}
		if at, ok := last[c.ClientID]; ok {
			v := at
			item.LastAccessAt = &v
		}
		out = append(out, item)
	}
	return out, nil
}
