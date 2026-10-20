package dto

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// 本文件锁定"具名化不改变响应语义"这一契约（AC-7）。
// 判据是语义等价而非逐字节相等：gin.H 是 map，encoding/json 按键字典序输出；
// 结构体按字段声明序输出，键序天然不同，对 JSON 消费方无影响。

func canonical(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func keySet(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertEquivalent(t *testing.T, legacy map[string]any, named any) {
	t.Helper()
	got := canonical(t, named)
	want := canonical(t, legacy)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload changed\n legacy(gin.H): %#v\n named(%T):   %#v", want, named, got)
	}
	if !reflect.DeepEqual(keySet(got), keySet(want)) {
		t.Fatalf("key set changed\n legacy: %v\n named:  %v", keySet(want), keySet(got))
	}
}

func TestLoginMfaChallengeMatchesLegacyPayload(t *testing.T) {
	assertEquivalent(t,
		map[string]any{"mfaRequired": true, "mfaTicket": "mft_5c1e9a7b3d2f", "expiresIn": 300},
		LoginMfaChallenge{MFARequired: true, MFATicket: "mft_5c1e9a7b3d2f", ExpiresIn: 300},
	)
}

func TestLoginResultMatchesLegacyPayload(t *testing.T) {
	brief := UserBrief{ID: "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A", Username: "zhangsan", DisplayName: "张三"}
	assertEquivalent(t,
		map[string]any{
			"mfaRequired": false,
			"mfaTicket":   nil,
			"expiresIn":   0,
			"redirectTo":  "/account",
			"user":        brief,
		},
		LoginResult{MFARequired: false, MFATicket: nil, ExpiresIn: 0, RedirectTo: "/account", User: brief},
	)
}

func TestMfaResultMatchesLegacyPayload(t *testing.T) {
	brief := UserBrief{ID: "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A", Username: "zhangsan", DisplayName: "张三"}
	assertEquivalent(t,
		map[string]any{"redirectTo": "/account", "user": brief},
		MfaResult{RedirectTo: "/account", User: brief},
	)
}

func TestAppItemMatchesLegacyPayload(t *testing.T) {
	last := "2026-10-20T09:00:00Z"
	assertEquivalent(t,
		map[string]any{
			"clientId":     "portal",
			"name":         "统一门户",
			"description":  "员工自助门户",
			"logoUrl":      "https://cdn.example.com/logo/portal.png",
			"homeUrl":      "https://portal.example.com",
			"lastAccessAt": &last,
		},
		AppItem{
			ClientID:     "portal",
			Name:         "统一门户",
			Description:  "员工自助门户",
			LogoURL:      "https://cdn.example.com/logo/portal.png",
			HomeURL:      "https://portal.example.com",
			LastAccessAt: &last,
		},
	)
}

func TestSessionItemMatchesLegacyPayload(t *testing.T) {
	ua := "Mozilla/5.0"
	assertEquivalent(t,
		map[string]any{
			"sid":        "sess_01J9ZK3A7B5C7D9E1F3G5H7J9K",
			"ip":         "203.0.113.10",
			"userAgent":  &ua,
			"createdAt":  "2026-10-20T09:00:00Z",
			"lastSeenAt": "2026-10-20T10:00:00Z",
			"current":    true,
		},
		SessionItem{
			SID:        "sess_01J9ZK3A7B5C7D9E1F3G5H7J9K",
			IP:         "203.0.113.10",
			UserAgent:  &ua,
			CreatedAt:  "2026-10-20T09:00:00Z",
			LastSeenAt: "2026-10-20T10:00:00Z",
			Current:    true,
		},
	)
}

func TestCreatedClientMatchesLegacyPayload(t *testing.T) {
	secret := "cs_9f2b8c1e4d7a"
	assertEquivalent(t,
		map[string]any{"clientId": "portal", "clientSecret": &secret},
		CreatedClient{ClientID: "portal", ClientSecret: &secret},
	)

	// 公开客户端：密钥为 nil，键仍必须存在。
	assertEquivalent(t,
		map[string]any{"clientId": "spa", "clientSecret": nil},
		CreatedClient{ClientID: "spa", ClientSecret: nil},
	)
}

func TestRotatedSecretMatchesLegacyPayload(t *testing.T) {
	assertEquivalent(t,
		map[string]any{"clientId": "portal", "clientSecret": "cs_new", "expiresAt": nil},
		RotatedSecret{ClientID: "portal", ClientSecret: "cs_new", ExpiresAt: nil},
	)
}

func TestAuditItemMatchesLegacyPayload(t *testing.T) {
	uid := "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A"
	uname := "zhangsan"
	cid := "portal"
	ua := "Mozilla/5.0"
	detail := `{"reason":"ok"}`

	assertEquivalent(t,
		map[string]any{
			"id":        "01J9ZK5B9C1D3E5F7G9H1J3K5M",
			"eventType": "login_success",
			"userId":    &uid,
			"username":  &uname,
			"clientId":  &cid,
			"ip":        "203.0.113.10",
			"userAgent": &ua,
			"result":    "success",
			"detail":    &detail,
			"createdAt": "2026-10-20T10:12:33Z",
		},
		AuditItem{
			ID:        "01J9ZK5B9C1D3E5F7G9H1J3K5M",
			EventType: "login_success",
			UserID:    &uid,
			Username:  &uname,
			ClientID:  &cid,
			IP:        "203.0.113.10",
			UserAgent: &ua,
			Result:    "success",
			Detail:    &detail,
			CreatedAt: "2026-10-20T10:12:33Z",
		},
	)
}
