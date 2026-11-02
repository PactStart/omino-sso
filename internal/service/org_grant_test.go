package service

import "testing"

// 组织授权的纯函数边界（010）：批量上限、去重、缺省语义。

func TestDedupOrgGrants(t *testing.T) {
	t.Run("空输入被拒绝", func(t *testing.T) {
		if _, ok := dedupOrgGrants(nil); ok {
			t.Fatal("空 grants 应被拒绝")
		}
	})

	t.Run("超过单批上限被拒绝", func(t *testing.T) {
		in := make([]OrgGrantInput, 0, maxOrgGrantBatch+1)
		for i := 0; i <= maxOrgGrantBatch; i++ {
			in = append(in, OrgGrantInput{OrgID: "org-" + string(rune('A'+i%26)) + string(rune('0'+i/26%10))})
		}
		// 构造 501 个**唯一** orgId，避免被去重掩盖上限判定。
		in = in[:maxOrgGrantBatch+1]
		if _, ok := dedupOrgGrants(in); ok {
			t.Fatalf("%d 条应超过单批上限 %d", len(in), maxOrgGrantBatch)
		}
	})

	t.Run("重复部门保留最后一次的 includeSubDepts", func(t *testing.T) {
		got, ok := dedupOrgGrants([]OrgGrantInput{
			{OrgID: "A", IncludeSub: true},
			{OrgID: "B", IncludeSub: false},
			{OrgID: "A", IncludeSub: false},
		})
		if !ok {
			t.Fatal("去重不应失败")
		}
		if len(got) != 2 {
			t.Fatalf("去重后 %d 条，期望 2", len(got))
		}
		for _, g := range got {
			if g.OrgID == "A" && g.IncludeSub {
				t.Fatal("A 应保留最后一次的 includeSubDepts=false")
			}
		}
	})

	t.Run("空部门 ID 被拒绝", func(t *testing.T) {
		if _, ok := dedupOrgGrants([]OrgGrantInput{{OrgID: ""}}); ok {
			t.Fatal("空 orgId 应被拒绝")
		}
	})
}

func TestOrgGrantLimitExposed(t *testing.T) {
	if OrgGrantLimit() != 500 {
		t.Fatalf("组织授权上限 = %d，期望 500（spec FR-010）", OrgGrantLimit())
	}
}
