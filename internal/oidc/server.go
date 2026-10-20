package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"
	"go.uber.org/fx"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
)

// NewServer 构造协议层独立 http.Server（规范 §7 约束 2：与 Gin 物理分离）。
func NewServer(
	lc fx.Lifecycle,
	cfg *config.Config,
	provider *op.Provider,
	st *Storage,
	sst *store.Store,
	users *service.UserService,
	sessions *service.SessionService,
	clients *service.ClientService,
	audit *service.Auditor,
	logger *slog.Logger,
) *http.Server {
	login := newLoginHandler(cfg, st, users, sessions, clients, audit, op.AuthCallbackURL(provider), logger)

	mux := http.NewServeMux()
	mux.Handle("/login", login)
	mux.Handle("/logout", newLogoutHandler(cfg, sessions))
	mux.HandleFunc("/logged-out", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("signed out successfully"))
	})
	// op.Provider 自身实现 http.Handler，挂在根路径，不经过 Gin。
	mux.Handle("/", withTokenTx(pkceGuard(provider, sst), sst, logger))

	srv := &http.Server{
		Addr:         cfg.HTTPIdpAddr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("idp server listening", "addr", cfg.HTTPIdpAddr, "issuer", cfg.Issuer)
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("idp server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		},
	})
	return srv
}

// withTokenTx 让 /oauth2/token 的多次 Storage 调用共享同一事务（规范 §7 约束 3）。
// 响应先写入缓冲区，事务提交成功后再回写给客户端。
func withTokenTx(next http.Handler, st *store.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			next.ServeHTTP(w, r)
			return
		}
		rec := newBufferedResponse()
		holder := &tokenScope{}
		err := st.Transaction(r.Context(), func(ctx context.Context) error {
			ctx = context.WithValue(ctx, tokenScopeKey{}, holder)
			next.ServeHTTP(rec, r.WithContext(ctx))
			return nil
		})
		if err != nil {
			logger.Error("token endpoint transaction failed", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"server_error","error_description":"internal error"}`))
			return
		}
		// RFC 6749 §5.1：token 响应应包含 scope；协议库未输出该字段，此处补齐。
		rec.injectScope(holder.Scope)
		rec.flush(w)
	})
}

// tokenScope 在 Storage 与 HTTP 中间件之间传递本次授予的 scope。
type tokenScope struct{ Scope string }

type tokenScopeKey struct{}

func setGrantedScope(ctx context.Context, scopes []string) {
	if h, ok := ctx.Value(tokenScopeKey{}).(*tokenScope); ok && h != nil {
		h.Scope = strings.Join(scopes, " ")
	}
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header), status: http.StatusOK}
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *bufferedResponse) WriteHeader(status int)      { b.status = status }

func (b *bufferedResponse) injectScope(scope string) {
	if scope == "" || b.status != http.StatusOK {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(b.body.Bytes(), &m); err != nil {
		return
	}
	if _, ok := m["access_token"]; !ok {
		return
	}
	m["scope"] = scope
	out, err := json.Marshal(m)
	if err != nil {
		return
	}
	b.body.Reset()
	b.body.Write(out)
}

func (b *bufferedResponse) flush(w http.ResponseWriter) {
	delete(b.header, "Content-Length")
	for k, vs := range b.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}
