package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"math"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/jordan-wright/email"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store/model"
)

const (
	appName   = "omino-sso"
	sceneText = "验证码校验"

	// 端口决定 TLS 形态：465 是 SMTPS（连上即握手），587 是明文 + STARTTLS，
	// 其余（25 等）按明文发送。历史上这里一律明文拨号，对 465 会互等到 i/o timeout。
	portSMTPS    = 465
	portSTARTTLS = 587
)

// smtpSendTimeout 是单封邮件从发起到结束的上限（T013「3s 超时」）。
// 该库的 Send* 内部用 tls.Dial、不接受 ctx 也没有超时参数，因此在这里用外层
// watchdog 兜底：超时即向调用方返回错误并落失败记录，不阻塞 HTTP 请求。
// 声明为变量以便测试缩短等待。
var smtpSendTimeout = 3 * time.Second

// ErrSendTimeout 表示邮件在 smtpSendTimeout 内未完成。
var ErrSendTimeout = errors.New("smtp send timeout")

// smtpProvider 邮箱实现：报文组装与投递都用 github.com/jordan-wright/email，
// 正文为 multipart/alternative（text/plain + text/html）。
type smtpProvider struct {
	host          string
	port          int
	username      string
	password      string
	from          string
	envelope      string // SMTP 信封发件人（MAIL FROM），126/163 要求等于登录账号
	implicitTLS   bool   // 465：连接建立后立即 TLS 握手
	startTLS      bool   // 587：明文连接后 STARTTLS 升级
	tlsConf       *tls.Config
	tpl           *template.Template
	expireMinutes int // 邮件正文中的有效期，取自 VERIFY_CODE_TTL，不硬编码
}

// NewSMTP 读取并解析模板文件后构造 SMTP 实现；模板缺失视为配置错误、
// 启动失败（与"缺失必填配置必须启动失败"一致）。
// codeTTL 为验证码有效期，用于渲染邮件正文里的「N 分钟内有效」，避免与配置脱节。
func NewSMTP(cfg config.MailConfig, codeTTL time.Duration) (Provider, error) {
	tpl, err := template.ParseFiles(cfg.TemplatePath)
	if err != nil {
		return nil, fmt.Errorf("parse MAIL_TEMPLATE_PATH %q: %w", cfg.TemplatePath, err)
	}
	from := cfg.From
	fromAddr := from
	if parsed, err := mail.ParseAddress(from); err == nil {
		fromAddr = parsed.Address
	}
	if fromAddr == "" {
		return nil, fmt.Errorf("MAIL_FROM is invalid: %q", from)
	}
	// 信封发件人优先取登录账号：126/163 等会回
	// "553 Mail from must equal authorized user"，要求 MAIL FROM 与认证账号一致。
	// 账号不是邮箱形态（如 SendGrid 的 "apikey"）时退回 MAIL_FROM 里的地址。
	envelope := fromAddr
	if login, err := mail.ParseAddress(cfg.SMTPUsername); err == nil && strings.Contains(login.Address, "@") {
		envelope = login.Address
	}
	// 分钟数向上取整：90 秒应按「2 分钟」表述，避免正文比实际有效期更短。
	minutes := int(math.Ceil(codeTTL.Minutes()))
	if minutes <= 0 {
		minutes = 5
	}
	return &smtpProvider{
		host:          cfg.SMTPHost,
		port:          cfg.SMTPPort,
		username:      cfg.SMTPUsername,
		password:      cfg.SMTPPassword,
		from:          from,
		envelope:      envelope,
		implicitTLS:   cfg.SMTPPort == portSMTPS,
		startTLS:      cfg.SMTPPort == portSTARTTLS,
		tlsConf:       &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12},
		expireMinutes: minutes,
		tpl:           tpl,
	}, nil
}

func (p *smtpProvider) Name() string { return model.SendProviderSMTP }

// Send 渲染模板并发送验证码邮件。
func (p *smtpProvider) Send(ctx context.Context, channel, target, code string) error {
	if channel != ChannelEmail {
		return ErrNotSupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	html, err := p.render(code)
	if err != nil {
		return err
	}

	e := email.NewEmail()
	e.From = p.from
	e.To = []string{target}
	// 该库按原样写入 Subject 头，非 ASCII 主题须自行做 RFC 2047 编码。
	e.Subject = mime.QEncoding.Encode("UTF-8", appName+" 验证码")
	e.Text = []byte(fmt.Sprintf("%s %s 验证码：%s，%d 分钟内有效，仅可使用一次。", appName, sceneText, code, p.validMinutes()))
	e.HTML = []byte(html)
	// 覆盖信封发件人（默认取 From 里的地址），避免与企业邮箱的认证账号不一致。
	e.Sender = p.envelope

	addr := net.JoinHostPort(p.host, strconv.Itoa(p.port))
	var auth smtp.Auth
	if p.username != "" {
		auth = smtp.PlainAuth("", p.username, p.password, p.host)
	}
	return p.sendWithTimeout(ctx, e, addr, auth)
}

// sendWithTimeout 用 watchdog 给库的同步发送加时间上限：
// 超时后立即返回 ErrSendTimeout（上层据此写 send_records 失败记录并提示用户），
// 底层 goroutine 会在库自身返回时结束（最迟受 TCP/TLS 侧超时约束）。
func (p *smtpProvider) sendWithTimeout(ctx context.Context, e *email.Email, addr string, auth smtp.Auth) error {
	done := make(chan error, 1)
	go func() {
		switch {
		case p.implicitTLS:
			done <- e.SendWithTLS(addr, auth, p.tlsConf)
		case p.startTLS:
			done <- e.SendWithStartTLS(addr, auth, p.tlsConf)
		default:
			done <- e.Send(addr, auth)
		}
	}()

	timer := time.NewTimer(smtpSendTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("smtp send %s: %w", addr, err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("smtp send %s: %w", addr, ErrSendTimeout)
	case <-ctx.Done():
		return fmt.Errorf("smtp send %s: %w", addr, ctx.Err())
	}
}

// validMinutes 返回邮件正文里应写的有效期分钟数；未通过 NewSMTP 构造（如测试）
// 时退回默认 5 分钟，避免正文出现「0 分钟内有效」。
func (p *smtpProvider) validMinutes() int {
	if p.expireMinutes > 0 {
		return p.expireMinutes
	}
	return 5
}

func (p *smtpProvider) render(code string) (string, error) {
	var buf bytes.Buffer
	data := map[string]any{
		"AppName":       appName,
		"SceneText":     sceneText,
		"Code":          code,
		"ExpireMinutes": p.validMinutes(),
		"Year":          time.Now().Year(),
	}
	if err := p.tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render mail template: %w", err)
	}
	return buf.String(), nil
}
