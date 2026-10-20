// Package docs 提供管理 API 的接口文档页与 OpenAPI 3.0.3 描述。
//
// 文档产物 internal/api/docs/openapi.json 由 `make docs` 生成并入库，编译期通过
// go:embed 内嵌进二进制；运行时只做两件事：把信息版本替换为构建版本、把运行环境
// 写入 info.description。swagger-ui 的静态资源来自 github.com/swaggo/files，
// 同样内嵌，因此文档页在完全离线的环境下可用（规范 §4.0）。
package docs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	// swaggerFiles 提供内嵌的 swagger-ui 静态资源与 http.FileSystem 适配。
	swaggerFiles "github.com/swaggo/files"
)

// Version 是构建版本，由 `-ldflags "-X github.com/omino/omino-sso/internal/api/docs.Version=v1.2.3"` 注入。
// 语义与 openapi.json 里的接口契约版本（1.0）不同，后者不随构建变化。
var Version string

//go:embed openapi.json
var embeddedSpec []byte

// RoutePrefix 是文档路由的挂载前缀（规范 §4.0）。
const RoutePrefix = "/docs"

// indexHTML 是自写的文档页骨架：只引用同源内嵌资源，不请求任何外部域（FR-020）。
const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>omino-sso 管理 API</title>
  <link rel="stylesheet" type="text/css" href="./swagger-ui.css">
  <link rel="icon" type="image/png" href="./favicon-32x32.png" sizes="32x32">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="./swagger-ui-bundle.js" charset="UTF-8"></script>
  <script src="./swagger-ui-standalone-preset.js" charset="UTF-8"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "./openapi.json",
      dom_id: "#swagger-ui",
      deepLinking: true,
      persistAuthorization: true,
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
      plugins: [SwaggerUIBundle.plugins.DownloadUrl],
      layout: "StandaloneLayout"
    });
  </script>
</body>
</html>
`

var (
	specCache sync.Map // env -> []byte

	staticHandler = http.StripPrefix(RoutePrefix, http.FileServer(swaggerFiles.HTTP))
)

// BuildVersion 返回构建版本：ldflags 注入优先，其次模块/VCS 信息，最终兜底 "dev"。
func BuildVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		var rev string
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				break
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			return rev
		}
	}
	return "dev"
}

// Spec 返回注入了运行环境与构建版本的 OpenAPI 文档字节。
// 同一 env 只计算一次；输出对同一输入逐字节稳定（map 键由 encoding/json 排序）。
func Spec(env string) []byte {
	if v, ok := specCache.Load(env); ok {
		return v.([]byte)
	}
	out := patchSpec(embeddedSpec, env)
	specCache.Store(env, out)
	return out
}

func patchSpec(raw []byte, env string) []byte {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		// 内嵌产物非法属构建期问题，这里不 panic 也不 500，直接返回原始字节并保留可用性。
		return raw
	}
	info, ok := doc["info"].(map[string]any)
	if !ok {
		info = map[string]any{}
		doc["info"] = info
	}
	info["version"] = BuildVersion()
	info["description"] = strings.TrimRight(fmt.Sprint(info["description"]), "\n") +
		"\n\n环境：" + env + " | 服务：omino-sso | 文档端口：8081（仅非 prod 环境提供）"
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return raw
	}
	return append(out, '\n')
}

// Handler 返回挂载在 /docs/*any 上的 gin handler，负责三件事：
//   - /docs/ 与 /docs/index.html → 文档页 HTML
//   - /docs/openapi.json → 注入环境与版本后的 OpenAPI 3.0.3 文档
//   - 其它路径 → 内嵌 swagger-ui 静态资源；不存在时按普通 404 返回
//
// 该 handler 不套统一响应、不读 cookie、不写 cookie（规范 §4.0）。
func Handler(env string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rel := c.Param("any")
		switch rel {
		case "", "/", "/index.html":
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(indexHTML))
		case "/openapi.json":
			c.Data(http.StatusOK, "application/json; charset=utf-8", Spec(env))
		default:
			staticHandler.ServeHTTP(c.Writer, c.Request)
		}
	}
}
