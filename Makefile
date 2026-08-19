.PHONY: dev build run docker clean help

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

dev: ## 本地开发（两个终端）：go run . 和 cd web && npm run dev
	@echo "终端1: go run .              → API 在 :8787"
	@echo "终端2: cd web && npm run dev → 页面在 :5173（自动代理 /api）"

build: ## 构建前端并编译出单二进制 ./transfer
	cd web && npm install && npm run build
	go build -trimpath -ldflags="-s -w" -o transfer .
	@echo "✅ 完成：./transfer"

run: ## 本地运行（密码 dev123）
	ACCESS_PASSWORD=dev123 ./transfer

docker: ## Docker 构建并后台启动
	docker compose up -d --build

clean: ## 清理构建产物
	rm -f transfer transfer-bin
