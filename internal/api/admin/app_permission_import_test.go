package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/service"
)

// 权限点导入 handler 的错误映射与请求体边界（specs/011-app-permission-import T011 / T022）。
// 这里不触库：超过 512KB、Content-Type 不符、业务错误映射都在到达 service 之前/之外完成。

type importResp struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newImportHandler() *Handler {
	return &Handler{
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		audit: service.NewAuditor(nil, 8, slog.New(slog.NewTextHandler(io.Discard, nil))),
	}
}

func serveImport(t *testing.T, h *Handler, method, path string, body string, contentType string) (int, importResp) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/clients/:id/permissions/import/validate", h.ValidatePermissionImport)
	r.POST("/clients/:id/permissions/import", h.ConfirmPermissionImport)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var got importResp
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
	}
	return rec.Code, got
}

// Content-Type 不是 application/json → 400 VALIDATION_FAILED（不读 body、不触库）。
func TestPermissionImportRejectsNonJSONContentType(t *testing.T) {
	h := newImportHandler()
	status, body := serveImport(t, h, http.MethodPost,
		"/clients/demo-app/permissions/import/validate", `{"permissions":[]}`, "text/plain")

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d，期望 400", status)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q，期望 VALIDATION_FAILED", body.Code)
	}
}

// 请求体超过 512KB → 413 APP_PERMISSION_IMPORT_TOO_LARGE（提示不声称知道条数）。
func TestPermissionImportRejectsOversizedBody(t *testing.T) {
	h := newImportHandler()
	huge := `{"permissions":[{"code":"a","name":"` + strings.Repeat("x", service.MaxImportBodyBytes+16) + `"}]}`
	status, body := serveImport(t, h, http.MethodPost,
		"/clients/demo-app/permissions/import", huge, "application/json")

	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d，期望 413", status)
	}
	if body.Code != "APP_PERMISSION_IMPORT_TOO_LARGE" {
		t.Fatalf("code = %q，期望 APP_PERMISSION_IMPORT_TOO_LARGE", body.Code)
	}
	// 体积超限时请求体不完整，提示不得声称"本次共 N 条"，只能给上限。
	if strings.Contains(body.Message, "本次共") {
		t.Fatalf("体积超限时不得声称知道条数：%q", body.Message)
	}
	if !strings.Contains(body.Message, "512KB") {
		t.Fatalf("提示应给出实际上限 512KB：%q", body.Message)
	}
	// 体积超限时只给上限与成因提示（FR-013），不猜条数
	if !strings.Contains(body.Message, "可能是描述或名称过长所致") {
		t.Fatalf("体积超限提示应说明可能成因：%q", body.Message)
	}
}

// 条数超限（413）文案必须含本次实际条数与上限（FR-013），与体积超限的文案区分开。
func TestImportTooLargeMessageByReason(t *testing.T) {
	byCount := importTooLargeResponse(&service.ImportTooLargeError{Count: 1001})
	if byCount.Code != "APP_PERMISSION_IMPORT_TOO_LARGE" || byCount.HTTP != http.StatusRequestEntityTooLarge {
		t.Fatalf("条数超限映射 = %s/%d", byCount.Code, byCount.HTTP)
	}
	if !strings.Contains(byCount.Message, "本次共 1001 条") ||
		!strings.Contains(byCount.Message, "上限 1000 条") {
		t.Fatalf("条数超限文案应含实际条数与上限：%q", byCount.Message)
	}

	bySize := importTooLargeResponse(&service.ImportTooLargeError{})
	if strings.Contains(bySize.Message, "本次共") {
		t.Fatalf("体积超限不得声称知道条数：%q", bySize.Message)
	}
	if !strings.Contains(bySize.Message, "512KB") ||
		!strings.Contains(bySize.Message, "可能是描述或名称过长所致") {
		t.Fatalf("体积超限文案缺上限或成因：%q", bySize.Message)
	}
}

