package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// session 是服务端会话：令牌只在这里出现，绝不写进 cookie 或页面。
type session struct {
	Sub       string
	Username  string
	Name      string
	Email     string
	Tenant    string
	DeptID    string
	Roles     []string
	AMR       []string

	AccessToken     string
	RefreshToken    string
	IDToken         string
	AccessExpiresAt time.Time
	LastRefreshedAt time.Time
}

// pending 只在「授权中」阶段存在，与登录态分开存放，避免互相覆盖。
type pending struct {
	State     string
	Nonce     string
	Verifier  string
	CreatedAt time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	pendings map[string]*pending
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		sessions: make(map[string]*session),
		pendings: make(map[string]*pending),
	}
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (s *sessionStore) putPending(id string, p *pending) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 只保留最新的一个 pending：重复点「登录」应覆盖而不是堆叠。
	for k := range s.pendings {
		delete(s.pendings, k)
	}
	s.pendings[id] = p
}

func (s *sessionStore) takePending(id string) (*pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pendings[id]
	if ok {
		delete(s.pendings, id)
	}
	return p, ok
}

func (s *sessionStore) put(id string, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

func (s *sessionStore) get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// 本地 http 演示关闭 Secure；生产必须开启（见 README「仅演示」）。
		Secure: false,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func sessionID(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
