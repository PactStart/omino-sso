package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/store/model"
)

// fakeAppAdmin 用固定错误模拟 AuthorizeAppAdmin 的各种拒绝原因。
type fakeAppAdmin struct {
	err      error
	clientID string
	userID   string
}

func (f *fakeAppAdmin) AuthorizeAppAdmin(_ context.Context, clientID, userID string) error {
	f.clientID, f.userID = clientID, userID
	return f.err
}

func newAppAdminRouter(checker AppAdminChecker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := gin.New()
	g := r.Group("/me/apps/:id")
	// 直接注入用户，模拟 Auth 中间件的结果。
	g.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Anonymous") != "1" {
			c.Set(CtxUser, &model.User{ID: "01J9ZK9M1N3P5Q7R9S1T3V5W7X", Username: "zhangsan"})
		}
		c.Next()
	})
	g.Use(AppAdmin(checker, logger))
	g.GET("", func(c *gin.Context) {
		v, _ := c.Get(CtxClientID)
		c.String(http.StatusOK, v.(string))
	})
	return r
}

func TestAppAdminAllowsAppAdmin(t *testing.T) {
	checker := &fakeAppAdmin{}
	r := newAppAdminRouter(checker)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me/apps/demo-app", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, 期望 200", rec.Code)
	}
	if rec.Body.String() != "demo-app" {
		t.Fatalf("CtxClientID = %q, 期望 demo-app", rec.Body.String())
	}
	if checker.clientID != "demo-app" || checker.userID != "01J9ZK9M1N3P5Q7R9S1T3V5W7X" {
		t.Fatalf("判定入参 = (%q,%q)", checker.clientID, checker.userID)
	}
}

// 四种拒绝原因必须返回完全一致的响应体：不得暴露应用是否存在（FR-010）。
func TestAppAdminDeniesWithIdenticalBody(t *testing.T) {
	reasons := []error{
		errors.New("client not found"),
		errors.New("app member not found"),
		errors.New("app admin required"),
		errors.New("app client disabled"),
	}
	var first string
	for i, reason := range reasons {
		r := newAppAdminRouter(&fakeAppAdmin{err: reason})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me/apps/demo-app", nil))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("reason %d: status = %d, 期望 403", i, rec.Code)
		}
		raw := rec.Body.Bytes()
		if first == "" {
			first = string(raw)
			continue
		}
		// traceId 每次不同，只比较 code 与 data 部分。
		var got, want map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("reason %d: 响应体不是统一响应: %v", i, err)
		}
		if err := json.Unmarshal([]byte(first), &want); err != nil {
			t.Fatalf("首个响应体不可解析: %v", err)
		}
		if got["code"] != want["code"] || got["message"] != want["message"] || got["data"] != want["data"] {
			t.Fatalf("reason %d: 响应体与其他拒绝原因不一致（可能泄露应用是否存在）", i)
		}
	}
	if first == "" {
		t.Fatal("未产生任何响应")
	}
}

func TestAppAdminRequiresSession(t *testing.T) {
	r := newAppAdminRouter(&fakeAppAdmin{})
	req := httptest.NewRequest(http.MethodGet, "/me/apps/demo-app", nil)
	req.Header.Set("X-Test-Anonymous", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, 期望 401", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "UNAUTHENTICATED" {
		t.Fatalf("code = %q, 期望 UNAUTHENTICATED", body.Code)
	}
}
