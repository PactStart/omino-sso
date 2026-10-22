package admin

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestHandler() *Handler {
	return &Handler{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// 请求体非法 JSON → 400 VALIDATION_FAILED（不触达 service）。
func TestAddMembersRejectsMalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	r := gin.New()
	r.POST("/clients/:id/members", h.AddMembers)

	req := httptest.NewRequest(http.MethodPost, "/clients/demo-app/members", strings.NewReader(`{"userIds":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, 期望 VALIDATION_FAILED", body.Code)
	}
}

// userIds 与 deptId 同时提交 → 400（互斥，不触达 service）。
func TestAddMembersRejectsUserIdAndDeptID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	r := gin.New()
	r.POST("/clients/:id/members", h.AddMembers)

	req := httptest.NewRequest(http.MethodPost, "/clients/demo-app/members",
		strings.NewReader(`{"userIds":["01J9ZJ8Q2M4N6P8R0T2V4X6Z8A"],"deptId":"01J9ZJ7A1B2C3D4E5F6G7H8J9K"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400", rec.Code)
	}
}

// 006：设置应用管理员 —— 请求体非法或缺少 isAdmin 时 400 VALIDATION_FAILED（不触达 service）。
func TestSetMemberAdminRejectsBadBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	r := gin.New()
	r.PUT("/clients/:id/members/:uid/admin", h.SetMemberAdmin)

	for _, body := range []string{`{"isAdmin":`, `{}`, `{"isAdmin":null}`, `{"isAdmin":"yes"}`} {
		req := httptest.NewRequest(http.MethodPut, "/clients/demo-app/members/u1/admin", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, 期望 400", body, rec.Code)
		}
		var resp struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VALIDATION_FAILED" {
			t.Fatalf("body %q: code = %q, 期望 VALIDATION_FAILED", body, resp.Code)
		}
	}
}
