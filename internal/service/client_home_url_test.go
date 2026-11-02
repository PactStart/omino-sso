package service

import (
	"context"
	"errors"
	"testing"
)

// 首页地址（011）的纯校验边界：注册与编辑都必填，且必须是 http(s) 绝对地址。
// 这些用例不依赖数据库：Create 在碰 store 之前就完成校验。

func TestValidateRequiredURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
		want string
	}{
		{name: "正常 https", in: "https://app.example.com", ok: true, want: "https://app.example.com"},
		{name: "正常 http + 端口", in: "http://localhost:5173", ok: true, want: "http://localhost:5173"},
		{name: "去除首尾空白", in: "  https://app.example.com  ", ok: true, want: "https://app.example.com"},
		{name: "空串被拒绝", in: "", ok: false},
		{name: "只有空白被拒绝", in: "   ", ok: false},
		{name: "缺 scheme 被拒绝", in: "app.example.com", ok: false},
		{name: "非 http(s) 被拒绝", in: "ftp://app.example.com", ok: false},
		{name: "缺 host 被拒绝", in: "https://", ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := validateRequiredURL(c.in, 512)
			if c.ok {
				if err != nil {
					t.Fatalf("%q 应通过校验，实际报错 %v", c.in, err)
				}
				if got != c.want {
					t.Fatalf("归一化结果 = %q，期望 %q", got, c.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("%q 应被拒绝，实际通过（got=%q）", c.in, got)
			}
			if !errors.Is(err, ErrAppValidation) {
				t.Fatalf("%q 的错误应为 ErrAppValidation，实际 %v", c.in, err)
			}
		})
	}
}

func TestCreateRejectsEmptyHomeURL(t *testing.T) {
	// 未注入 store：一旦校验没拦住就会 panic，而不是静默通过。
	svc := &ClientService{}
	_, _, err := svc.Create(context.Background(), CreateClientInput{
		ClientName:   "验收应用",
		RedirectURIs: []string{"https://demo.example.com/cb"},
	})
	if !errors.Is(err, ErrAppValidation) {
		t.Fatalf("缺首页地址应返回 ErrAppValidation，实际 %v", err)
	}
}
