GO ?= go
BIN := bin/sso

# 构建版本注入 internal/api/docs.Version：优先 tag，回退提交哈希，最终 dev。
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DOCS_VERSION_PKG := github.com/omino/omino-sso/internal/api/docs
SWAGGER_JSON := .tmp/swagger/swagger.json
OPENAPI_JSON := internal/api/docs/openapi.json

.PHONY: help deps keys dev build test lint build-web docs docs-check docs-serve migrate-up migrate-down clean

help:
	@echo "deps        下载依赖"
	@echo "keys        生成开发用 RSA 签名密钥"
	@echo "dev         启动服务（:8080 协议层 / :8081 管理 API）"
	@echo "build       漂移检查 + 编译到 $(BIN)"
	@echo "docs        从 handler 注解重新生成 $(OPENAPI_JSON)"
	@echo "docs-check  校验已提交文档与注解是否一致（不一致则非零退出）"
	@echo "docs-serve  生成文档并启动服务，回显文档地址"
	@echo "migrate-up  执行数据库迁移"
	@echo "test        单元测试"
	@echo "build-web   构建两个前端产物到 web/dist"

deps:
	$(GO) mod download

keys:
	@mkdir -p configs
	@test -f configs/dev-signing-key.pem || openssl genrsa -out configs/dev-signing-key.pem 2048
	@chmod 600 configs/dev-signing-key.pem
	@echo "signing key ready: configs/dev-signing-key.pem"

dev:
	$(GO) run ./cmd/sso

build: docs-check
	@mkdir -p bin
	$(GO) build -ldflags "-X $(DOCS_VERSION_PKG).Version=$(VERSION)" -o $(BIN) ./cmd/sso

# swag 只产出 Swagger 2.0，转换到 OpenAPI 3.0.3 的步骤不可省略（规范 §2.4）。
# 两条命令都在 tools/ 内执行，CLI 版本由 tools/go.sum 锁定，禁止写 @version。
docs:
	@mkdir -p .tmp/swagger
	cd tools && $(GO) run github.com/swaggo/swag/cmd/swag init -g cmd/sso/main.go -d .. -o ../.tmp/swagger --outputTypes json
	cd tools && $(GO) run ./openapi3 -in ../$(SWAGGER_JSON) -out ../$(OPENAPI_JSON)
	@echo "docs: $(OPENAPI_JSON)"

# 生成到 .tmp/swagger-check，再与入库产物逐字段比对；不覆盖工作区已提交文件。
docs-check:
	@mkdir -p .tmp/swagger-check
	cd tools && $(GO) run github.com/swaggo/swag/cmd/swag init -g cmd/sso/main.go -d .. -o ../.tmp/swagger-check --outputTypes json >/dev/null
	cd tools && $(GO) run ./openapi3 -in ../.tmp/swagger-check/swagger.json -check -baseline ../$(OPENAPI_JSON)

docs-serve: docs
	@echo "文档入口: http://127.0.0.1:8081/docs/ （APP_ENV=prod 时不提供）"
	$(GO) run ./cmd/sso

test:
	$(GO) test ./...

lint:
	golangci-lint run ./... || true

# 两个前端产物 -> web/dist/portal、web/dist/admin
build-web:
	cd web/portal && npm ci && npm run build
	cd web/admin && npm ci && npm run build
	@mkdir -p web/dist
	@rm -rf web/dist/portal web/dist/admin
	@cp -r web/portal/dist web/dist/portal
	@cp -r web/admin/dist web/dist/admin
	@echo "web/dist ready: portal + admin"

DB_HOST ?= 127.0.0.1
DB_PORT ?= 3307
DB_USER ?= root
DB_PASSWORD ?= omino_dev_pwd
DB_NAME ?= omino_sso

migrate-up:
	mysql -h $(DB_HOST) -P $(DB_PORT) -u $(DB_USER) -p$(DB_PASSWORD) --default-character-set=utf8mb4 $(DB_NAME) < migrations/0001_init.up.sql

migrate-down:
	mysql -h $(DB_HOST) -P $(DB_PORT) -u $(DB_USER) -p$(DB_PASSWORD) $(DB_NAME) < migrations/0001_init.down.sql

clean:
	rm -rf bin
