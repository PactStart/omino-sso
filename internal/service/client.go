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
	if err := s.st.CreateClient(ctx, c); err != nil {
		return nil, "", err
	}
	return c, secret, nil
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

// List 分页列出应用。
func (s *ClientService) List(ctx context.Context, keyword string, page, pageSize int) ([]model.Client, int64, error) {
	return s.st.ListClients(ctx, keyword, page, pageSize)
}

// Get 查询单个应用。
func (s *ClientService) Get(ctx context.Context, clientID string) (*model.Client, error) {
	return s.st.GetClient(ctx, clientID)
}

// ListForUser 返回用户可访问的应用。
// clients 表没有描述/图标/主页字段（规范 §6.3 未定义），
// 因此 description/logoUrl 返回空串，homeUrl 取首个 redirect_uri 的 origin。
func (s *ClientService) ListForUser(ctx context.Context, userID string) ([]AppItem, error) {
	clients, _, err := s.st.ListClients(ctx, "", 1, 100)
	if err != nil {
		return nil, err
	}
	last, err := s.st.LastAccessByClient(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]AppItem, 0, len(clients))
	for _, c := range clients {
		if c.Status != 1 {
			continue
		}
		item := AppItem{ClientID: c.ClientID, Name: c.ClientName}
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
