package docs

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildVersionFallback 覆盖 T007 的版本回退链：ldflags 注入 > 模块/VCS > dev。
func TestBuildVersionFallback(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "v2.3.4"
	if got := BuildVersion(); got != "v2.3.4" {
		t.Fatalf("ldflags 注入未生效: got %q", got)
	}

	// 未注入时不得返回空串；go test 下通常落到 VCS 短哈希或 dev。
	Version = ""
	if got := BuildVersion(); got == "" {
		t.Fatal("未注入版本时回退结果为空，应至少为 dev")
	}
}

// TestSpecInjectsEnvAndVersion 覆盖 CT-3：info.description 含 APP_ENV，info.version 为构建版本。
func TestSpecInjectsEnvAndVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v9.9.9-docs-test"

	raw := Spec("env-injection-case")
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("注入后的文档不是合法 JSON: %v", err)
	}
	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatal("缺少 info 对象")
	}
	if got := info["version"]; got != "v9.9.9-docs-test" {
		t.Fatalf("info.version = %v, 期望注入值", got)
	}
	desc, _ := info["description"].(string)
	if !strings.Contains(desc, "环境：env-injection-case") {
		t.Fatalf("info.description 未包含运行环境: %q", desc)
	}
}

// TestSpecIsDeterministic 覆盖 INV-2 的运行时一半：同一 env 多次取用逐字节一致。
func TestSpecIsDeterministic(t *testing.T) {
	first := Spec("determinism-case")
	second := Spec("determinism-case")
	if string(first) != string(second) {
		t.Fatal("同一环境的两次输出不一致")
	}
	third := patchSpec(embeddedSpec, "determinism-case")
	if string(first) != string(third) {
		t.Fatal("缓存输出与重新计算结果不一致")
	}
}

// TestIndexHTMLStaysOffline 覆盖 FR-020：文档页只引用同源内嵌资源。
func TestIndexHTMLStaysOffline(t *testing.T) {
	if !strings.Contains(indexHTML, `url: "./openapi.json"`) {
		t.Fatal("index.html 未指向同源 openapi.json")
	}
	for _, bad := range []string{"http://", "https://", "//unpkg", "//cdn"} {
		if strings.Contains(indexHTML, bad) {
			t.Fatalf("index.html 引用了外部资源 %q", bad)
		}
	}
	for _, asset := range []string{"./swagger-ui.css", "./swagger-ui-bundle.js", "./swagger-ui-standalone-preset.js"} {
		if !strings.Contains(indexHTML, asset) {
			t.Fatalf("index.html 缺少内嵌资源引用 %q", asset)
		}
	}
}

// TestEmbeddedSpecStructure 覆盖 T016 的文档结构断言（AC-1 的路由一致性在 router_test.go）。
func TestEmbeddedSpecStructure(t *testing.T) {
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]struct {
			Summary   string             `json:"summary"`
			Tags      []string           `json:"tags"`
			Security  []map[string][]any `json:"security"`
			Responses map[string]any     `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas         map[string]any `json:"schemas"`
			SecuritySchemes map[string]struct {
				Type string `json:"type"`
				In   string `json:"in"`
				Name string `json:"name"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(embeddedSpec, &doc); err != nil {
		t.Fatalf("内嵌 openapi.json 解析失败: %v", err)
	}

	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("openapi = %q, 期望 3.0.3", doc.OpenAPI)
	}
	if doc.Info.Version != "1.0" {
		t.Fatalf("入库产物的接口契约版本应为 1.0，实际 %q（运行版本由运行时注入，不应写入产物）", doc.Info.Version)
	}

	ops := 0
	for path, item := range doc.Paths {
		for method, op := range item {
			ops++
			if strings.TrimSpace(op.Summary) == "" {
				t.Errorf("%s %s 缺少 summary", method, path)
			}
			if len(op.Tags) == 0 {
				t.Errorf("%s %s 缺少 tags", method, path)
			}
			has2xx, has500 := false, false
			for code := range op.Responses {
				if strings.HasPrefix(code, "2") {
					has2xx = true
				}
				if code == "500" {
					has500 = true
				}
			}
			if !has2xx {
				t.Errorf("%s %s 缺少 2xx 响应", method, path)
			}
			if !has500 {
				t.Errorf("%s %s 缺少 500 响应", method, path)
			}
			// 公开端点不标 security，其余必须标。
			if path == "/auth/login" || path == "/auth/mfa/verify" {
				if len(op.Security) != 0 {
					t.Errorf("%s %s 是公开端点，不应声明 security", method, path)
				}
			} else if len(op.Security) == 0 {
				t.Errorf("%s %s 缺少 security 声明", method, path)
			}
		}
	}
	if ops != 16 {
		t.Fatalf("operation 数为 %d，规范要求 16", ops)
	}

	scheme, ok := doc.Components.SecuritySchemes["cookieAuth"]
	if !ok {
		t.Fatal("缺少 components.securitySchemes.cookieAuth（INV-12）")
	}
	if scheme.Type != "apiKey" || scheme.In != "cookie" || scheme.Name != "sid" {
		t.Fatalf("cookieAuth 定义错误: %+v", scheme)
	}

	// INV-8：不得为单个接口另造信封结构。
	for name := range doc.Components.Schemas {
		if strings.HasSuffix(name, "Response") || strings.HasSuffix(name, "Resp") {
			t.Errorf("components.schemas 出现逐接口信封 %q（INV-8）", name)
		}
	}

	// INV-9：示例中的密钥类字段必须是占位串。
	if strings.Contains(string(embeddedSpec), `"clientSecret": "`) &&
		!strings.Contains(string(embeddedSpec), `"clientSecret": "cs_placeholder"`) {
		t.Error("clientSecret 出现非占位示例值（INV-9）")
	}
}
