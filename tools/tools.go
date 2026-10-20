//go:build tools

// Package tools pins the build-time CLI so that `go run` resolves it from this
// module's dependency graph — and therefore from tools/go.sum — instead of
// fetching a version at call time. Without this file a plain `go mod tidy`
// would drop swag and `make docs` would have to fall back to `go run ...@version`,
// which requires network access and bypasses the pinned version.
package tools

import _ "github.com/swaggo/swag/cmd/swag"
