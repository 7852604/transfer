# ---- 前端构建 ----
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --registry=https://registry.npmmirror.com
COPY web/ ./
# vite outDir 指向 ../internal/webui/dist，即输出到 /src/internal/webui/dist
RUN npm run build

# ---- Go 构建 ----
FROM golang:1.25-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/transfer .

# ---- 运行 ----
FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/transfer /app/transfer
ENV DATA_DIR=/app/data PORT=8787
VOLUME /app/data
EXPOSE 8787
ENTRYPOINT ["/app/transfer"]
