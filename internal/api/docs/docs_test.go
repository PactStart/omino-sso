package docs

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
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

// TestGlobalFailurePathsDocumented 覆盖 FR-009：不属于单个端点的通用失败路径
// （未匹配路由的 404、全局限流的 429）必须记录在全局说明里，且给出真实响应体样例。
func TestGlobalFailurePathsDocumented(t *testing.T) {
	desc := infoDescription(t, Spec("failure-paths-case"))
	for _, want := range []string{
		"404",
		"NOT_FOUND",
		"429",
		"RATE_LIMITED",
		`"data":null`,
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("info.description 未记录通用失败路径，缺少 %q\n---\n%s", want, desc)
		}
	}
	// 泛化描述（"资源不存在"）之外必须给出可复制的 JSON 样例。
	if !strings.Contains(desc, `{"code":"NOT_FOUND"`) || !strings.Contains(desc, `{"code":"RATE_LIMITED"`) {
		t.Fatalf("info.description 缺少 404/429 的响应体样例:\n%s", desc)
	}
}

// codeTokenPattern 匹配注解里作为业务码书写的 token：全大写 + 数字/下划线，长度 >= 9
// （最短的登记码为 FORBIDDEN，9 个字符），从而排除 "MFA" / "TOTP" 这类缩写。
var codeTokenPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{8,}$`)

// TestAnnotationErrorCodesAreRegistered 覆盖 AC-5 与 INV-11：注解中出现的业务码必须全部
// 登记在 internal/api/response/errors.go，禁止 handler 侧自造 code。
// 注解把业务码写在响应的 description 中（多个码用 " / " 分隔），故按 token 解析。
func TestAnnotationErrorCodesAreRegistered(t *testing.T) {
	registered := registeredErrorCodes(t)

	var doc struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Description string `json:"description"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(embeddedSpec, &doc); err != nil {
		t.Fatalf("内嵌 openapi.json 解析失败: %v", err)
	}

	seen := make(map[string]struct{})
	for path, item := range doc.Paths {
		for method, op := range item {
			for status, resp := range op.Responses {
				for _, part := range strings.Split(resp.Description, "/") {
					part = strings.TrimSpace(part)
					if !codeTokenPattern.MatchString(part) {
						continue
					}
					if _, ok := registered[part]; !ok {
						t.Errorf("%s %s 的 %s 响应声明了未登记的业务码 %q（AC-5 / INV-11）",
							strings.ToUpper(method), path, status, part)
					}
					seen[part] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("未从注解中提取到任何业务码：AC-5 断言已失效，请检查错误码是否仍写在响应 description 中")
	}
}

// registeredErrorCodes 解析 response/errors.go 中 &Error{<status>, "<code>", "<msg>"} 形式的
// 字面量，返回已登记的业务码集合（以源码为唯一真源，避免测试自带一份副本）。
func registeredErrorCodes(t *testing.T) map[string]struct{} {
	t.Helper()
	const src = "../response/errors.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", src, err)
	}

	out := make(map[string]struct{})
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || len(lit.Elts) < 2 {
			return true
		}
		basic, ok := lit.Elts[1].(*ast.BasicLit)
		if !ok || basic.Kind != token.STRING {
			return true
		}
		code, err := strconv.Unquote(basic.Value)
		if err != nil {
			return true
		}
		out[code] = struct{}{}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("%s 中未解析到任何业务码，登记项提取失效", src)
	}
	return out
}

func infoDescription(t *testing.T, raw []byte) string {
	t.Helper()
	var doc struct {
		Info struct {
			Description string `json:"description"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	return doc.Info.Description
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
		// servers 固定为 "/"，因此 path 必须自带 /api/v1 前缀，否则 Try it out 会 404。
		if !strings.HasPrefix(path, "/api/v1/") {
			t.Errorf("path %q 缺少 /api/v1 前缀（basePath 未折入）", path)
		}
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
			if path == "/api/v1/auth/login" || path == "/api/v1/auth/mfa/verify" {
				if len(op.Security) != 0 {
					t.Errorf("%s %s 是公开端点，不应声明 security", method, path)
				}
			} else if len(op.Security) == 0 {
				t.Errorf("%s %s 缺少 security 声明", method, path)
			}
		}
	}
	if ops != 36 {
		t.Fatalf("operation 数为 %d，规范要求 36", ops)
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
