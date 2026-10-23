// Package clientip 提供协议层（net/http）与 Gin 一致的可信代理来源 IP 解析。
//
// 背景：多域名部署后请求要经过「入口 LB → 前端 nginx → api 入口」多跳，
// X-Forwarded-For 会被逐跳追加。若像早期实现那样无条件信任 XFF，
// 任何客户端都能伪造审计 IP；而若完全不看 XFF，审计里只剩反代地址。
// 因此只在「直连方位于可信网段」时才采信 XFF，并从右向左取第一个非可信地址
// （最右侧由最近的代理写入，左侧可被客户端伪造）。
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// FromRequest 返回可采信的客户端 IP（不含端口）。
//
// trusted 为空表示不信任任何代理；此时直接使用 RemoteAddr，忽略 XFF。
func FromRequest(r *http.Request, trusted []netip.Prefix) string {
	if r == nil {
		return ""
	}
	remote := hostOnly(r.RemoteAddr)
	if remote == "" {
		return ""
	}
	if len(trusted) == 0 {
		return remote
	}
	remoteAddr, err := netip.ParseAddr(remote)
	if err != nil {
		return remote
	}
	if !contains(trusted, remoteAddr) {
		return remote
	}
	chain := forwardedChain(r.Header.Values("X-Forwarded-For"))
	for i := len(chain) - 1; i >= 0; i-- {
		if !contains(trusted, chain[i]) {
			return chain[i].String()
		}
	}
	if len(chain) > 0 {
		// 整条链都来自可信网段：退化为最左值（最接近原始客户端）。
		return chain[0].String()
	}
	return remote
}

// forwardedChain 解析 XFF 中的全部地址（跳过无法解析的项）。
func forwardedChain(values []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(values))
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			item := strings.TrimSpace(part)
			if item == "" {
				continue
			}
			if addr, err := netip.ParseAddr(item); err == nil {
				out = append(out, addr.Unmap())
			}
		}
	}
	return out
}

func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// hostOnly 从 RemoteAddr（可能是 "ip:port"、"ip"、"[v6]:port"）取出主机部分。
func hostOnly(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}
