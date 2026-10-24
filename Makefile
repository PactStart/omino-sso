GO ?= go
BIN := bin/sso

# 构建版本注入 internal/api/docs.Version：优先 tag，回退提交哈希，最终 dev。
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DOCS_VERSION_PKG := github.com/omino/omino-sso/internal/api/docs
SWAGGER_JSON := .tmp/swagger/swagger.json
OPENAPI_JSON := internal/api/docs/openapi.json

# ---------- 容器镜像（007）：三个镜像同一 tag 成套发布 ----------
# 用法：make images DOMAIN=example.com IMAGE_TAG=2026.10.23-1
# 前端绝对地址由 DOMAIN 派生，不必手抄 build-arg；确需覆盖时显式传 ADMIN_HOME=... 等。
REGISTRY ?= registry.example.com
IMAGE_TAG ?=
DOMAIN ?=
# 另一环境的主域，仅 image-check（串环境反证）需要
FOREIGN_DOMAIN ?=
SITE_NAME ?= 统一身份认证
ADMIN_HOME ?= $(if $(DOMAIN),https://admin.$(DOMAIN)/)
PORTAL_HOME ?= $(if $(DOMAIN),https://portal.$(DOMAIN)/account/profile)
PORTAL_LOGIN_URL ?= $(if $(DOMAIN),https://portal.$(DOMAIN)/login)
BACKEND_IMAGE := $(REGISTRY)/omino-sso:$(IMAGE_TAG)
PORTAL_IMAGE := $(REGISTRY)/omino-portal:$(IMAGE_TAG)
ADMIN_IMAGE := $(REGISTRY)/omino-admin:$(IMAGE_TAG)

.PHONY: help deps keys assets dev build test test-go test-web test-integration lint lint-go lint-web check check-registry check-docs-commands check-artifact-refs check-build-env verify-fast verify-full hooks-install ci-local build-web images image-backend image-portal image-admin image-push image-check nginx-check e2e docs docs-check docs-serve migrate-up migrate-down clean

help:
	@echo "deps        下载依赖 + 安装 git 钩子（pre-push）"
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
	@echo "images      构建三个容器镜像（同 tag）= image-backend + image-portal + image-admin"
	@echo "            用法：make images DOMAIN=<主域> IMAGE_TAG=<版本>-<序号>"
	@echo "image-backend  只构建后端镜像（只需 IMAGE_TAG）"
	@echo "image-portal   只构建门户镜像（需 DOMAIN + IMAGE_TAG）"
	@echo "image-admin    只构建后台镜像（需 DOMAIN + IMAGE_TAG）"
	@echo "image-check 镜像自检：nginx -t / 无 default.conf / 入口 nginx 语法 / 串环境检查（需 DOMAIN + FOREIGN_DOMAIN）"
	@echo "nginx-check 入口 nginx（api/sso 域）配置语法自检（临时自签证书，不需要镜像）"
	@echo "image-push  推送三个镜像到 \$$REGISTRY"
	@echo "e2e         本地 test 形态端到端验证（四域 HTTPS；需 docker + bin/sso + 两个 dist）"
	@echo "check       一致性门禁：注册表 + 文档命令 + 制品引用 + 串环境"
	@echo "verify-fast 提交边界门禁（lint + check + 单测），pre-commit 用"
	@echo "verify-full 交付边界门禁（build + test + lint + check），pre-push / PR 用"
	@echo "hooks-install 安装 git 钩子（pre-push -> verify-full）"

# 一步到位的上手：拉依赖 + 装 push 边界钩子（漏装会让 full 档门禁形同不存在，
# 而 `.git/hooks` 不入库，克隆后必须重装一次）。
deps: hooks-install
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

test: test-go test-web

test-go:
	$(GO) test ./...

# 集成测试（需要 MySQL）：会 DROP DATABASE 并重建 schema，
# 因此 TEST_MYSQL_DSN 的库名必须以 _test 结尾（护栏见 internal/service/org_integration_test.go）。
# 另带两个 api 包：internal/api/me 的会话失效用例需要真实 MySQL + Redis（TEST_REDIS_ADDR），
# 锁的是「改密清 cookie / 验证码重置不清 cookie」这条 handler 接线（007 T065）。
test-integration:
	@test -n "$$TEST_MYSQL_DSN" || { echo "请先导出 TEST_MYSQL_DSN（库名必须以 _test 结尾，例如 .../omino_sso_test）"; exit 1; }
	TEST_MYSQL_DSN="$$TEST_MYSQL_DSN" TEST_REDIS_ADDR="$$TEST_REDIS_ADDR" $(GO) test -p 1 \
		./internal/service/ ./internal/store/ ./internal/api/auth/ ./internal/api/me/ -count=1

# 前端用例走 vitest：portal（登录方式切换、图形验证码、验证码登录失败提示）
# + admin（菜单 index 无 /admin 冗余段、应用详情 tab、服务不可用页回跳）。
# node_modules 不入库：干净检出时先按 $(NPM_INSTALL) 安装，已装过则直接跑，保证新克隆可直接 `make test`。
test-web:
	@test -d web/portal/node_modules || (cd web/portal && $(NPM_INSTALL))
	cd web/portal && npm run test
	@test -d web/admin/node_modules || (cd web/admin && $(NPM_INSTALL))
	cd web/admin && npm run test

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

# 与 test-web 同理：node_modules 不入库，干净检出（CI）时两套前端都要先安装，
# 否则 `eslint: command not found` 会让 `make lint` 以 127 退出（`make test` 只装了 portal）。
lint-web:
	@test -d web/portal/node_modules || (cd web/portal && $(NPM_INSTALL))
	cd web/portal && npm run lint
	@test -d web/admin/node_modules || (cd web/admin && $(NPM_INSTALL))
	cd web/admin && npm run lint

# 复盘（2026-10-22）：注册表与文档命令都是确定性检查，前移到提交前，
# 避免再出现「代码改了、AGENTS/契约没跟着改」与「文档教用户跑不存在的目标」。
# 两者都只读仓库文件，不访问网络、不依赖 node_modules。
check: check-registry check-docs-commands check-artifact-refs check-build-env

check-registry:
	bash scripts/check-registry.sh

check-docs-commands:
	bash scripts/check-docs-commands.sh

# 制品引用（2026-10-24）：AGENTS §3 目录树里的路径、tasks.md 里 `DoD:` 行与被引用的
# 测试/脚本路径必须真实存在。007 的 4 轮 converge 里 3–4 条 finding 是「任务声称写了
# 某个测试，文件不存在」，属确定性检查，前移到提交前（详见 scripts/check-artifact-refs.sh）。
check-artifact-refs:
	bash scripts/check-artifact-refs.sh

# 门禁分档（2026-10-24 复盘）：
#   fast —— 提交边界。lint + 一致性 + 单测，约 30s（实测 lint 10s / check 5s / test 14s），
#           不含 make build（docs 生成与漂移检查留给 push），让 WIP 提交不被文档类问题卡住。
#   full —— 交付边界。pre-push 与 PR 前跑全套，与宪法「交付检查」口径一致。
# 注意：fast 档不是"弱化门禁"，`make build` 覆盖的 docs-check 由 pre-push 兜底；
# 该扩展的 golden rule（git 边界通过 ⇒ CI 通过）在本仓库由 `make ci-local` 独立保证。
verify-fast: lint check test

verify-full: build test lint check

# .git/hooks 不入库，克隆后需重装（`make deps` 会带上）；pre-commit 由 .specify/gates 的 stub
# 负责，这里只接管它不管理的 pre-push。非 git 检出（发布包/worktree 拷贝）直接跳过。
hooks-install:
	@git rev-parse --git-dir >/dev/null 2>&1 || { echo "hooks-install: skip（非 git 检出）"; exit 0; }
	@mkdir -p .git/hooks
	@install -m 0755 scripts/git-hooks/pre-push .git/hooks/pre-push
	@echo "hooks-install: .git/hooks/pre-push -> make verify-full"

# 串环境检查（007）：需要 PORTAL_IMAGE/ADMIN_IMAGE 才真正执行，未提供时打印 skip 并以 0 退出，
# 保证无 docker 的开发机上 `make check` 仍然可跑。用法见 DEPLOYMENT.md §5。
check-build-env:
	bash scripts/check-build-env.sh

# 模拟干净检出（CI）：临时 git worktree + 无 node_modules / 无 .env / 无未跟踪文件。
# 用于发现「本地绿、CI 红」类问题（005 的 lint-web 事故即此类）。
ci-local:
	bash scripts/ci-local.sh

# 本地 test 形态端到端验证：*.lvh.me 四域 + 自签 TLS + nginx 容器，
# 另起 :9080/:9081 实例（REDIS_DB=1）验证跨子域 SSO / cookie / 探针暴露面 / 可信代理，
# 全程不碰 dev 实例。依赖：docker + 已构建的 $(BIN) 与两个前端 dist（脚本内会校验并提示）。
# 注意：会临时改演示账号密码与 portal 回调，退出时自动还原；DB_HOST 非本机时拒绝执行。
e2e:
	bash scripts/e2e/run.sh

# 两个前端产物 -> web/dist/portal、web/dist/admin
# NPM_INSTALL 可覆盖：流水线保持 `npm ci`（锁文件一致性），本机 npm 的批量删除保护
# 可能让 `npm ci` 以 SAFE_DELETE_BULK_CONFIRM_REQUIRED 中止，此时用 `make build-web NPM_INSTALL=npm install`。
#
# WEB_MODE 按环境构建（同一份代码 + 不同 VITE_*），模板见 web/{portal,admin}/.env.*.example：
#   留空        -> vite 默认 mode=production，加载 .env.production（prod）
#   WEB_MODE=test -> 加载 .env.test（test）
NPM_INSTALL ?= npm ci
WEB_MODE ?=
VITE_MODE_FLAG = $(if $(WEB_MODE),-- --mode $(WEB_MODE),)
build-web:
	cd web/portal && $(NPM_INSTALL) && npm run build $(VITE_MODE_FLAG)
	cd web/admin && $(NPM_INSTALL) && npm run build $(VITE_MODE_FLAG)
	@mkdir -p web/dist
	@rm -rf web/dist/portal web/dist/admin
	@cp -r web/portal/dist web/dist/portal
	@cp -r web/admin/dist web/dist/admin
	@echo "web/dist ready: portal + admin"

# ---------------------------------------------------------------------------
# 容器镜像（007）：三镜像同 tag，成套构建 / 推送 / 自检
#
#   make images DOMAIN=example.com IMAGE_TAG=2026.10.23-1
#   make image-check DOMAIN=example.com FOREIGN_DOMAIN=example-test.com IMAGE_TAG=2026.10.23-1
#   make image-push  REGISTRY=registry.example.com IMAGE_TAG=2026.10.23-1
#
# tag 规范 <版本或日期>-<序号>；回滚时整体改回旧 tag（三镜像必须同 tag）。
# ---------------------------------------------------------------------------
define require_image_tag
	@test -n "$(IMAGE_TAG)" || { \
		echo "缺少 IMAGE_TAG（形如 2026.10.23-1；三镜像必须同 tag）"; exit 1; }
endef

define require_domain
	@test -n "$(DOMAIN)" || { \
		echo "缺少 DOMAIN=<主域>（前端构建期需要绝对地址；也可显式传 ADMIN_HOME/PORTAL_HOME/PORTAL_LOGIN_URL）"; exit 1; }
endef

images: image-backend image-portal image-admin
	@echo "三镜像就绪（tag=$(IMAGE_TAG)）："
	@echo "  $(BACKEND_IMAGE)"
	@echo "  $(PORTAL_IMAGE)"
	@echo "  $(ADMIN_IMAGE)"

image-backend:
	$(require_image_tag)
	docker build -t $(BACKEND_IMAGE) .

image-portal:
	$(require_image_tag)
	$(require_domain)
	docker build -f web/Dockerfile --target portal \
		--build-arg VITE_ADMIN_HOME=$(ADMIN_HOME) \
		--build-arg VITE_PORTAL_HOME=$(PORTAL_HOME) \
		--build-arg VITE_PORTAL_LOGIN_URL=$(PORTAL_LOGIN_URL) \
		--build-arg VITE_SITE_NAME="$(SITE_NAME)" \
		-t $(PORTAL_IMAGE) ./web

image-admin:
	$(require_image_tag)
	$(require_domain)
	docker build -f web/Dockerfile --target admin \
		--build-arg VITE_PORTAL_HOME=$(PORTAL_HOME) \
		--build-arg VITE_PORTAL_LOGIN_URL=$(PORTAL_LOGIN_URL) \
		--build-arg VITE_SITE_NAME="$(SITE_NAME)" \
		-t $(ADMIN_IMAGE) ./web

image-push:
	$(require_image_tag)
	docker push $(BACKEND_IMAGE)
	docker push $(PORTAL_IMAGE)
	docker push $(ADMIN_IMAGE)

# 入口 nginx（api/sso 域，不进镜像）的语法自检：临时自签证书 + 本地解析 sso 主机名。
# 这份配置只在宿主机 nginx 上生效，写错了要等上线才暴露，所以单独兜一道 nginx -t。
nginx-check:
	@command -v openssl >/dev/null || { echo "nginx-check 需要 openssl 生成临时证书"; exit 1; }
	@mkdir -p .tmp/nginx-check/tls
	@openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=nginx-check" \
		-keyout .tmp/nginx-check/tls/sso.key -out .tmp/nginx-check/tls/sso.crt >/dev/null 2>&1
	docker run --rm --add-host sso:127.0.0.1 \
		-v "$(CURDIR)/web/nginx/api.conf:/etc/nginx/conf.d/default.conf:ro" \
		-v "$(CURDIR)/.tmp/nginx-check/tls:/etc/nginx/tls:ro" \
		nginx:1.27-alpine nginx -t
	@rm -rf .tmp/nginx-check

# 只读自检：配置可解析、镜像内无官方 default.conf（它会抢走 80 端口）、产物不串环境
image-check:
	$(require_image_tag)
	$(require_domain)
	@test -n "$(FOREIGN_DOMAIN)" || { \
		echo "缺少 FOREIGN_DOMAIN（另一环境主域，用于串环境反证）"; exit 1; }
	docker run --rm $(PORTAL_IMAGE) nginx -t
	docker run --rm $(ADMIN_IMAGE) nginx -t
	@docker run --rm --entrypoint sh $(PORTAL_IMAGE) -c 'ls /etc/nginx/conf.d' | grep -q default.conf && { \
		echo "镜像内残留 /etc/nginx/conf.d/default.conf（80 端口默认 server，会导致全站 404）"; exit 1; } || true
	@$(MAKE) --no-print-directory nginx-check
	PORTAL_IMAGE=$(PORTAL_IMAGE) ADMIN_IMAGE=$(ADMIN_IMAGE) \
	EXPECT_DOMAIN=$(DOMAIN) FOREIGN_DOMAIN=$(FOREIGN_DOMAIN) \
	EXPECT_SITE_NAME="$(SITE_NAME)" \
		bash scripts/check-build-env.sh

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
