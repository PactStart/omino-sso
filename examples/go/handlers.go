package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

//go:embed templates/index.html
var templatesFS embed.FS

type meResponse struct {
	Sub             string   `json:"sub"`
	Username        string   `json:"username"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Tenant          string   `json:"tenant"`
	DeptID          string   `json:"deptId"`
	Roles           []string `json:"roles"`
	AMR             []string `json:"amr"`
	AppRoles        []string `json:"appRoles"`
	Permissions     []string `json:"permissions"`
	Degraded        []string `json:"degraded"`
	AccessExpiresIn int      `json:"accessExpiresIn"`
	LastRefreshedAt *string  `json:"lastRefreshedAt"`
}

type pageData struct {
	LoggedIn  bool
	Me        meResponse
	Issuer    string
	HasRefreshed bool
}

type apiError struct {
	Error  string `json:"error"`
	Stage  string `json:"stage"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, stage, code, detail string) {
	writeJSON(w, status, apiError{Error: code, Stage: stage, Detail: detail})
}

// handleLogin 生成 PKCE 参数、记下 pending，并跳到 IdP 授权端点。
func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	sid := sessionID(r)
	if sid == "" {
		sid = randomID()
		setSessionCookie(w, sid)
	}

	state, nonce, verifier, challenge, err := newAuthParams()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login", "internal_error", "生成 PKCE 参数失败")
		return
	}
	a.sessions.putPending(sid, &pending{
		State:     state,
		Nonce:     nonce,
		Verifier:  verifier,
		CreatedAt: time.Now(),
	})

	http.Redirect(w, r, a.authCodeURL(state, nonce, challenge), http.StatusFound)
}

// handleCallback 校验 state → 换令牌 → 验签 → 建服务端会话。
func (a *app) handleCallback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sid := sessionID(r)

	p, ok := a.sessions.takePending(sid)
	if !ok {
		writeError(w, http.StatusBadRequest, "callback", "no_pending_authorization", "没有进行中的授权请求")
		return
	}

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		writeError(w, http.StatusBadRequest, "authorize", e, q.Get("error_description"))
		return
	}
	if q.Get("state") != p.State {
		writeError(w, http.StatusBadRequest, "callback", "state_mismatch", "state 与本地记录不一致")
		return
	}
	code := q.Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "callback", "missing_code", "回调缺少 code")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tok, err := a.exchange(ctx, code, p.Verifier)
	if err != nil {
		writeError(w, http.StatusBadGateway, "token_exchange", "invalid_grant", err.Error())
		return
	}
	rawIDToken, _ := tok.Extra("id_token").(string)
	if rawIDToken == "" {
		writeError(w, http.StatusBadGateway, "token_exchange", "missing_id_token", "令牌响应没有 id_token")
		return
	}
	claims, err := a.verifyIDToken(ctx, rawIDToken, p.Nonce)
	if err != nil {
		writeError(w, http.StatusBadGateway, "id_token", "invalid_id_token", err.Error())
		return
	}

	sess := &session{
		Sub:             stringClaim(claims, "sub"),
		Username:        stringClaim(claims, "preferred_username"),
		Name:            stringClaim(claims, "name"),
		Email:           stringClaim(claims, "email"),
		Tenant:          stringClaim(claims, "tenant"),
		DeptID:          stringClaim(claims, "dept_id"),
		Roles:           stringsClaim(claims, "roles"),
		AMR:             stringsClaim(claims, "amr"),
		AccessToken:     tok.AccessToken,
		RefreshToken:    tok.RefreshToken,
		IDToken:         rawIDToken,
		AccessExpiresAt: tok.Expiry,
	}
	a.sessions.put(sid, sess)

	log.Printf("[login] sub=%s amr=%v elapsed=%s", sess.Sub, sess.AMR, time.Since(start).Round(time.Millisecond))
	http.Redirect(w, r, "/", http.StatusFound)
}

// current 取会话并在必要时刷新；返回 (nil, false) 表示未登录/已失效。
func (a *app) current(r *http.Request, w http.ResponseWriter) (*session, bool) {
	sid := sessionID(r)
	if sid == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	sess, err := a.ensureFresh(ctx, sid)
	if err != nil {
		if errors.Is(err, errRefresh) {
			// 刷新被拒：清掉失效会话，让用户重新授权，而不是反复用旧令牌。
			a.sessions.delete(sid)
			clearSessionCookie(w)
		}
		return nil, false
	}
	return sess, true
}

func buildView(sess *session, ui userinfoResponse, degraded []string) meResponse {
	refreshed := (*string)(nil)
	if !sess.LastRefreshedAt.IsZero() {
		s := sess.LastRefreshedAt.UTC().Format(time.RFC3339)
		refreshed = &s
	}
	return meResponse{
		Sub:             sess.Sub,
		Username:        sess.Username,
		Name:            sess.Name,
		Email:           sess.Email,
		Tenant:          sess.Tenant,
		DeptID:          sess.DeptID,
		Roles:           nonNil(sess.Roles),
		AMR:             nonNil(sess.AMR),
		AppRoles:        nonNil(ui.AppRoles),
		Permissions:     nonNil(ui.Permissions),
		Degraded:        degraded,
		AccessExpiresIn: int(time.Until(sess.AccessExpiresAt).Seconds()),
		LastRefreshedAt: refreshed,
	}
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// handleMe 返回当前用户；userinfo 不可用时降级为 200 + degraded（不放大成 5xx）。
func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.current(r, w)
	if !ok {
		writeError(w, http.StatusUnauthorized, "session", "unauthenticated", "未登录或会话已失效")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	degraded := []string{}
	ui, err := a.fetchUserInfo(ctx, sess.AccessToken)
	if err != nil {
		degraded = []string{"userinfo"}
	}
	writeJSON(w, http.StatusOK, buildView(sess, ui, degraded))
}

// handleLogout 清本地会话后交给 IdP 的 /logout（end_session 不清 sid）。
func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sid := sessionID(r); sid != "" {
		a.sessions.delete(sid)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, a.cfg.Issuer+"/logout?next=/logged-out", http.StatusFound)
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := pageData{Issuer: a.cfg.Issuer}

	if sess, ok := a.current(r, w); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		degraded := []string{}
		ui, err := a.fetchUserInfo(ctx, sess.AccessToken)
		if err != nil {
			degraded = []string{"userinfo"}
		}
		data.LoggedIn = true
		data.Me = buildView(sess, ui, degraded)
		data.HasRefreshed = data.Me.LastRefreshedAt != nil
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.Execute(w, data); err != nil {
		log.Printf("[error] render index: %v", err)
	}
}
