package service

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/omino/omino-sso/internal/store/model"
)

// TestNormalizeOrgName 覆盖名称去空白语义（spec FR-004）。
func TestNormalizeOrgName(t *testing.T) {
	cases := map[string]string{
		"技术中心":     "技术中心",
		"  技术中心  ": "技术中心",
		"\t平台组\n":  "平台组",
		"":         "",
		"   ":      "",
	}
	for in, want := range cases {
		if got := NormalizeOrgName(in); got != want {
			t.Errorf("NormalizeOrgName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestValidateOrgName 覆盖 1–64 rune 边界、空串/全空白/控制字符拒绝。
func TestValidateOrgName(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"技术中心", "技术中心"},
		{"  技术中心  ", "技术中心"},
		{strings.Repeat("部", 64), strings.Repeat("部", 64)},
		{strings.Repeat("a", 64), strings.Repeat("a", 64)},
	}
	for _, tc := range ok {
		got, err := ValidateOrgName(tc.in)
		if err != nil {
			t.Errorf("ValidateOrgName(%q) 意外失败: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ValidateOrgName(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}

	bad := []string{
		"",
		"   ",
		"\t\n",
		strings.Repeat("部", 65),
		"平台\x00组",
		"平台\x07组",
	}
	for _, in := range bad {
		if _, err := ValidateOrgName(in); !errors.Is(err, ErrOrgInvalidName) {
			t.Errorf("ValidateOrgName(%q) = %v, 期望 ErrOrgInvalidName", in, err)
		}
	}
}

// TestNormalizeOrgSort 覆盖缺省 0 与 int32 越界拒绝（data-model §5）。
func TestNormalizeOrgSort(t *testing.T) {
	if got, err := NormalizeOrgSort(nil); err != nil || got != 0 {
		t.Fatalf("缺省 sort = (%d,%v), 期望 (0,nil)", got, err)
	}
	v := 1_000_000
	if got, err := NormalizeOrgSort(&v); err != nil || got != v {
		t.Fatalf("sort = (%d,%v), 期望 (%d,nil)", got, err, v)
	}
	for _, out := range []int{math.MaxInt32 + 1, math.MinInt32 - 1} {
		v := out
		if _, err := NormalizeOrgSort(&v); !errors.Is(err, ErrOrgInvalidSort) {
			t.Errorf("sort=%d 期望 ErrOrgInvalidSort, 实际 %v", out, err)
		}
	}
}

// TestOrgDepth 覆盖物化路径的层级推导（根级为 1）。
func TestOrgDepth(t *testing.T) {
	cases := map[string]int{
		"/01J9ZJ7A1B2C3D4E5F6G7H8J9K/":                            1,
		"/01J9ZJ7A1B2C3D4E5F6G7H8J9K/01J9ZK9M1N3P5Q7R9S1T3V5W7X/": 2,
		"/A/B/C/": 3,
	}
	for path, want := range cases {
		if got := model.OrgDepth(path); got != want {
			t.Errorf("OrgDepth(%q) = %d, 期望 %d", path, got, want)
		}
	}
}

// TestJoinOrgPath 覆盖根级与子级路径拼接。
func TestJoinOrgPath(t *testing.T) {
	if got := model.JoinOrgPath("", "A"); got != "/A/" {
		t.Errorf("根级 JoinOrgPath = %q, 期望 /A/", got)
	}
	if got := model.JoinOrgPath("/", "A"); got != "/A/" {
		t.Errorf("根级（/）JoinOrgPath = %q, 期望 /A/", got)
	}
	if got := model.JoinOrgPath("/A/", "B"); got != "/A/B/" {
		t.Errorf("子级 JoinOrgPath = %q, 期望 /A/B/", got)
	}
	if d := model.OrgDepth(model.JoinOrgPath("/A/B/", "C")); d != 3 {
		t.Errorf("拼接后深度 = %d, 期望 3", d)
	}
}

// TestIsPathPrefix 覆盖自身/后代/兄弟/无关四种判定（成环与子树范围依赖它）。
func TestIsPathPrefix(t *testing.T) {
	a := "/A/"
	ab := "/A/B/"
	ac := "/A/C/"
	b := "/B/"
	cases := []struct {
		prefix, p string
		want      bool
	}{
		{a, a, true},    // 自身
		{a, ab, true},   // 后代
		{ab, a, false},  // 反向
		{a, ac, true},   // 另一分支仍是后代
		{a, b, false},   // 无关
		{ab, ac, false}, // 兄弟
	}
	for _, tc := range cases {
		if got := model.IsPathPrefix(tc.prefix, tc.p); got != tc.want {
			t.Errorf("IsPathPrefix(%q,%q) = %v, 期望 %v", tc.prefix, tc.p, got, tc.want)
		}
	}
	if got := model.OrgPathPattern("/A/"); got != "/A/%" {
		t.Errorf("OrgPathPattern = %q, 期望 /A/%%", got)
	}
}

// TestSubtreeHeight 覆盖子树高度计算（移动时的深度校验依赖它）。
func TestSubtreeHeight(t *testing.T) {
	root := model.Organization{ID: "A", Path: "/A/"}
	child := model.Organization{ID: "B", Path: "/A/B/", ParentID: &root.ID}
	grand := model.Organization{ID: "C", Path: "/A/B/C/", ParentID: &child.ID}
	other := model.Organization{ID: "D", Path: "/D/"}
	orgs := []model.Organization{root, child, grand, other}

	if got := subtreeHeight("/A/", orgs); got != 3 {
		t.Errorf("subtreeHeight(/A/) = %d, 期望 3", got)
	}
	if got := subtreeHeight("/A/B/", orgs); got != 2 {
		t.Errorf("subtreeHeight(/A/B/) = %d, 期望 2", got)
	}
	if got := subtreeHeight("/A/B/C/", orgs); got != 1 {
		t.Errorf("subtreeHeight(/A/B/C/) = %d, 期望 1", got)
	}
}

// TestSameOrgParent 覆盖父节点"是否未变化"的三态比较。
func TestSameOrgParent(t *testing.T) {
	a, b := "A", "B"
	if !sameOrgParent(nil, nil) {
		t.Error("nil 与 nil 应视为同一（均为根级）")
	}
	if sameOrgParent(nil, &a) || sameOrgParent(&a, nil) {
		t.Error("根级与非根级不应视为相同")
	}
	if !sameOrgParent(&a, &a) {
		t.Error("相同 ID 应视为未变化")
	}
	if sameOrgParent(&a, &b) {
		t.Error("不同 ID 应视为已变化")
	}
}

// TestNormalizeOrgID 覆盖空串父节点视为根级。
func TestNormalizeOrgID(t *testing.T) {
	if got := normalizeOrgID(nil); got != nil {
		t.Errorf("nil 应保持 nil, 实际 %v", *got)
	}
	blank := "  "
	if got := normalizeOrgID(&blank); got != nil {
		t.Errorf("全空白应视为根级, 实际 %q", *got)
	}
	v := " 01J9ZJ7A1B2C3D4E5F6G7H8J9K "
	got := normalizeOrgID(&v)
	if got == nil || *got != "01J9ZJ7A1B2C3D4E5F6G7H8J9K" {
		t.Errorf("应去空白并保留 ID, 实际 %v", got)
	}
}

// TestIsRetryableTxError 覆盖死锁/锁等待超时的重试判定。
func TestIsRetryableTxError(t *testing.T) {
	if isRetryableTxError(errors.New("boom")) {
		t.Error("普通错误不应重试")
	}
	if isRetryableTxError(nil) {
		t.Error("nil 不应重试")
	}
}
