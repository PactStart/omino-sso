package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store"
)

// 本文件固定 007 探针的三条隐性契约（tasks.md T012 / contracts/health-probes.md）：
//  ① 探针不参与限流：把 API 配额压到 1 后连打 /healthz 10 次必须全 200；
//  ② 响应体不套统一响应：不得出现 code / message / data / traceId；
//  ③ /readyz 依赖失败时 503，且 failing 只含 mysql / redis（不回传错误原文）。

// newFailingStore 返回一个 MySQL 必然连不上的 Store：
// gorm 关闭自动 ping（否则构造即失败），Ping 时才真正拨号 → 立刻连接拒绝。
// MySQL 失败时 PingDetail 不再探测 Redis（依赖链上游已断），因此 redis 客户端可为 nil。
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

// 契约①：配额压到 1 时探针仍全通。
// 少了 middleware.RateLimit 的短路，容器 healthcheck 会 429 → 判 unhealthy → 重启风暴。
func TestProbeBypassesRateLimit(t *testing.T) {
	r := newTestRouterWith(t, "test", nil, func(c *config.Config) {
		c.RateLimitAPIPerMin = 1
		c.RateLimitLoginPerMin = 1
	})

	const rounds = 10
	for i := 0; i < rounds; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次 /healthz = %d（期望 200；429 说明探针被业务配额化了）：%s",
				i+1, w.Code, w.Body.String())
		}
	}
}

// 契约②：探针响应不套 {code,message,data,traceId}，且带 no-store。
func TestProbeNotWrappedInUnifiedResponse(t *testing.T) {
	r := newTestRouter(t, "test")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store（防中间层缓存 200 掩盖故障）", got)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("探针响应不是合法 JSON: %v（body=%s）", err, w.Body.String())
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("body = %v, want {\"status\":\"ok\"}（多出的键说明套了统一响应或泄露实现细节）", body)
	}
	for _, forbidden := range []string{"code", "message", "data", "traceId"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("探针响应出现统一响应字段 %q（body=%v）", forbidden, body)
		}
	}
}

// 契约③：依赖失败 → 503，failing 只含 mysql / redis，且不带错误原文。
func TestReadyzReportsFailingDependency(t *testing.T) {
	r := newTestRouterWith(t, "test", newFailingStore(t), nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503（body=%s）", w.Code, w.Body.String())
	}
	var body struct {
		Status  string   `json:"status"`
		Failing []string `json:"failing"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("/readyz 响应不是合法 JSON: %v（body=%s）", err, w.Body.String())
	}
	if body.Status != "unready" {
		t.Fatalf("status = %q, want unready", body.Status)
	}
	if len(body.Failing) != 1 || body.Failing[0] != "mysql" {
		t.Fatalf("failing = %v, want [mysql]（MySQL 不通时不应把 redis 一起标红）", body.Failing)
	}
	// 不回传 DSN / 内网地址（宪法 IV）：错误原文不得出现在响应里。
	for _, leak := range []string{"127.0.0.1", "tcp", "connection refused"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Fatalf("响应泄露内部细节 %q（body=%s）", leak, w.Body.String())
		}
	}
}

// 探针只接受 GET：容器编排常误配成 HEAD/POST，必须显式 405 而非静默 200。
func TestProbeRejectsNonGET(t *testing.T) {
	r := newTestRouter(t, "test")

	for _, method := range []string{http.MethodPost, http.MethodHead} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/healthz", nil))
		if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
			t.Fatalf("%s /healthz = %d, want 405（或 404：Gin 未注册该方法）", method, w.Code)
		}
	}
}
