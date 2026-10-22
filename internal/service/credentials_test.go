package service

import "testing"

// TestDetectIdentifier 覆盖 FR-005：按输入格式识别账号类型。
func TestDetectIdentifier(t *testing.T) {
	cases := []struct {
		in       string
		wantKind IdentifierKind
		wantVal  string
	}{
		{"13800138000", IdentifierPhone, "13800138000"},
		{" 13800138000 ", IdentifierPhone, "13800138000"},
		{"23800138000", IdentifierUsername, "23800138000"},
		{"zhangsan", IdentifierUsername, "zhangsan"},
		{" ZhangSan@Example.COM ", IdentifierEmail, "zhangsan@example.com"},
		{"admin", IdentifierUsername, "admin"},
		{"1380013800", IdentifierUsername, "1380013800"},
	}
	for _, c := range cases {
		kind, val := DetectIdentifier(c.in)
		if kind != c.wantKind || val != c.wantVal {
			t.Fatalf("DetectIdentifier(%q) = (%v,%q)，期望 (%v,%q)", c.in, kind, val, c.wantKind, c.wantVal)
		}
	}
}

// TestValidatePassword 覆盖密码强度规则（8–128 且含字母与数字）。
func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw   string
		want bool
	}{
		{"abc12345", true},
		{"Abcdefg1", true},
		{"abc1234", false},
		{"12345678", false},
		{"abcdefgh", false},
	}
	for _, c := range cases {
		if got := ValidatePassword(c.pw); got != c.want {
			t.Fatalf("ValidatePassword(%q) = %v，期望 %v", c.pw, got, c.want)
		}
	}
}
