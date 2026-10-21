package service

import (
	"reflect"
	"testing"
)

func TestRoleNamePattern(t *testing.T) {
	ok := []string{"a", "opsAdmin", "A1", "Admin", "Z9"}
	bad := []string{"", "ops-admin", "ops_admin", "运维", "a b", "abcdefghijklmnopqrstuvwxyz0123456" /* 33 位 */}
	for _, v := range ok {
		if !roleNamePattern.MatchString(v) {
			t.Fatalf("%q 应通过角色名校验", v)
		}
	}
	for _, v := range bad {
		if roleNamePattern.MatchString(v) {
			t.Fatalf("%q 不应通过角色名校验", v)
		}
	}
}

func TestPermCodePattern(t *testing.T) {
	ok := []string{"order:read", "order.read", "order.read:detail", "a-b_c", "A1", "x.y-z:w"}
	bad := []string{"", "订单读取", "a b", "a/b", "a;b", "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0123456789abc" /* 65 位 */}
	for _, v := range ok {
		if !permCodePattern.MatchString(v) {
			t.Fatalf("%q 应通过权限编码校验", v)
		}
	}
	for _, v := range bad {
		if permCodePattern.MatchString(v) {
			t.Fatalf("%q 不应通过权限编码校验", v)
		}
	}
}

func TestDedupStrings(t *testing.T) {
	got := dedupStrings([]string{"u1", " u2 ", "u1", "", "u3"})
	want := []string{"u1", "u2", "u3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupStrings = %v, 期望 %v", got, want)
	}
	if got := dedupStrings(nil); len(got) != 0 {
		t.Fatalf("空输入应返回空切片，实际 %v", got)
	}
}
