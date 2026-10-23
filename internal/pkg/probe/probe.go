// Package probe 实现运维探针（/healthz、/readyz）的共享响应逻辑。
//
// 探针属运维面而非业务接口，因此：
//   - 不套统一响应 {code,message,data,traceId}（规范 §4.0 / 宪法「技术与接口约束」）；
//   - 不进 swagger（openapi.json 必须零漂移）；
//   - 由调用方保证不参与限流（见 internal/api/middleware/ratelimit.go）。
//
// 两个 HTTP server（:8080 协议层、:8081 管理 API）共用本包，避免两份逻辑各自演化。
package probe

import (
	"encoding/json"
	"net/http"
)

// StatusOK / StatusReady / StatusUnready 是对外契约里的取值，改动即破坏探针契约。
const (
	StatusOK      = "ok"
	StatusReady   = "ready"
	StatusUnready = "unready"
)

// WriteHealth 输出存活结果（不探测任何依赖）。
func WriteHealth(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"status": StatusOK})
}

// WriteReady 输出就绪结果；任一依赖失败即 503，并在 failing 中列出依赖名，
// 不回传错误原文（避免泄露 DSN / 内网地址，宪法 IV）。
func WriteReady(w http.ResponseWriter, mysqlErr, redisErr error) {
	failing := Failing(mysqlErr, redisErr)
	if len(failing) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  StatusUnready,
			"failing": failing,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": StatusReady})
}

// Failing 返回失败的依赖名（只可能是 mysql / redis）。
func Failing(mysqlErr, redisErr error) []string {
	failing := make([]string, 0, 2)
	if mysqlErr != nil {
		failing = append(failing, "mysql")
	}
	if redisErr != nil {
		failing = append(failing, "redis")
	}
	if len(failing) == 0 {
		return nil
	}
	return failing
}

// IsGet 判断方法是否允许；探针只接受 GET。
func IsGet(r *http.Request) bool { return r.Method == http.MethodGet }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 防中间层缓存 200 掩盖故障。
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
