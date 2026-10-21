package admin

import (
	"encoding/json"
	"testing"
)

func strPtr(s string) *string { return &s }

// TestOptionalStringThreeStates 覆盖 parentId 的三态语义：
// 字段缺失（不变）、显式 null（移到根级）、字符串值（移到该节点下）。
func TestOptionalStringThreeStates(t *testing.T) {
	cases := []struct {
		name string
		body string
		set  bool
		val  *string
	}{
		{"字段缺失", `{}`, false, nil},
		{"显式 null", `{"parentId":null}`, true, nil},
		{"字符串值", `{"parentId":"01J9ZJ7A1B2C3D4E5F6G7H8J9K"}`, true, strPtr("01J9ZJ7A1B2C3D4E5F6G7H8J9K")},
		{"空串", `{"parentId":""}`, true, strPtr("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req UpdateOrgRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("绑定失败: %v", err)
			}
			if req.ParentID.Set != tc.set {
				t.Fatalf("Set = %v, 期望 %v", req.ParentID.Set, tc.set)
			}
			switch {
			case tc.val == nil && req.ParentID.Value != nil:
				t.Fatalf("Value = %q, 期望 nil", *req.ParentID.Value)
			case tc.val != nil && req.ParentID.Value == nil:
				t.Fatalf("Value = nil, 期望 %q", *tc.val)
			case tc.val != nil && *req.ParentID.Value != *tc.val:
				t.Fatalf("Value = %q, 期望 %q", *req.ParentID.Value, *tc.val)
			}
		})
	}
}

// TestUpdateOrgRequestEmptyBody 覆盖空请求体语义：name/sort/parentId 均未提交，
// service 侧据此不执行任何 UPDATE（幂等）。
func TestUpdateOrgRequestEmptyBody(t *testing.T) {
	var req UpdateOrgRequest
	if err := json.Unmarshal([]byte(`{"parentId":null,"sort":null}`), &req); err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	if req.Name != nil {
		t.Errorf("name 应为 nil, 实际 %q", *req.Name)
	}
	if req.Sort != nil {
		t.Errorf("sort 应为 nil, 实际 %d", *req.Sort)
	}
	if !req.ParentID.Set || req.ParentID.Value != nil {
		t.Errorf("parentId 应为显式 null（Set=true, Value=nil）, 实际 %+v", req.ParentID)
	}
}

// TestUpdateOrgRequestInvalidParentType 覆盖类型错误必须绑定失败（→ 400，不写审计）。
func TestUpdateOrgRequestInvalidParentType(t *testing.T) {
	var req UpdateOrgRequest
	if err := json.Unmarshal([]byte(`{"parentId":123}`), &req); err == nil {
		t.Fatal("parentId 为数字时应绑定失败")
	}
}

// TestUnknownFieldsIgnored 覆盖 tenantId 不属于契约：提交后被忽略且不影响绑定。
func TestUnknownFieldsIgnored(t *testing.T) {
	var req CreateOrgRequest
	if err := json.Unmarshal([]byte(`{"name":"平台组","tenantId":"other"}`), &req); err != nil {
		t.Fatalf("未知字段不应导致绑定失败: %v", err)
	}
	if req.Name != "平台组" {
		t.Fatalf("name = %q", req.Name)
	}
	if req.ParentID != nil {
		t.Fatalf("parentId 应为 nil（根级）")
	}
	if req.Sort != nil {
		t.Fatalf("sort 缺省应为 nil，由 service 落 0")
	}
}
