# BraceSync Makefile — Go monorepo 构建与运维入口
# 对齐：docs/ §6.5 #2

SERVICES := gateway user-service device-service data-service alert-service file-service msg-service
GO_PACKAGES := $(foreach svc,$(SERVICES),./services/$(svc)/...)

DB_URL ?= postgres://bracesync:bracesync@localhost:5432/bracesync?sslmode=disable
MIGRATE_DIR := scripts/db/migrations

.PHONY: build test test-integration lint clean \
        run-gateway run-user-service run-device-service run-data-service \
        run-alert-service run-file-service run-msg-service \
        migrate-up migrate-down seed

## ─── 构建与测试 ───────────────────────────────────────────────

build: ## 编译全部服务
	go build $(GO_PACKAGES)

test: ## 单元测试 + HTTP 层测试
	go test $(GO_PACKAGES)

test-integration: ## 集成测试（testcontainers，需 Docker）
	go test -tags=integration $(GO_PACKAGES)

coverage: ## 生成覆盖率报告
	go test -coverprofile=coverage.out -covermode=atomic $(GO_PACKAGES) && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report → coverage.html"

coverage-check: ## 检查覆盖率门禁（≥70%）
	go test -coverprofile=coverage.out -covermode=atomic $(GO_PACKAGES)
	@TOTAL=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | tr -d '%'); \
	echo "Total coverage: $$TOTAL%"; \
	if [ "$$(echo "$$TOTAL < 70" | bc -l)" = "1" ]; then \
		echo "FAIL: coverage $$TOTAL% below 70% threshold"; exit 1; \
	else \
		echo "PASS: coverage $$TOTAL% >= 70%"; \
	fi

lint: ## 静态分析
	golangci-lint run --timeout=5m --go=1.22 --config=.golangci.yml $(GO_PACKAGES) ./services/testhelper/...

clean: ## 清理构建产物
	rm -rf bin/
	go clean -cache

## ─── 本地运行 ─────────────────────────────────────────────────

run-gateway: ## 启动 API Gateway（:8080）
	go run ./services/gateway/cmd/server

run-user-service: ## 启动 User Service
	go run ./services/user-service/cmd/server

run-device-service: ## 启动 Device Service
	go run ./services/device-service/cmd/server

run-data-service: ## 启动 Data Service
	go run ./services/data-service/cmd/server

run-alert-service: ## 启动 Alert Service
	go run ./services/alert-service/cmd/server

run-file-service: ## 启动 File Service
	go run ./services/file-service/cmd/server

run-msg-service: ## 启动 Msg Service
	go run ./services/msg-service/cmd/server

## ─── 数据库 ───────────────────────────────────────────────────

migrate-up: ## 执行数据库迁移（up）
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" up

migrate-down: ## 回滚数据库迁移（down 1 步）
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" down 1

seed: ## 载入测试种子数据
	psql "$(DB_URL)" -f scripts/db/seed/seed.sql

## ─── 本地开发环境 ─────────────────────────────────────────────

dev-up: ## 启动本地 PG + Redis
	docker compose -f scripts/dev/docker-compose.dev.yml up -d

dev-down: ## 停止本地 PG + Redis
	docker compose -f scripts/dev/docker-compose.dev.yml down

dev-init: ## 一键初始化本地 DB（up + migrate + seed）
	bash scripts/dev/init-db.sh
