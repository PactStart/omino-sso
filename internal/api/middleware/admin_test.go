package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/store/model"
)

// 平台管理员闸门（Admin）的行为固化：010 的组织授权在 admin 端只允许平台管理员操作（FR-021）。
// 这里直接注入 Auth 的结果，避免为「角色判定」这类纯中间件逻辑引入数据库依赖。

func newAdminRouter(user *model.User) *gin.Engine {
	gin.SetMode(gin.TestMode)
	_ = slog.New(slog.NewTextHandler(io.Discard, nil))
	r := gin.New()
	g := r.Group("/api/v1/admin")
	g.Use(func(c *gin.Context) {
		if user != nil {
			c.Set(CtxUser, user)
		}
		c.Next()
	})
	g.Use(Admin())
	g.GET("/clients/:id/org-grants", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func TestAdminAllowsPlatformAdmin(t *testing.T) {
	r := newAdminRouter(&model.User{
		ID:       "01J9ZK9M1N3P5Q7R9S1T3V5W7X",
		Username: "admin",
		Roles:    []string{"user", "admin"},
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/clients/demo-app/org-grants", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("平台管理员访问 = %d, 期望 200", rec.Code)
	}
}

// 非平台管理员访问 admin 端的组织授权必须 403，且错误码为 FORBIDDEN（不是 401 / 404）。
func TestAdminDeniesNonAdmin(t *testing.T) {
	r := newAdminRouter(&model.User{
		ID:       "01J9ZK9M1N3P5Q7R9S1T3V5W7X",
		Username: "zhangsan",
		Roles:    []string{"user"},
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/clients/demo-app/org-grants", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("非平台管理员访问 = %d, 期望 403", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "FORBIDDEN")
}

func TestAdminRequiresSession(t *testing.T) {
	r := newAdminRouter(nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/clients/demo-app/org-grants", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无会话访问 = %d, 期望 401", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "UNAUTHENTICATED")
}

func assertErrorCode(t *testing.T, raw []byte, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
		Data any    `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("响应体不是统一响应: %v (%s)", err, raw)
	}
	if body.Code != want {
		t.Fatalf("code = %q, 期望 %q", body.Code, want)
	}
	if body.Data != nil {
		t.Fatalf("失败响应 data 必须为 null，实际 %v", body.Data)
	}
}
