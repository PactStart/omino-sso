package oidc

import (
	"net/http"

	"github.com/omino/omino-sso/internal/pkg/probe"
	"github.com/omino/omino-sso/internal/store"
)

// newHealthzHandler 协议层存活探针（与 :8081 同一实现，见 internal/pkg/probe）。
func newHealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !probe.IsGet(r) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		probe.WriteHealth(w)
	}
}

// newReadyzHandler 协议层就绪探针；公网不暴露（nginx 只放行 /healthz）。
func newReadyzHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !probe.IsGet(r) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mysqlErr, redisErr := st.PingDetail(r.Context())
		probe.WriteReady(w, mysqlErr, redisErr)
	}
}
