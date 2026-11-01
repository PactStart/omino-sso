package model

import (
	"strings"
	"testing"
)

// TestOrgGrantMatchSQL 固化判定片段：列名漂移会让 6 处调用点同时静默失效，
// 因此对文本做逐字断言（critique E1）。
func TestOrgGrantMatchSQL(t *testing.T) {
	got := OrgGrantMatchSQL("g", "go", "d")
	want := "((g.include_sub = 1 AND d.path LIKE CONCAT(go.path, '%')) OR (g.include_sub = 0 AND d.id = g.org_id))"
	if got != want {
		t.Fatalf("OrgGrantMatchSQL = %q\nwant %q", got, want)
	}
}

func TestOrgGrantMatchPlanSQL(t *testing.T) {
	got := OrgGrantMatchPlanSQL("g", "o")
	want := "((g.include_sub = 1 AND o.path LIKE CONCAT((SELECT p.path FROM organizations p WHERE p.id = g.org_id), '%')) OR (g.include_sub = 0 AND o.id = g.org_id))"
	if got != want {
		t.Fatalf("OrgGrantMatchPlanSQL = %q\nwant %q", got, want)
	}
}

// TestOrgGrantMatchSQL_NoUserInput 片段中不得出现占位符以外的东西被当作用户输入拼接。
func TestOrgGrantMatchSQL_NoUserInput(t *testing.T) {
	for _, s := range []string{OrgGrantMatchSQL("g", "go", "d"), OrgGrantMatchPlanSQL("g", "o")} {
		if strings.Count(s, "?") != 0 {
			t.Fatalf("判定片段不应包含绑定占位符（参数由调用方追加）: %q", s)
		}
		if strings.Contains(strings.ToLower(s), "select 1") {
			t.Fatalf("判定片段不应包含独立子查询改写: %q", s)
		}
	}
}
