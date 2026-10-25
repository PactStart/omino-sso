package oidc

// 登录落点判定与门户跳转地址构造（009-portal-login-redirect）。
//
// 为什么判定放在这里而不是 op.Client.LoginURL：协议库对**每一次**授权都会调用
// LoginURL，而它拿不到我们的会话状态。只有 /login 处理器同时知道「请求是否存在」、
// 「浏览器是否带有效会话」与「URL 上有没有回跳标记」，因此落点只能在这里决定，
// 并且抽成纯函数以便确定性单测（见 plan.md D1 / research.md R1）。

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

const (
	// portalMarkerParam 是「已从门户回跳」的标记参数名，值恒为 portalMarkerValue。
	// 由 IdP 生成、随一次性授权请求 ID 绑定，不含任何凭据：用户删掉它只会多一次
	// 门户往返（每次 StepPortal 都会重新生成带标记的完整地址），加上它也不会提权。
	portalMarkerParam = "portal"
	portalMarkerValue = "1"

	// portalAppParam 携带来源应用显示名，仅用于门户登录页的副标题文案。
	portalAppParam = "app"
)

// LoginStep 是 GET /login 的六种落点（contracts/login-entry.md 的 C1a–C5）。
type LoginStep int

const (
	// StepComplete：有会话 → 直接完成授权并 302 回调（FR-009）。
	StepComplete LoginStep = iota
	// StepPortal：无会话 + 已配置门户地址 + 无标记 → 302 门户登录页（FR-001）。
	StepPortal
	// StepTemplate：无会话 + 未配置门户地址 → 内置登录页（FR-005 降级）。
	StepTemplate
	// StepTemplateDegraded：无会话 + 已配置 + 有标记 → 内置登录页（降级态，FR-006）。
	StepTemplateDegraded
	// StepRequestExpired：请求不存在/过期 + 有标记 → 过期提示页（FR-021）。
	StepRequestExpired
	// StepBadRequest：请求不存在/过期 + 无标记 → 既有 400 文本（行为不变）。
	StepBadRequest
)

// decideStep 决定本次 GET /login 的落点。
//
// 判定顺序不可调换：
//  1. 请求不存在/过期优先于一切（此时拿不到 client，也就无法跳门户）；
//  2. 会话优先于标记——已登录用户即便 URL 带 portal=1 也必须直接完成授权；
//  3. 未配置门户地址时永远是内置页（dev/test 的降级路径）；
//  4. 带标记却仍无会话 = 门户往返没带回来会话 → 降级，不再跳门户（防死循环）。
func decideStep(reqExists, hasSession, portalConfigured, portalMarker bool) LoginStep {
	if !reqExists {
		if portalMarker {
			return StepRequestExpired
		}
		return StepBadRequest
	}
	if hasSession {
		return StepComplete
	}
	if !portalConfigured {
		return StepTemplate
	}
	if portalMarker {
		return StepTemplateDegraded
	}
	return StepPortal
}

// ssoLoginEvent 构造「凭既有会话直接完成授权」的审计事件（FR-010）。
//
// entry 区分回跳来源（portal / template），amr 取会话的认证方式；
// 事件里只放这些摘要字段，绝不写 sid / token / 密码 / 验证码（宪法 IV）。
func ssoLoginEvent(userID, username, clientID, ip, userAgent string, amr []string, fromPortal bool) service.Event {
	entry := "template"
	if fromPortal {
		entry = "portal"
	}
	if amr == nil {
		amr = []string{}
	}
	return service.Event{
		Type:      model.EventSSOLoginSuccess,
		UserID:    userID,
		Username:  username,
		ClientID:  clientID,
		IP:        ip,
		UserAgent: userAgent,
		Result:    "success",
		Fields:    map[string]any{"entry": entry, "amr": amr},
	}
}

// portalRedirectURL 构造送到门户登录页的地址。
//
//	base   = 配置的门户登录页（绝对地址，可能已带查询串，如 /login?from=sso）
//	next   = <issuer>/login?id=<reqID>&portal=1
//	app    = 应用显示名（为空则不下发该参数）
//
// next 必须是**单次**编码：门户解码一次后拿到的地址要能直接整页跳转，
// 二次编码会让 id 带着多余转义回来，命中「请求已过期」。
func portalRedirectURL(base, issuer, reqID, clientName string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", errors.New("portal login url is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse portal login url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("portal login url must be absolute (scheme http/https): %q", base)
	}
	if u.Host == "" {
		return "", fmt.Errorf("portal login url must have a host: %q", base)
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("portal login url must not contain a fragment: %q", base)
	}
	if strings.TrimSpace(issuer) == "" {
		return "", errors.New("issuer is empty")
	}

	next := strings.TrimSuffix(issuer, "/") + "/login?id=" + url.QueryEscape(reqID) + "&" + portalMarkerParam + "=" + portalMarkerValue

	q := u.Query()
	q.Set("next", next)
	if name := strings.TrimSpace(clientName); name != "" {
		q.Set(portalAppParam, name)
	} else {
		q.Del(portalAppParam)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
