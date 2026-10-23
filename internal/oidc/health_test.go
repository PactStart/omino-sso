package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/pkg/clientip"
	"github.com/omino/omino-sso/internal/store"
)

// 本文件固定 007 协议层（:8080）的两件事（tasks.md T013 / T014）：
//  1. 探针与 :8081 完全同语义——同一 probe 包、同一响应形态，不得逻辑分叉；
//  2. 登录链路取客户端 IP 走 clientip.FromRequest（多跳 XFF 判定唯一实现）。
//
// 探针暴露面（公网只放行 /healthz、/readyz 必 404）由 nginx 保证，
// 已在 contracts/nginx-routing.md §4 与 scripts/smoke-deploy.sh 覆盖，这里不重复。

// newFailingStore 与 internal/api/health_test.go 同构：MySQL 必然连不上，
// 让 /readyz 走到 503 分支（gorm 关闭自动 ping，Ping 时才拨号）。
func newFailingStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := gorm.Open(
		mysql.New(mysql.Config{
			DSN:                       "test:test@tcp(127.0.0.1:1)/none?timeout=100ms",
			SkipInitializeWithVersion: true,
		}),
		&gorm.Config{DisableAutomaticPing: true, Logger: gormlogger.Discard},
	)
	if err != nil {
		t.Fatalf("构造失效 gorm 句柄失败: %v", err)
	}
	return store.New(db, nil)
}

func serve(t *testing.T, h http.HandlerFunc, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestHealthzHandler(t *testing.T) {
	h := newHealthzHandler()

	w := serve(t, h, http.MethodGet, "/healthz")
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("body = %v, want {\"status\":\"ok\"}（两个 server 的探针不得各自演化）", body)
	}
	for _, forbidden := range []string{"code", "message", "data", "traceId"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("探针响应出现统一响应字段 %q", forbidden)
		}
	}

	if w := serve(t, h, http.MethodPost, "/healthz"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz = %d, want 405", w.Code)
	}
}

func TestReadyzHandler(t *testing.T) {
	h := newReadyzHandler(newFailingStore(t))

	w := serve(t, h, http.MethodGet, "/readyz")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503（body=%s）", w.Code, w.Body.String())
	}
	var body struct {
		Status  string   `json:"status"`
		Failing []string `json:"failing"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, w.Body.String())
	}
	if body.Status != "unready" || len(body.Failing) != 1 || body.Failing[0] != "mysql" {
		t.Fatalf("status/failing = %q/%v, want unready/[mysql]", body.Status, body.Failing)
	}

	if w := serve(t, h, http.MethodPost, "/readyz"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /readyz = %d, want 405", w.Code)
	}
}

// 协议层登录（login.go）用同一份判定取 IP：多跳链路下取最右非可信地址。
func TestLoginClientIPExtraction(t *testing.T) {
	cfg := &config.Config{TrustedProxies: []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.18.0.0/16"),
	}}

	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	r.RemoteAddr = "172.18.0.6:40000" // 前端 nginx（可信）
	r.Header.Set("X-Forwarded-For", "198.51.100.20, 10.0.0.7")

	if got := clientip.FromRequest(r, cfg.TrustedProxies); got != "198.51.100.20" {
		t.Fatalf("客户端 IP = %q, want 198.51.100.20（审计与限流都依赖这个值）", got)
	}

	// 反证：直连方不在可信网段时，伪造的 XFF 必须被忽略。
	r2 := httptest.NewRequest(http.MethodGet, "/login", nil)
	r2.RemoteAddr = "203.0.113.9:40000"
	r2.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientip.FromRequest(r2, cfg.TrustedProxies); got != "203.0.113.9" {
		t.Fatalf("客户端 IP = %q, want 203.0.113.9（不可信来源的 XFF 不得采信）", got)
	}
}
