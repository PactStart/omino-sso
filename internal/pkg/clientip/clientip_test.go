package clientip

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// 本文件固定 007 的可信代理契约（tasks.md T008）：五个用例分别对应
// 直连、单跳、多跳、伪造 XFF、漏配前端网段。
//
// 背景：多域名部署的真实链路是 `浏览器 → 入口 LB → 前端 nginx → api 入口 → sso:8081`，
// XFF 至少两跳。判定规则只有一条：**直连方位于可信网段时才采信 XFF，并从右向左
// 取第一个非可信地址**。漏配某一段不会报错，只会让审计 IP 静默变成容器网段，
// 因此每个分支都必须有用例钉住。

func prefixes(t *testing.T, raw ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(raw))
	for _, r := range raw {
		p, err := netip.ParsePrefix(r)
		if err != nil {
			t.Fatalf("测试前置：非法 CIDR %q: %v", r, err)
		}
		out = append(out, p.Masked())
	}
	return out
}

func request(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		xff     string
		trusted []string
		want    string
	}{
		{
			// 用例 1：直连（没有反代）。即使客户端自带 XFF 也无从采信。
			name:    "直连且未配置可信网段",
			remote:  "203.0.113.9:54321",
			xff:     "1.2.3.4",
			trusted: nil,
			want:    "203.0.113.9",
		},
		{
			// 用例 2：单跳（LB 终止）。remote 是 LB，XFF 里只有一个真实客户端。
			name:    "单跳 LB",
			remote:  "10.0.0.7:443",
			xff:     "198.51.100.20",
			trusted: []string{"10.0.0.0/8"},
			want:    "198.51.100.20",
		},
		{
			// 用例 3：多跳（LB → 前端 nginx → api 入口）。从右向左跳过可信地址，取第一个非可信值。
			name:    "多跳取最右侧非可信地址",
			remote:  "172.18.0.6:41234",
			xff:     "198.51.100.20, 10.0.0.7",
			trusted: []string{"10.0.0.0/8", "172.18.0.0/16"},
			want:    "198.51.100.20",
		},
		{
			// 用例 4：伪造 XFF。直连方不可信 → 整条 XFF 一律忽略，取 RemoteAddr。
			name:    "不可信来源伪造 XFF 被忽略",
			remote:  "203.0.113.9:5555",
			xff:     "1.2.3.4, 5.6.7.8",
			trusted: []string{"10.0.0.0/8"},
			want:    "203.0.113.9",
		},
		{
			// 用例 5（反证）：只配了 LB 网段、漏配前端容器网段。
			// 结果会退化成前端容器 IP（172.x）——这正是 quickstart §4 要求人工观察的现象：
			// 系统不报错，只是审计 IP 悄悄变错。
			name:    "漏配前端容器网段时退化为容器 IP",
			remote:  "172.18.0.6:41234",
			xff:     "198.51.100.20, 10.0.0.7",
			trusted: []string{"10.0.0.0/8"},
			want:    "172.18.0.6",
		},
		{
			// 整条链都在可信网段内（同为内网直连）：退化为最左值，即最接近原始客户端的一项。
			name:    "整链可信时取最左值",
			remote:  "10.0.0.7:443",
			xff:     "10.0.1.5, 10.0.2.6",
			trusted: []string{"10.0.0.0/8"},
			want:    "10.0.1.5",
		},
		{
			// IPv6：RemoteAddr 是 [::1]:port 形式，必须正确剥离方括号与端口。
			name:    "IPv6 直连",
			remote:  "[::1]:54321",
			xff:     "",
			trusted: nil,
			want:    "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromRequest(request(tt.remote, tt.xff), prefixes(t, tt.trusted...))
			if got != tt.want {
				t.Fatalf("FromRequest() = %q, want %q（remote=%q xff=%q trusted=%v）",
					got, tt.want, tt.remote, tt.xff, tt.trusted)
			}
		})
	}
}

// 空请求不 panic：探针/健康检查可能没有真实请求上下文。
func TestFromRequest_NilRequest(t *testing.T) {
	if got := FromRequest(nil, nil); got != "" {
		t.Fatalf("FromRequest(nil) = %q, want empty", got)
	}
}
