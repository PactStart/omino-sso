package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/api/docs"
	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/service/notification"
	"github.com/omino/omino-sso/internal/store"
)

// newTestRouter 装配一个只依赖内存对象的路由。
// Redis 客户端指向必然连不上的地址：限流器本身 fail-open，「文档页不依赖 Redis」
// 因此由真实请求路径验证（CT-7），无需真的停掉 Redis。
func newTestRouter(t *testing.T, env string) *gin.Engine {
	t.Helper()
	return newTestRouterWith(t, env, nil, nil)
}

// newTestRouterWith 在默认装配上允许注入自定义 store 与配置覆盖。
// 探针用例需要「可控的依赖失败」（/readyz 的 failing 明细）与「极小的限流配额」
// （验证探针不被限流），其余装配保持唯一实现，避免测试各自拼出第二套路由。
func newTestRouterWith(t *testing.T, env string, st *store.Store, mutate func(*config.Config)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		AppEnv:               env,
		RateLimitAPIPerMin:   60,
		RateLimitLoginPerMin: 60,
		SessionTTL:           time.Hour,
		SessionTTLRemember:   7 * 24 * time.Hour,
	}
	if mutate != nil {
		mutate(cfg)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  0,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if st == nil {
		st = store.New(nil, rdb)
	}
	audit := service.NewAuditor(st, 1, logger)
	users := service.NewUserService(st, cfg, audit, logger)
	sessions := service.NewSessionService(st, cfg)
	clients := service.NewClientService(st)
	orgs := service.NewOrgService(st, logger)
	appAuth := service.NewAppAuthService(st)
	fileSvc := service.NewFileService(st, store.NewNoopFileStore(), audit)
	notif, err := notification.New(cfg, st, audit, logger)
	if err != nil {
		t.Fatalf("构造验证码服务失败: %v", err)
	}
	mfaSvc := service.NewMFAService(st, cfg, notif, audit, logger)
	captchaSvc := service.NewCaptchaService(st, logger)
	return NewRouter(cfg, st, users, sessions, clients, orgs, appAuth, fileSvc, notif, captchaSvc, mfaSvc, audit, logger)
}

// TestOrgRoutesRegistered 固化组织架构的 4 条路由，并验证三条写路由在无
// 会话时于触达业务前返回统一响应格式的 401（契约 C-1）。
func TestOrgRoutesRegistered(t *testing.T) {
	r := newTestRouter(t, "test")

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/admin/orgs/tree",
		"POST /api/v1/admin/orgs",
		"PATCH /api/v1/admin/orgs/:id",
		"DELETE /api/v1/admin/orgs/:id",
	} {
		if !registered[want] {
			t.Errorf("缺少路由 %s", want)
		}
	}

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/admin/orgs", `{"name":"技术中心"}`},
		{http.MethodPatch, "/api/v1/admin/orgs/01J9ZK9M1N3P5Q7R9S1T3V5W7X", `{"name":"x"}`},
		{http.MethodDelete, "/api/v1/admin/orgs/01J9ZK9M1N3P5Q7R9S1T3V5W7X", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, 期望 401", tc.method, tc.path, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s 响应体不是统一响应: %v", tc.method, tc.path, err)
		}
		for _, key := range []string{"code", "message", "data", "traceId"} {
			if _, ok := body[key]; !ok {
				t.Errorf("%s %s 响应体缺少 %q", tc.method, tc.path, key)
			}
		}
		if body["code"] != "UNAUTHENTICATED" {
			t.Errorf("%s %s code = %v, 期望 UNAUTHENTICATED", tc.method, tc.path, body["code"])
		}
	}
}

