GO ?= go
BIN := bin/sso

.PHONY: help deps keys dev build test lint build-web migrate-up migrate-down clean

help:
	@echo "deps        下载依赖"
	@echo "keys        生成开发用 RSA 签名密钥"
	@echo "dev         启动服务（:8080 协议层 / :8081 管理 API）"
	@echo "build       编译到 $(BIN)"
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

build:
	@mkdir -p bin
	$(GO) build -o $(BIN) ./cmd/sso

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
