package notification

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSend_ImplicitTLS 复现 465/SMTPS 场景：服务器连上即等 TLS ClientHello。
// 修复前（明文 net.Dial + smtp.NewClient）双方互等到 i/o timeout；
// 修复后应通过 email.SendWithTLS 完成投递，且正文为 multipart/alternative。
func TestSend_ImplicitTLS(t *testing.T) {
	srv := startTLSSMTPServer(t)
	p := &smtpProvider{
		host:        "127.0.0.1",
		port:        srv.port,
		username:    "user@example.com",
		password:    "secret",
		from:        "omino-sso <no-reply@example.com>",
		envelope:    "user@example.com",
		implicitTLS: true,
		tlsConf:     &tls.Config{ServerName: "127.0.0.1", InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		tpl:         testTemplate(t),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Send(ctx, ChannelEmail, "to@example.com", "123456"); err != nil {
		t.Fatalf("implicit TLS 投递失败: %v", err)
	}

	msg := srv.message()
	for _, want := range []string{
		"Content-Type: multipart/alternative",
		"Content-Type: text/plain",
		"Content-Type: text/html",
		"To: <to@example.com>",
		"Subject: =?UTF-8?",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("报文缺少 %q\n%s", want, msg)
		}
	}
	// 信封发件人应为登录账号（否则 126/163 回 553）。
	if !srv.sawMailFrom("user@example.com") {
		t.Errorf("MAIL FROM 应为认证账号，实际记录: %v", srv.commands())
	}
}

// TestSend_Timeout 守住 T013 的 3 秒上限：库自身不带超时，超出后必须由
// smtpProvider 的 watchdog 返回 ErrSendTimeout（上层据此写失败记录并提示用户）。
func TestSend_Timeout(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		// 接受连接但永不发 220 问候语，模拟供应商卡死。
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	prev := smtpSendTimeout
	smtpSendTimeout = 200 * time.Millisecond
	t.Cleanup(func() { smtpSendTimeout = prev })

	p := &smtpProvider{
		host:        "127.0.0.1",
		port:        ln.Addr().(*net.TCPAddr).Port,
		from:        "omino-sso <no-reply@example.com>",
		envelope:    "no-reply@example.com",
		implicitTLS: true,
		tlsConf:     &tls.Config{ServerName: "127.0.0.1", InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		tpl:         testTemplate(t),
	}
	err = p.Send(context.Background(), ChannelEmail, "to@example.com", "123456")
	if !errors.Is(err, ErrSendTimeout) {
		t.Fatalf("期望 ErrSendTimeout，实际 %v", err)
	}
}

// TestSend_ExpireMinutesFollowsConfig 邮件正文的有效期必须跟随 VERIFY_CODE_TTL，
// 不得硬编码 5 分钟（改配置后文案要与实际有效期一致）。
func TestSend_ExpireMinutesFollowsConfig(t *testing.T) {
	p := &smtpProvider{expireMinutes: 10, tpl: testTemplate(t)}
	html, err := p.render("123456")
	if err != nil {
		t.Fatalf("render 失败: %v", err)
	}
	if !strings.Contains(html, "10 分钟内有效") {
		t.Errorf("邮件正文应写 10 分钟，实际:\n%s", html)
	}
	if (&smtpProvider{}).validMinutes() != 5 {
		t.Error("未配置时应退回默认 5 分钟")
	}
}

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.ParseFiles("../../../configs/verify-mail.tmpl")
	if err != nil {
		t.Fatalf("解析邮件模板失败: %v", err)
	}
	return tpl
}

// fakeSMTPServer 是一个仅接受 TLS 的极简 SMTP 服务，只实现握手所需命令。
type fakeSMTPServer struct {
	port int

	mu   sync.Mutex
	buf  strings.Builder
	cmds []string
}

func (s *fakeSMTPServer) message() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *fakeSMTPServer) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

func (s *fakeSMTPServer) sawMailFrom(addr string) bool {
	for _, c := range s.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "MAIL FROM:") && strings.Contains(c, addr) {
			return true
		}
	}
	return false
}

func startTLSSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	srv := &fakeSMTPServer{}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	return srv
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	br := bufio.NewReader(conn)
	_, _ = fmt.Fprint(conn, "220 fake ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		s.mu.Lock()
		s.cmds = append(s.cmds, strings.TrimSpace(line))
		s.mu.Unlock()
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			_, _ = fmt.Fprint(conn, "250-fake\r\n250-AUTH PLAIN\r\n250 OK\r\n")
		case strings.HasPrefix(cmd, "AUTH"):
			_, _ = fmt.Fprint(conn, "235 ok\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			_, _ = fmt.Fprint(conn, "354 end with <CRLF>.<CRLF>\r\n")
			var body strings.Builder
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
				body.WriteString(l)
			}
			s.mu.Lock()
			s.buf.WriteString(body.String())
			s.mu.Unlock()
			_, _ = fmt.Fprint(conn, "250 queued\r\n")
		case strings.HasPrefix(cmd, "QUIT"):
			_, _ = fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			_, _ = fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatalf("加载证书失败: %v", err)
	}
	return cert
}
