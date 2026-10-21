package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/omino/omino-sso/internal/config"
	"github.com/omino/omino-sso/internal/store/model"
)

const (
	appName    = "omino-sso"
	sceneText  = "验证码校验"
	smtpTimout = 3 * time.Second
)

// smtpProvider 邮箱实现：标准库 net/smtp + html/template 模板文件（决策 3）。
// 正文为 multipart/alternative（text/plain + text/html），3 秒超时、不自动重试。
type smtpProvider struct {
	host     string
	port     int
	username string
	password string
	from     string
	fromAddr string
	tpl      *template.Template
}

// NewSMTP 读取并解析模板文件后构造 SMTP 实现；模板缺失视为配置错误、
// 启动失败（与"缺失必填配置必须启动失败"一致）。
func NewSMTP(cfg config.MailConfig) (Provider, error) {
	tpl, err := template.ParseFiles(cfg.TemplatePath)
	if err != nil {
		return nil, fmt.Errorf("parse MAIL_TEMPLATE_PATH %q: %w", cfg.TemplatePath, err)
	}
	from := cfg.From
	addr := from
	if parsed, err := mail.ParseAddress(from); err == nil {
		addr = parsed.Address
	}
	if addr == "" {
		return nil, fmt.Errorf("MAIL_FROM is invalid: %q", from)
	}
	return &smtpProvider{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     from,
		fromAddr: addr,
		tpl:      tpl,
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
	msg, err := p.buildMessage(target, code, html)
	if err != nil {
		return err
	}
	return p.deliver(target, msg)
}

func (p *smtpProvider) render(code string) (string, error) {
	var buf bytes.Buffer
	data := map[string]any{
		"AppName":       appName,
		"SceneText":     sceneText,
		"Code":          code,
		"ExpireMinutes": 5,
		"Year":          time.Now().Year(),
	}
	if err := p.tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render mail template: %w", err)
	}
	return buf.String(), nil
}

func (p *smtpProvider) buildMessage(to, code, html string) ([]byte, error) {
	boundary := "omino-sso-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	plain := fmt.Sprintf("%s %s 验证码：%s，5 分钟内有效，仅可使用一次。", appName, sceneText, code)

	var b strings.Builder
	b.WriteString("From: " + p.from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", appName+" 验证码") + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(wrapBase64(base64.StdEncoding.EncodeToString([]byte(plain))))
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(wrapBase64(base64.StdEncoding.EncodeToString([]byte(html))))
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(b.String()), nil
}

// deliver 使用带超时的 TCP 连接完成 SMTP 会话（STARTTLS 可用则升级）。
func (p *smtpProvider) deliver(to string, msg []byte) error {
	addr := net.JoinHostPort(p.host, strconv.Itoa(p.port))
	conn, err := net.DialTimeout("tcp", addr, smtpTimout)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(smtpTimout))
	c, err := smtp.NewClient(conn, p.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: p.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if p.username != "" {
		auth := smtp.PlainAuth("", p.username, p.password, p.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(p.fromAddr); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	_ = c.Quit()
	return nil
}

// wrapBase64 将 base64 串按 76 字符折行，符合 RFC 2045 行长限制。
func wrapBase64(s string) string {
	const width = 76
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\r\n")
		s = s[width:]
	}
	b.WriteString(s)
	b.WriteString("\r\n")
	return b.String()
}
