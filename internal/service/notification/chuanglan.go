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
	url        string
	account    string
	password   string
	templateID string
	template   string
	client     *http.Client
}

// NewChuanglan 构造创蓝短信实现。
func NewChuanglan(cfg config.SmsConfig) Provider {
	return &chuanglanProvider{
		url:        cfg.ChuanglanURL,
		account:    cfg.ChuanglanAccount,
		password:   cfg.ChuanglanPassword,
		templateID: cfg.ChuanglanTemplateID,
		template:   cfg.ChuanglanTemplate,
		client:     &http.Client{Timeout: 3 * time.Second},
	}
}

func (p *chuanglanProvider) Name() string { return model.SendProviderChuanglan }

// Send 发送验证码短信。
//
// 配置了模板 ID 时走 V2 模板报备接口（`/msg/sms/v2/tpl/send`），请求体为：
//
//	{"account":"...","password":"...","phoneNumbers":"13800000000",
//	 "templateId":"1019888301","templateParamJson":"[{\"param1\":\"123456\"}]"}
//
// 三个易错点（均由实测/已验证实现确定）：
//   - `phoneNumbers` 是逗号分隔的**字符串**，传数组会被网关判 HTTP 400；
//   - `templateParamJson` 是**字符串里装 JSON 数组**，直接传数组或传 `["123456"]`
//     会得到 `code=130 templateParamJson 解析异常`；
//   - 变量 key 是创蓝按位置命名的 `param1`…`paramN`，**不是**模板文案里的 {code} 名字。
//
// 成功码为 `000000`（旧接口为 `0`，两者都接受）。
// 未配置模板 ID 时退化为内容模式（老接口：`phone` + `msg` 整段文案）。
func (p *chuanglanProvider) Send(ctx context.Context, channel, target, code string) error {
	if channel != ChannelSMS {
		return ErrNotSupported
	}
	var payload map[string]any
	if p.templateID != "" {
		paramJSON, err := json.Marshal([]map[string]string{{chuanglanParam: code}})
		if err != nil {
			return fmt.Errorf("chuanglan param: %w", err)
		}
		payload = map[string]any{
			"account":           p.account,
			"password":          p.password,
			"phoneNumbers":      target,
			"templateId":        p.templateID,
			"templateParamJson": string(paramJSON),
		}
	} else {
		payload = map[string]any{
			"account":  p.account,
			"password": p.password,
			"phone":    target,
			"report":   "false",
			"msg":      strings.NewReplacer("{code}", code, "{s}", code).Replace(p.template),
		}
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
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		ErrorMsg   string `json:"errorMsg"`
		SuccessNum string `json:"successNum"`
		FailNum    string `json:"failNum"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("chuanglan decode: %s", truncate(string(raw), 200))
	}
	if out.Code != "0" && out.Code != chuanglanSuccessCode {
		return fmt.Errorf("chuanglan code=%s msg=%s errorMsg=%s", out.Code, out.Msg, out.ErrorMsg)
	}
	// V2 接口即使 code=000000，也可能整批号码提交失败（successNum=0）。
	if out.SuccessNum == "0" && out.FailNum != "" && out.FailNum != "0" {
		return fmt.Errorf("chuanglan submit failed: successNum=%s failNum=%s", out.SuccessNum, out.FailNum)
	}
	return nil
}

// chuanglanSuccessCode 是 V2 模板接口的成功码（旧接口为 "0"）。
const chuanglanSuccessCode = "000000"

// chuanglanParam 是模板第一个变量在 templateParamJson 中的 key。
// 创蓝 V2 按**位置**命名变量（param1、param2…），与模板文案里写的 {code} / ${s} 无关，
// 因此这里硬编码而不做成环境变量：配错只会让网关报「所有手机号或参数均不合法」，
// 排查成本远高于它带来的灵活性（本项目只发单变量验证码短信）。
const chuanglanParam = "param1"

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
