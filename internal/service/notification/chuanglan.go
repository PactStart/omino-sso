package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store/model"
)

// chuanglanProvider 创蓝短信实现：直连其 HTTP 接口，不引入第三方 SDK（决策 2）。
// 3 秒超时、不自动重试（失败由用户重新获取验证码）。
type chuanglanProvider struct {
	url      string
	account  string
	password string
	template string
	client   *http.Client
}

// NewChuanglan 构造创蓝短信实现。
func NewChuanglan(cfg config.SmsConfig) Provider {
	return &chuanglanProvider{
		url:      cfg.ChuanglanURL,
		account:  cfg.ChuanglanAccount,
		password: cfg.ChuanglanPassword,
		template: cfg.ChuanglanTemplate,
		client:   &http.Client{Timeout: 3 * time.Second},
	}
}

func (p *chuanglanProvider) Name() string { return model.SendProviderChuanglan }

// Send 发送验证码短信。{code} 占位由模板文案渲染为真实验证码。
func (p *chuanglanProvider) Send(ctx context.Context, channel, target, code string) error {
	if channel != ChannelSMS {
		return ErrNotSupported
	}
	payload := map[string]any{
		"account":  p.account,
		"password": p.password,
		"msg":      strings.ReplaceAll(p.template, "{code}", code),
		"phone":    target,
		"report":   "false",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("chuanglan marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("chuanglan request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("chuanglan call: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("chuanglan read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chuanglan http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out struct {
		Code     string `json:"code"`
		Msg      string `json:"msg"`
		ErrorMsg string `json:"errorMsg"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("chuanglan decode: %s", truncate(string(raw), 200))
	}
	if out.Code != "0" {
		return fmt.Errorf("chuanglan code=%s msg=%s errorMsg=%s", out.Code, out.Msg, out.ErrorMsg)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
