package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omino/omino-sso/internal/config"
)

// captureChuanglan 起一个假创蓝接口，返回捕获到的请求体。
func captureChuanglan(t *testing.T, respBody string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// TestChuanglanSend_TemplateMode 覆盖 V2 模板报备模式：
// 必须发 templateId + phoneNumbers（字符串）+ templateParamJson（字符串里装 JSON 数组），
// 且不得再带 msg；成功码为 000000。
func TestChuanglanSend_TemplateMode(t *testing.T) {
	srv, got := captureChuanglan(t, `{"code":"000000","successNum":"1","failNum":"0"}`)
	p := NewChuanglan(config.SmsConfig{
		ChuanglanURL:        srv.URL,
		ChuanglanAccount:    "N4302541",
		ChuanglanPassword:   "pwd",
		ChuanglanTemplateID: "1019888301",
		ChuanglanTemplate:   "验证码：{s}",
	})
	if err := p.Send(context.Background(), ChannelSMS, "13800000000", "123456"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if v, _ := (*got)["templateId"].(string); v != "1019888301" {
		t.Errorf("templateId = %v，期望 1019888301", (*got)["templateId"])
	}
	if v, _ := (*got)["phoneNumbers"].(string); v != "13800000000" {
		t.Errorf("phoneNumbers = %v，期望字符串 13800000000", (*got)["phoneNumbers"])
	}
	raw, _ := (*got)["templateParamJson"].(string)
	var params []map[string]string
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatalf("templateParamJson 应为 JSON 数组字符串，实际 %q: %v", raw, err)
	}
	if len(params) != 1 || params[0]["param1"] != "123456" {
		t.Errorf("templateParamJson = %q，期望 [{\"param1\":\"123456\"}]", raw)
	}
	for _, f := range []string{"msg", "phone"} {
		if _, has := (*got)[f]; has {
			t.Errorf("模板模式不应发送 %s 字段", f)
		}
	}
}

// TestChuanglanSend_ParamKeyHardcoded 参数 key 必须恒为创蓝按位置约定的 param1，
// 不受任何环境变量影响（历史上做成配置项后，配错会让网关报「所有手机号或参数均不合法」）。
func TestChuanglanSend_ParamKeyHardcoded(t *testing.T) {
	srv, got := captureChuanglan(t, `{"code":"000000","successNum":"1","failNum":"0"}`)
	p := NewChuanglan(config.SmsConfig{
		ChuanglanURL:        srv.URL,
		ChuanglanAccount:    "acc",
		ChuanglanPassword:   "pwd",
		ChuanglanTemplateID: "1019888301",
	})
	if err := p.Send(context.Background(), ChannelSMS, "15002080164", "8888"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if raw, _ := (*got)["templateParamJson"].(string); raw != `[{"param1":"8888"}]` {
		t.Errorf("templateParamJson = %q，期望 [{\"param1\":\"8888\"}]", raw)
	}
}

// TestChuanglanSend_ErrorCode 非成功码（如 130）必须作为错误返回，便于上层记录与提示。
func TestChuanglanSend_ErrorCode(t *testing.T) {
	srv, _ := captureChuanglan(t, `{"code":"130","errorMsg":"templateId 不能为空"}`)
	p := NewChuanglan(config.SmsConfig{
		ChuanglanURL:        srv.URL,
		ChuanglanAccount:    "acc",
		ChuanglanPassword:   "pwd",
		ChuanglanTemplateID: "1019888301",
	})
	err := p.Send(context.Background(), ChannelSMS, "13800000000", "123456")
	if err == nil || !strings.Contains(err.Error(), "code=130") {
		t.Fatalf("期望返回 code=130 错误，实际 %v", err)
	}
}

// TestChuanglanSend_ContentMode 覆盖未配模板 ID 时的内容模式：
// {code} 与创蓝习惯的 {s} 都应被替换为验证码。
func TestChuanglanSend_ContentMode(t *testing.T) {
	for _, tmpl := range []string{"验证码：{code}，5 分钟内有效", "验证码：{s}，请勿泄露"} {
		srv, got := captureChuanglan(t, `{"code":"0","msg":"ok"}`)
		p := NewChuanglan(config.SmsConfig{
			ChuanglanURL:      srv.URL,
			ChuanglanAccount:  "N4302541",
			ChuanglanPassword: "pwd",
			ChuanglanTemplate: tmpl,
		})
		if err := p.Send(context.Background(), ChannelSMS, "13800000000", "654321"); err != nil {
			t.Fatalf("Send 失败: %v", err)
		}
		msg, _ := (*got)["msg"].(string)
		if !strings.Contains(msg, "654321") {
			t.Errorf("msg = %q，期望含验证码", msg)
		}
		if strings.Contains(msg, "{code}") || strings.Contains(msg, "{s}") {
			t.Errorf("msg = %q，占位符未被替换", msg)
		}
		if _, has := (*got)["templateId"]; has {
			t.Error("内容模式不应发送 templateId")
		}
	}
}
