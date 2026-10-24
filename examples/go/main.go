// Package main 是 omino-sso 的 Go 接入示例。
//
// 它只演示协议接入，不是产品代码：三个端点之外不暴露任何业务路由，
// 令牌只存在进程内（浏览器只拿到 sid_go）。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// cookieName 必须带语言前缀：三个示例与 IdP 同处 localhost，而 HTTP cookie
// 不按端口隔离，用 sid 会覆盖 IdP 的会话（见 contracts/example-app-api.md）。
const cookieName = "sid_go"

type config struct {
	Issuer          string
	ClientID        string
	ClientSecret    string
	RedirectURI     string
	SessionSecret   string
	Port            string
	UserInfoOverride string
}

func loadConfig() (config, error) {
	c := config{
		Issuer:           strings.TrimRight(os.Getenv("SSO_ISSUER"), "/"),
		ClientID:         os.Getenv("SSO_CLIENT_ID"),
		ClientSecret:     os.Getenv("SSO_CLIENT_SECRET"),
		RedirectURI:      os.Getenv("SSO_REDIRECT_URI"),
		SessionSecret:    os.Getenv("SESSION_SECRET"),
		Port:             os.Getenv("APP_PORT"),
		UserInfoOverride: os.Getenv("SSO_USERINFO_URL_OVERRIDE"),
	}
	if c.Port == "" {
		c.Port = "8000"
	}
	var missing []string
	for name, v := range map[string]string{
		"SSO_ISSUER":        c.Issuer,
		"SSO_CLIENT_ID":     c.ClientID,
		"SSO_CLIENT_SECRET": c.ClientSecret,
		"SSO_REDIRECT_URI":  c.RedirectURI,
		"SESSION_SECRET":    c.SessionSecret,
	} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("缺少必需环境变量：%s", strings.Join(missing, ", "))
	}
	if len(c.SessionSecret) < 32 {
		return c, errors.New("SESSION_SECRET 长度必须 ≥ 32 字符")
	}
	return c, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	app, err := newApp(ctx, cfg)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 连不上 issuer %s：%v\n", cfg.Issuer, err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.handleIndex)
	mux.HandleFunc("GET /login", app.handleLogin)
	mux.HandleFunc("GET /callback", app.handleCallback)
	mux.HandleFunc("GET /api/me", app.handleMe)
	mux.HandleFunc("GET /logout", app.handleLogout)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 端口 %s 不可用（不自动换端口，换端口会让已注册回调失效）：%v\n", cfg.Port, err)
		os.Exit(1)
	}

	log.Printf("[start] listen=:%s issuer=%s redirect=%s", cfg.Port, cfg.Issuer, cfg.RedirectURI)
	if cfg.UserInfoOverride != "" {
		log.Printf("[start] debug=userinfo_override url=%s", cfg.UserInfoOverride)
	}

	idle := make(chan struct{})
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("[fatal] serve: %v", serveErr)
		}
		close(idle)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	<-idle
	log.Printf("[stop] bye")
}
