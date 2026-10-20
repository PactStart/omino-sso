package oidc

import (
	"net/http"

	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/omino/omino-sso/internal/pkg/crypto"
	"github.com/omino/omino-sso/internal/store"
)

// pkceGuard 在进入协议库之前强制 PKCE 规则（规范 §9 第 2 条）。
// 协议库仅对 AuthMethodNone 的公共客户端校验 code_verifier，
// 机密客户端需要在此补齐，否则"带 challenge 但不带 verifier"会签发令牌。
func pkceGuard(next http.Handler, st *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/oauth2/authorize":
			if err := checkAuthorizePKCE(r, st); err != "" {
				writeOAuthError(w, http.StatusBadRequest, "invalid_request", err)
				return
			}
		case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
			if err := checkTokenPKCE(r, st); err != "" {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func checkAuthorizePKCE(r *http.Request, st *store.Store) string {
	q := r.URL.Query()
	challenge := q.Get("code_challenge")
	method := q.Get("code_challenge_method")

	if challenge == "" {
		if clientID := q.Get("client_id"); clientID != "" {
			if c, err := st.GetClient(r.Context(), clientID); err == nil && c.IsPublic {
				return "code_challenge is required for public clients"
			}
		}
		return ""
	}
	if method != string(oidc.CodeChallengeMethodS256) {
		return "code_challenge_method must be S256"
	}
	if !validCodeChallenge(challenge) {
		return "code_challenge is malformed"
	}
	return ""
}

func checkTokenPKCE(r *http.Request, st *store.Store) string {
	if err := r.ParseForm(); err != nil {
		return "malformed form body"
	}
	if r.PostFormValue("grant_type") != "authorization_code" {
		return ""
	}
	code := r.PostFormValue("code")
	verifier := r.PostFormValue("code_verifier")
	if code == "" {
		return ""
	}
	if verifier == "" {
		return "code_verifier is required"
	}
	row, err := st.FindAuthCode(r.Context(), crypto.SHA256(code))
	if err != nil {
		// 交由协议库返回统一的 invalid_grant。
		return ""
	}
	ch := &oidc.CodeChallenge{
		Challenge: row.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(row.CodeChallengeMethod),
	}
	if !oidc.VerifyCodeChallenge(ch, verifier) {
		return "invalid code_verifier"
	}
	return ""
}

func validCodeChallenge(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '.', c == '_', c == '~':
		default:
			return false
		}
	}
	return true
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `","error_description":"` + desc + `"}`))
}
