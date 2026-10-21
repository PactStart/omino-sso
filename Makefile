GO ?= go
BIN := bin/sso

# 构建版本注入 internal/api/docs.Version：优先 tag，回退提交哈希，最终 dev。
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DOCS_VERSION_PKG := github.com/omino/omino-sso/internal/api/docs
SWAGGER_JSON := .tmp/swagger/swagger.json
OPENAPI_JSON := internal/api/docs/openapi.json

.PHONY: help deps keys assets dev build test lint lint-go lint-web build-web docs docs-check docs-serve migrate-up migrate-down clean

help:
	@echo "deps        下载依赖"
	@echo "assets      校验运行时资源（邮件验证码模板）存在"
	@echo "keys        生成开发用 RSA 签名密钥"
	@echo "dev         启动服务（:8080 协议层 / :8081 管理 API）"
	@echo "build       漂移检查 + 编译到 $(BIN)"
	@echo "docs        从 handler 注解重新生成 $(OPENAPI_JSON)"
	@echo "docs-check  校验已提交文档与注解是否一致（不一致则非零退出）"
	@echo "docs-serve  生成文档并启动服务，回显文档地址"
	@echo "migrate-up  执行数据库迁移"
	@echo "test        单元测试（集成测试需 TEST_MYSQL_DSN / TEST_REDIS_ADDR）"
	@echo "lint        Go + 两个前端的 lint 门禁"
	@echo "build-web   构建两个前端产物到 web/dist"

deps:
	$(GO) mod download

keys:
	@mkdir -p configs
	@test -f configs/dev-signing-key.pem || openssl genrsa -out configs/dev-signing-key.pem 2048
	@chmod 600 configs/dev-signing-key.pem
	@echo "signing key ready: configs/dev-signing-key.pem"

# 运行时资源（005）：邮件验证码模板随 configs/ 交付，路径由 MAIL_TEMPLATE_PATH 指定；
# 缺失时启动后首次发信才失败，这里提前到构建/启动前暴露。
assets:
	@test -f configs/verify-mail.tmpl || { \
		echo "缺少运行时资源：configs/verify-mail.tmpl（MAIL_TEMPLATE_PATH 默认指向它）"; \
		exit 1; \
	}

dev: assets
	$(GO) run ./cmd/sso

build: docs-check assets
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

# lint 是宪法要求的交付检查（「开发工作流与质量门禁」第 1 条），必须能真正失败：
# 不允许 `|| true` 吞掉问题，也不允许工具缺失时静默通过。
GOLANGCI ?= golangci-lint
# 前后端聚合门禁：任一失败即非零退出
lint: lint-go lint-web

lint-go:
	@command -v $(GOLANGCI) >/dev/null 2>&1 || { \
		echo "golangci-lint 未安装，无法执行 lint 门禁。安装："; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	}
	$(GOLANGCI) run ./...

lint-web:
	cd web/portal && npm run lint
	cd web/admin && npm run lint

# 两个前端产物 -> web/dist/portal、web/dist/admin
# NPM_INSTALL 可覆盖：流水线保持 `npm ci`（锁文件一致性），本机 npm 的批量删除保护
# 可能让 `npm ci` 以 SAFE_DELETE_BULK_CONFIRM_REQUIRED 中止，此时用 `make build-web NPM_INSTALL=npm install`。
NPM_INSTALL ?= npm ci
build-web:
	cd web/portal && $(NPM_INSTALL) && npm run build
	cd web/admin && $(NPM_INSTALL) && npm run build
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

# 按文件名升序执行全部 *.up.sql（0001_init → 0002_clients_meta → 0003_app_auth），
# 不再硬编码单个文件：新增迁移只加文件，不改 Makefile（quickstart §0）。
MIGRATIONS_DIR ?= migrations

migrate-up:
	@for f in $$(ls $(MIGRATIONS_DIR)/*.up.sql | sort); do \
		echo "applying $$f"; \
		mysql -h $(DB_HOST) -P $(DB_PORT) -u $(DB_USER) -p$(DB_PASSWORD) --default-character-set=utf8mb4 $(DB_NAME) < $$f || exit 1; \
	done

# 逆序回退（先 0003 再 0002 再 0001），避免外键/列依赖顺序问题
migrate-down:
	@for f in $$(ls $(MIGRATIONS_DIR)/*.down.sql | sort -r); do \
		echo "reverting $$f"; \
		mysql -h $(DB_HOST) -P $(DB_PORT) -u $(DB_USER) -p$(DB_PASSWORD) --default-character-set=utf8mb4 $(DB_NAME) < $$f || exit 1; \
	done

clean:
	rm -rf bin