// 错误映射表：只有业务拒绝才写失败审计（FR-025）。
func TestImportFailureReasonMapping(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantHTTP  int
		wantAudit bool
		wantWhy   string
	}{
		{"内容非法", service.ErrAppPermissionImportInvalid, "APP_PERMISSION_IMPORT_INVALID", 400, false, ""},
		{"体积超限", service.ErrAppPermissionImportTooLarge, "APP_PERMISSION_IMPORT_TOO_LARGE", 413, true, "too_large"},
		{"并发冲突", service.ErrAppResourceStale, "APP_RESOURCE_STALE", 409, true, "stale"},
		{"应用不存在", service.ErrClientNotFound, "NOT_FOUND", 404, true, "client_not_found"},
		{"应用停用", service.ErrAppClientDisabled, "FORBIDDEN", 403, true, "client_disabled"},
		{"内部错误", errors.New("db down"), "", 0, false, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, respErr, audit := importFailureReason(c.err)
			if c.wantCode == "" {
				if respErr != nil || audit {
					t.Fatalf("未映射错误应返回 nil/false，实际 %v/%v", respErr, audit)
				}
				return
			}
			if respErr == nil {
				t.Fatal("映射结果不得为 nil")
			}
			if respErr.Code != c.wantCode || respErr.HTTP != c.wantHTTP {
				t.Fatalf("映射 = %s/%d，期望 %s/%d", respErr.Code, respErr.HTTP, c.wantCode, c.wantHTTP)
			}
			if audit != c.wantAudit {
				t.Fatalf("audit = %v，期望 %v", audit, c.wantAudit)
			}
			if reason != c.wantWhy {
				t.Fatalf("reason = %q，期望 %q", reason, c.wantWhy)
			}
		})
	}
}

// 409 必须是 409（不得被映射成 400），且失败响应 data 恒为 null（FR-029）。
func TestFailPermissionImportStays409(t *testing.T) {
	h := newImportHandler()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/t", func(c *gin.Context) {
		h.failPermissionImport(c, "demo-app", "paste", service.ErrAppResourceStale)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d，期望 409", rec.Code)
	}
	var body importResp
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解析失败：%v", err)
	}
	if body.Code != "APP_RESOURCE_STALE" {
		t.Fatalf("code = %q，期望 APP_RESOURCE_STALE", body.Code)
	}
	if string(body.Data) != "null" {
		t.Fatalf("失败响应 data 必须为 null，实际 %s", body.Data)
	}
}

// 内容非法 → 400 且 data 为 null；内部错误 → 500 INTERNAL_ERROR。
func TestFailPermissionImportInvalidAndInternal(t *testing.T) {
	h := newImportHandler()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/invalid", func(c *gin.Context) {
		h.failPermissionImport(c, "demo-app", "paste", service.ErrAppPermissionImportInvalid)
	})
	r.POST("/internal", func(c *gin.Context) {
		h.failPermissionImport(c, "demo-app", "paste", errors.New("boom"))
	})

	for _, c := range []struct {
		path string
		http int
		code string
	}{
		{"/invalid", http.StatusBadRequest, "APP_PERMISSION_IMPORT_INVALID"},
		{"/internal", http.StatusInternalServerError, "INTERNAL_ERROR"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, c.path, nil))
		if rec.Code != c.http {
			t.Fatalf("%s status = %d，期望 %d", c.path, rec.Code, c.http)
		}
		var body importResp
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != c.code {
			t.Fatalf("%s code = %q，期望 %q", c.path, body.Code, c.code)
		}
		if string(body.Data) != "null" {
			t.Fatalf("%s 失败响应 data 必须为 null，实际 %s", c.path, body.Data)
		}
	}
}

// 成功导入的审计字段：0 变更不写事件；addedIds 截断 20 条并保留全量计数（FR-023）。
func TestImportAuditFieldsSkipsNoopAndTruncatesIDs(t *testing.T) {
	noChange := &dto.PermissionImportResult{Total: 20, Unchanged: 20}
	if fields, ok := importAuditFields("demo-app", noChange, &service.ImportOutcome{}); ok || fields != nil {
		t.Fatalf("新增与更新都为 0 时不得写成功事件，实际 ok=%v fields=%v", ok, fields)
	}

	ids := make([]string, 25)
	for i := range ids {
		ids[i] = fmt.Sprintf("01ID%02d", i)
	}
	fields, ok := importAuditFields("demo-app",
		&dto.PermissionImportResult{Added: 25, Unchanged: 5, Total: 30, Source: service.ImportSourceFile},
		&service.ImportOutcome{AddedIDs: ids, AddedCount: 25, DocSha256: strings.Repeat("a", 64)})
	if !ok {
		t.Fatal("有新增时应写成功事件")
	}
	got, isSlice := fields["addedIds"].([]string)
	if !isSlice || len(got) != 20 {
		t.Fatalf("addedIds 应截断为前 20 条，实际 %v（%d 条）", fields["addedIds"], len(got))
	}
	if fields["addedCount"] != 25 {
		t.Fatalf("addedCount = %v，期望 25（保留全量计数）", fields["addedCount"])
	}
	if fields["docSha256"] != strings.Repeat("a", 64) {
		t.Fatalf("docSha256 缺失或错误：%v", fields["docSha256"])
	}
	if fields["source"] != service.ImportSourceFile || fields["clientId"] != "demo-app" {
		t.Fatalf("来源/应用字段不对：%v / %v", fields["source"], fields["clientId"])
	}
}
