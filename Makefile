# ScopeSentry 开发者入口
# 用法：make help
SHELL := /bin/bash
GO ?= go
PNPM ?= pnpm
BINARY ?= dist/ScopeSentry

# 不依赖 Mongo/Redis 的包（其余包在 init 阶段就会连库）
HERMETIC_PKGS := \
	./internal/services/task/tasksync/ \
	./internal/services/task/taskstate/ \
	./internal/services/task/tasksnapshot/ \
	./internal/services/task/taskmsg/ \
	./internal/services/task/audit/ \
	./internal/api/routes/audit/ \
	./internal/repositories/plugin/ \
	./internal/services/project/ \
	./internal/api/handlers/sensitiveRule/ \
	./internal/api/middleware/compress/ \
	./internal/utils/redislist/ \
	./internal/services/export/exportfilter/ \
	./internal/utils/tagpalette/ \
	./internal/utils/mongouri/ \
	./internal/constants/

.PHONY: help build binary binary-linux run test-hermetic test-integration vet fmt fmt-check mod-check assetpack-check assetpack-zip \
	frontend-install frontend-build frontend-test frontend-lint frontend-check compose-check ci clean

help: ## 列出所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## 编译全部 Go 包（不产出二进制）
	$(GO) build ./...

binary: ## 产出服务器二进制到 dist/ScopeSentry
	mkdir -p dist
	$(GO) build -o $(BINARY) ./cmd/main

binary-linux: ## 产出 linux/amd64 二进制（内嵌前端，保留 /map 微前端资源）
	./scripts/build-linux.sh

run: ## 本地启动服务（需先配置 Mongo/Redis 环境变量）
	$(GO) run ./cmd/main

test-hermetic: ## 跑不依赖 Mongo/Redis 的单元测试
	$(GO) test -count=1 $(HERMETIC_PKGS)

test-integration: ## 全量测试（需要可用的 Mongo/Redis，读 MONGODB_*/REDIS_* 环境变量）
	$(GO) test -tags integration -count=1 ./...

vet: ## 全仓 go vet（CI 同款阻塞闸）
	$(GO) vet ./...

fmt: ## 格式化所有 Go 代码
	gofmt -w .

fmt-check: ## 检查是否有未格式化的 Go 文件
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then \
		echo "以下文件未格式化（运行 make fmt）:"; echo "$$out"; exit 1; fi

mod-check: ## 检查 go.mod / go.sum 是否与代码一致
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum 有漂移，请提交 tidy 结果"; exit 1)

frontend-install: ## 安装前端依赖
	cd frontend && $(PNPM) install --no-frozen-lockfile

frontend-build: ## 前端生产构建（dist-pro）
	cd frontend && $(PNPM) build:pro

frontend-test: ## 前端单元测试（vitest）
	cd frontend && $(PNPM) test

frontend-lint: ## 前端 ESLint（与 CI 完全同一口径，避免格式问题只在 CI 才暴露）
	cd frontend && npx eslint --ext .js,.ts,.vue ./src --max-warnings 0

frontend-check: frontend-lint frontend-build frontend-test ## 前端自检：ESLint + 生产构建 + 语言包/主题自检 + 单测
	cd frontend && node scripts/check-locales.mjs && npx esno ./scripts/check-theme.ts

# 与 .github/workflows/ci.yml 的三道阻塞闸对齐：Go build/vet/单测 + 前端 ESLint/构建/自检/单测
compose-check: ## 校验单机部署 compose（含国内镜像覆盖）与 deploy.sh 语法
	bash -n deploy.sh
	docker compose -f single-host-deployment.yml config -q
	docker compose -f single-host-deployment.yml -f single-host-deployment.cn.yml config -q

ci: fmt-check mod-check build vet test-hermetic frontend-check ## 本地复现 CI 的全部阻塞门禁

assetpack-check: ## 校验内嵌资产包：清单/sha256/行数 + nuclei 模板（不落盘，CI 可跑）
	go test ./internal/constants/ -count=1
	nuclei -validate -t internal/constants/assets/poc/ -duc=false

assetpack-zip: ## 打包 POC 模板为 dist/poc-pack.zip（供「POC 管理」手工导入）
	python3 tools/assetpack/package.py

clean: ## 清理构建产物
	rm -rf dist frontend/dist-pro

preflight: ## 离线预检：build / vet / 单测 / UI 审计 / 前端构建（不需要生产）
	bash scripts/preflight.sh --with-frontend

health: ## 生产自检：SSH / 容器 / healthz / 磁盘 / 节点
	bash scripts/healthcheck.sh

recover: ## 主机恢复后一键收尾（自检 -> 部署 -> scan 限载重建 -> 复检）
	bash scripts/recover.sh
