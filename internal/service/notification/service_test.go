package notification

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store/model"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestMockCodeFixed 覆盖 US6/SC-004：mock 供应商下验证码固定 888888。
func TestMockCodeFixed(t *testing.T) {
	cfg := &config.Config{}
	cfg.SMS.Provider = "mock"
	cfg.Mail.Provider = "mock"
	svc, err := New(cfg, nil, nil, testLogger())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := svc.newCode(model.SendProviderMock); got != MockCode {
		t.Fatalf("mock 验证码 = %q，期望 %q", got, MockCode)
	}
	for i := 0; i < 50; i++ {
		if got := svc.newCode(model.SendProviderMock); got != "888888" {
			t.Fatalf("第 %d 次 mock 验证码 = %q，期望 888888", i, got)
		}
	}
	// 真实供应商必须是 6 位随机数字。
	for i := 0; i < 20; i++ {
		code := svc.newCode(model.SendProviderChuanglan)
		if len(code) != 6 || code == MockCode {
			t.Fatalf("真实供应商验证码 = %q，期望 6 位随机数字", code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("验证码含非数字字符: %q", code)
			}
		}
	}
}

// TestProviderSelection 校验按配置选择实现，未配置时通道关闭。
func TestProviderSelection(t *testing.T) {
	cfg := &config.Config{}
	cfg.SMS.Provider = "mock"
	cfg.Mail.Provider = "mock"
	svc, err := New(cfg, nil, nil, testLogger())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	for _, ch := range []string{ChannelSMS, ChannelEmail} {
		p, err := svc.provider(ch)
		if err != nil {
			t.Fatalf("渠道 %s 应已启用: %v", ch, err)
		}
		if p.Name() != model.SendProviderMock {
			t.Fatalf("渠道 %s provider = %q，期望 mock", ch, p.Name())
		}
	}

	disabled, err := New(&config.Config{}, nil, nil, testLogger())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if _, err := disabled.provider(ChannelSMS); err != ErrChannelDisabled {
		t.Fatalf("未配置短信通道应返回 ErrChannelDisabled，实际 %v", err)
	}
	if disabled.Enabled(ChannelEmail) {
		t.Fatal("未配置邮件通道不应报告为启用")
	}
}

// TestMockProviderDoesNotCallRealVendor 覆盖 US6：mock 实现只返回成功，不触网。
func TestMockProviderDoesNotCallRealVendor(t *testing.T) {
	p := NewMock()
	if p.Name() != model.SendProviderMock {
		t.Fatalf("Name = %q，期望 mock", p.Name())
	}
	if err := p.Send(context.Background(), ChannelSMS, "13800138000", MockCode); err != nil {
		t.Fatalf("mock 短信发送失败: %v", err)
	}
	if err := p.Send(context.Background(), ChannelEmail, "zhangsan@example.com", MockCode); err != nil {
		t.Fatalf("mock 邮件发送失败: %v", err)
	}
	if err := p.Send(context.Background(), "unknown", "x", MockCode); err != ErrNotSupported {
		t.Fatalf("未知渠道应返回 ErrNotSupported，实际 %v", err)
	}
}

// TestNormalizeTarget 覆盖手机号/邮箱格式校验与规范化。
func TestNormalizeTarget(t *testing.T) {
	cases := []struct {
		channel string
		in      string
		want    string
		wantErr bool
	}{
		{ChannelSMS, " 13800138000 ", "13800138000", false},
		{ChannelSMS, "1380013800", "", true},
		{ChannelSMS, "23800138000", "", true},
		{ChannelSMS, "zhangsan@example.com", "", true},
		{ChannelEmail, " ZhangSan@Example.COM ", "zhangsan@example.com", false},
		{ChannelEmail, "zhangsan", "", true},
		{ChannelEmail, "张三 <zhangsan@example.com>", "", true},
		{"unknown", "x", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeTarget(c.channel, c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%s %q 应校验失败", c.channel, c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s %q 校验失败: %v", c.channel, c.in, err)
		}
		if got != c.want {
			t.Fatalf("%s %q 规范化 = %q，期望 %q", c.channel, c.in, got, c.want)
		}
	}
}

// TestMask 覆盖服务端脱敏（澄清 Q4）。
func TestMask(t *testing.T) {
	if got := MaskPhone("13800138000"); got != "138****8000" {
		t.Fatalf("MaskPhone = %q", got)
	}
	if got := MaskEmail("zhangsan@example.com"); got != "z***@example.com" {
		t.Fatalf("MaskEmail = %q", got)
	}
	if got := MaskEmail("a@b.com"); got != "a***@b.com" {
		t.Fatalf("MaskEmail 单字符本地名 = %q", got)
	}
	if got := MaskTarget(ChannelSMS, "13800138000"); got != "138****8000" {
		t.Fatalf("MaskTarget(sms) = %q", got)
	}
}

// TestChannelForScene 校验场景与渠道的匹配约束。
func TestChannelForScene(t *testing.T) {
	if ch, ok := ChannelForScene(SceneBindPhone); !ok || ch != ChannelSMS {
		t.Fatalf("bind_phone 应只允许 sms，实际 %q/%v", ch, ok)
	}
	if ch, ok := ChannelForScene(SceneBindEmail); !ok || ch != ChannelEmail {
		t.Fatalf("bind_email 应只允许 email，实际 %q/%v", ch, ok)
	}
	for _, s := range []string{SceneLogin, SceneFindPassword, SceneMFAUnbind} {
		if ch, ok := ChannelForScene(s); !ok || ch != "" {
			t.Fatalf("%s 应允许双渠道，实际 %q/%v", s, ch, ok)
		}
	}
	if _, ok := ChannelForScene("unknown"); ok {
		t.Fatal("未知场景应不合法")
	}
}
