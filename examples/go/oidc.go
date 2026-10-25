package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"


	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	errNoSession = errors.New("no session")
	errRefresh   = errors.New("refresh failed")
)

type app struct {
	cfg         config
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauth       oauth2.Config
	userinfoURL string
	sessions    *sessionStore
	httpClient  *http.Client
	tmpl        *template.Template

	// refreshMu 保证同一时刻只有一次刷新在进行：IdP 的 refresh 是轮换 + 重放即吊销整族，
	// 并发刷新会把用户踢下线（见 contracts 与 docs 的刷新章节）。
	refreshMu sync.Mutex
}

func newApp(ctx context.Context, cfg config) (*app, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	tmpl, err := template.New("index.html").ParseFS(templatesFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	userinfoURL := provider.UserInfoEndpoint()
	if cfg.UserInfoOverride != "" {
		// 仅调试：用于验证 userinfo 不可用时的降级（不得成为关闭校验的开关）。
		userinfoURL = cfg.UserInfoOverride
	}

	return &app{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURI,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "offline_access"},
		},
		userinfoURL: userinfoURL,
		sessions:    newSessionStore(),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		tmpl:        tmpl,
	}, nil
}

// newAuthParams 生成 state / nonce / PKCE verifier 与 challenge（只允许 S256）。
func newAuthParams() (state, nonce, verifier, challenge string, err error) {
	state, err = randomToken(24)
	if err != nil {
		return "", "", "", "", err
	}
	nonce, err = randomToken(24)
	if err != nil {
		return "", "", "", "", err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return state, nonce, verifier, challenge, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (a *app) authCodeURL(state, nonce, challenge string) string {
	return a.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (a *app) exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	return a.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
}

// verifyIDToken 验签并校验 iss/aud/exp/nonce；返回原始声明（身份字段都在这里）。
func (a *app) verifyIDToken(ctx context.Context, rawToken, expectNonce string) (map[string]any, error) {
	idToken, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("id_token verify: %w", err)
	}
	claims := map[string]any{}
	if err = idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("id_token claims: %w", err)
	}
	if expectNonce != "" {
		if got, _ := claims["nonce"].(string); got != expectNonce {
			return nil, errors.New("id_token nonce 不匹配")
		}
	}
	return claims, nil
}

// ensureFresh 在访问令牌即将过期时做一次单飞刷新，用轮换后的新 refresh_token 覆盖旧值。
func (a *app) ensureFresh(ctx context.Context, sid string) (*session, error) {
	sess, ok := a.sessions.get(sid)
	if !ok {
		return nil, errNoSession
	}
	if time.Until(sess.AccessExpiresAt) > 30*time.Second {
		return sess, nil
	}

	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()

	// 锁内复查：并发请求里第一个已经换过，其余直接复用，不再使用旧 refresh_token。
	cur, ok := a.sessions.get(sid)
	if !ok {
		return nil, errNoSession
	}
	if time.Until(cur.AccessExpiresAt) > 30*time.Second {
		return cur, nil
	}

	start := time.Now()
	src := a.oauth.TokenSource(ctx, &oauth2.Token{
		RefreshToken: cur.RefreshToken,
		Expiry:       time.Now().Add(-time.Minute),
	})
	tok, err := src.Token()
	if err != nil {
		log.Printf("[refresh] fail reason=%v → reauth", err)
		return nil, errRefresh
	}

	next := *cur
	next.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		next.RefreshToken = tok.RefreshToken
	}
	next.AccessExpiresAt = tok.Expiry
	next.LastRefreshedAt = time.Now()
	a.sessions.put(sid, &next)

	rotated := 0
	if tok.RefreshToken != "" && tok.RefreshToken != cur.RefreshToken {
		rotated = 1
	}
	log.Printf("[refresh] ok rotated=%d elapsed=%s", rotated, time.Since(start).Round(time.Millisecond))
	return &next, nil
}

type userinfoResponse struct {
	Sub         string   `json:"sub"`
	AppRoles    []string `json:"app_roles"`
	Permissions []string `json:"permissions"`
}

// fetchUserInfo 取仅在 userinfo 下发的应用内角色与权限；失败由调用方降级，不放大成 5xx。
func (a *app) fetchUserInfo(ctx context.Context, accessToken string) (userinfoResponse, error) {
	var out userinfoResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.userinfoURL, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, err
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("userinfo status=%d", resp.StatusCode)
	}
	if err = json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("userinfo decode: %w", err)
	}
	return out, nil
}

// stringClaim 安全读取字符串声明（IdP 的声明名已冻结：preferred_username/name/email/tenant/dept_id）。
func stringClaim(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

func stringsClaim(claims map[string]any, key string) []string {
	raw, ok := claims[key].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}


