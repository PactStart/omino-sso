package dto

import (
	"encoding/json"
	"testing"
)

// 成员列表（010）的 JSON 契约固化，避免「不该出现的 null / 该出现的空数组」被悄悄改坏：
//   - `visibleTotal` 只在 page=1 有值，其余页必须显式是 null（前端据此显示「—」）；
//   - `orgGrantHints` 只在 page=1 出现，其余页省略；
//   - 成员无组织授权时 `orgGrants` 必须是 []，不能是 null。
func TestMemberPageJSONShape(t *testing.T) {
	visible := int64(40)

	t.Run("page=1 带可见总人数与迁移提示", func(t *testing.T) {
		raw, err := json.Marshal(MemberPage{
			List:          []AppMemberItem{{UserID: "u1", OrgGrants: make([]OrgGrantRef, 0)}},
			Total:         1,
			Page:          1,
			PageSize:      20,
			VisibleTotal:  &visible,
			OrgGrantHints: []OrgGrantHint{{OrgID: "d1", DeptName: "研发部", MemberCount: 118}},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["visibleTotal"] != float64(40) {
			t.Fatalf("visibleTotal = %v，期望 40", got["visibleTotal"])
		}
		if _, ok := got["orgGrantHints"]; !ok {
			t.Fatal("page=1 必须返回 orgGrantHints")
		}
	})

	t.Run("page=2 显式返回 visibleTotal=null 且省略 orgGrantHints", func(t *testing.T) {
		raw, err := json.Marshal(MemberPage{
			List:     []AppMemberItem{},
			Total:    1,
			Page:     2,
			PageSize: 20,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		value, ok := got["visibleTotal"]
		if !ok {
			t.Fatal("page=2 必须保留 visibleTotal 字段（值为 null），不能整个省略")
		}
		if value != nil {
			t.Fatalf("page=2 visibleTotal = %v，期望 null", value)
		}
		if _, ok := got["orgGrantHints"]; ok {
			t.Fatal("page=2 不应返回 orgGrantHints")
		}
	})

	t.Run("无组织授权的成员序列化为空数组", func(t *testing.T) {
		raw, err := json.Marshal(AppMemberItem{UserID: "u1", OrgGrants: make([]OrgGrantRef, 0)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		arr, ok := got["orgGrants"].([]any)
		if !ok {
			t.Fatalf("orgGrants = %v，期望数组而非 null", got["orgGrants"])
		}
		if len(arr) != 0 {
			t.Fatalf("orgGrants 长度 = %d，期望 0", len(arr))
		}
	})
}