func get(r http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestDocsServedWithoutRedis 覆盖 CT-1 / CT-2 / CT-6 / CT-7：
// 非 prod 下文档页与描述可用、静态资源来自内嵌文件系统、且不依赖 Redis 与 cookie。
func TestDocsServedWithoutRedis(t *testing.T) {
	r := newTestRouter(t, "test")

	for _, path := range []string{"/docs/", "/docs/index.html"} {
		rec := get(r, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, 期望 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("GET %s Content-Type = %q, 期望 text/html", path, ct)
		}
		if body := rec.Body.String(); !strings.Contains(strings.ToLower(body), "swagger") {
			t.Fatalf("GET %s 响应体不含 swagger", path)
		}
		if ck := rec.Header().Get("Set-Cookie"); ck != "" {
			t.Fatalf("GET %s 不得下发 cookie，实际 %q（CT-6）", path, ck)
		}
	}

	rec := get(r, "/docs/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/openapi.json = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("openapi.json Content-Type = %q", ct)
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Description string `json:"description"`
		} `json:"info"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json 不是合法 JSON: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("openapi = %q, 期望 3.0.3", doc.OpenAPI)
	}
	ops := 0
	for _, item := range doc.Paths {
		ops += len(item)
	}
	// 56（005 及之前）+ 18（006：admin 1 + 门户 16 + 门户只读组织树 1）
	// + 4（门户应用信息：详情 / 更新 / Logo 设置 / Logo 清除）。
	if ops != 78 {
		t.Fatalf("(method,path) 组合数为 %d, 期望 78", ops)
	}
	if !strings.Contains(doc.Info.Description, "环境：test") {
		t.Fatalf("info.description 未注入环境: %q", doc.Info.Description)
	}

	cssRec := get(r, "/docs/swagger-ui.css")
	if cssRec.Code != http.StatusOK {
		t.Fatalf("内嵌静态资源 GET /docs/swagger-ui.css = %d, 期望 200", cssRec.Code)
	}
}

// TestDocsNotRegisteredInProd 覆盖 CT-4 / CT-5：prod 下文档路由不存在，
// 且响应体与任意不存在路径完全一致（仅 traceId 允许不同）。
func TestDocsNotRegisteredInProd(t *testing.T) {
	r := newTestRouter(t, "prod")

	docsRec := get(r, "/docs/index.html")
	absentRec := get(r, "/docs-absent")
	if docsRec.Code != http.StatusNotFound {
		t.Fatalf("prod GET /docs/index.html = %d, 期望 404", docsRec.Code)
	}
	if absentRec.Code != http.StatusNotFound {
		t.Fatalf("prod GET /docs-absent = %d, 期望 404", absentRec.Code)
	}
	if got, want := normalizeBody(t, docsRec.Body.Bytes()), normalizeBody(t, absentRec.Body.Bytes()); got != want {
		t.Fatalf("prod 下 /docs/index.html 与不存在路径响应体不一致（CT-4）:\n got=%s\nwant=%s", got, want)
	}

	specRec := get(r, "/docs/openapi.json")
	if specRec.Code != http.StatusNotFound {
		t.Fatalf("prod GET /docs/openapi.json = %d, 期望 404", specRec.Code)
	}
	if strings.Contains(specRec.Body.String(), "openapi") {
		t.Fatalf("prod 下文档描述被泄露（CT-5）: %s", specRec.Body.String())
	}
}

// TestDocsPathsMatchRouterRoutes 覆盖 AC-1：文档的 path 集合必须与路由注册完全一致。
func TestDocsPathsMatchRouterRoutes(t *testing.T) {
	r := newTestRouter(t, "test")

	// 文档的 path 必须带 /api/v1 前缀：servers 固定为 "/"，basePath 已在转换时折进 path，
	// 否则 swagger-ui 的 Try it out 会打到 /auth/login 之类的裸路径上（全 404）。
	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		if !strings.HasPrefix(ri.Path, "/api/v1") {
			continue
		}
		registered[toOpenAPIPath(ri.Path)] = true
	}

	documented := map[string]bool{}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(docs.Spec("test"), &doc); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	for p, item := range doc.Paths {
		for range item {
			documented[toOpenAPIPath(p)] = true
		}
	}

	if got, want := sortedKeys(documented), sortedKeys(registered); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("文档 path 集合与路由不一致（AC-1）:\n 文档=%v\n 路由=%v", got, want)
	}
}

// TestAppAdminRoutesRegistered 固化 006 的路由面：
// 22 条新路由必须注册（含应用信息详情/更新/Logo）；门户不得注册「设为管理员」与密钥；
// 未带会话时门户端点统一返回 401（鉴权先于业务）。
func TestAppAdminRoutesRegistered(t *testing.T) {
	r := newTestRouter(t, "test")

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{
		"PUT /api/v1/admin/clients/:id/members/:uid/admin",
		"GET /api/v1/me/orgs/tree",
		"GET /api/v1/me/apps/:id",
		"GET /api/v1/me/apps/:id/detail",
		"PATCH /api/v1/me/apps/:id",
		"POST /api/v1/me/apps/:id/logo",
		"DELETE /api/v1/me/apps/:id/logo",
		"GET /api/v1/me/apps/:id/members",
		"GET /api/v1/me/apps/:id/members/candidates",
		"POST /api/v1/me/apps/:id/members",
		"POST /api/v1/me/apps/:id/members/remove",
		"GET /api/v1/me/apps/:id/roles",
		"POST /api/v1/me/apps/:id/roles",
		"GET /api/v1/me/apps/:id/roles/:rid",
		"PATCH /api/v1/me/apps/:id/roles/:rid",
		"DELETE /api/v1/me/apps/:id/roles/:rid",
		"PUT /api/v1/me/apps/:id/roles/:rid/permissions",
		"PUT /api/v1/me/apps/:id/roles/:rid/members",
		"GET /api/v1/me/apps/:id/permissions",
		"POST /api/v1/me/apps/:id/permissions",
		"PATCH /api/v1/me/apps/:id/permissions/:pid",
		"DELETE /api/v1/me/apps/:id/permissions/:pid",
	} {
		if !registered[want] {
			t.Errorf("缺少路由 %s", want)
		}
	}

	// 门户不得暴露授予/取消管理员与密钥（FR-007）；应用配置只开放白名单字段，
	// 其余只读（口径调整后）——密钥与启停/TTL 仍只在 admin 端。
	for _, forbidden := range []string{
		"PUT /api/v1/me/apps/:id/members/:uid/admin",
		"POST /api/v1/me/apps/:id/secret",
		"PATCH /api/v1/me/apps/:id/secret",
	} {
		if registered[forbidden] {
			t.Errorf("门户不得注册 %s", forbidden)
		}
	}

	// 未带会话 → 401，且响应体为统一格式。
	for _, path := range []string{"/api/v1/me/apps/demo-app", "/api/v1/me/apps/demo-app/members", "/api/v1/me/orgs/tree"} {
		rec := get(r, path)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s = %d, 期望 401", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"UNAUTHENTICATED"`) {
			t.Fatalf("GET %s 响应体不是统一响应: %s", path, rec.Body.String())
		}
	}
}

func toOpenAPIPath(p string) string {
	// gin 的 :id / :sid 与 swag 的 {id} / {sid} 是同一路径的两种写法。
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ":") {
			p = strings.ReplaceAll(p, seg, "{"+strings.TrimPrefix(seg, ":")+"}")
		}
	}
	return p
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalizeBody 去掉每次新生成的 traceId，便于比较响应体其余部分。
func normalizeBody(t *testing.T, raw []byte) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("响应体不是合法 JSON: %v (%s)", err, raw)
	}
	delete(body, "traceId")
	out, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化响应体失败: %v", err)
	}
	return string(out)
}
