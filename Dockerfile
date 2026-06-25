# ---- Stage 1: Build frontend ----
FROM node:20-alpine AS frontend

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig.json vite.config.ts index.html ./
COPY public/ public/
COPY src/ src/
RUN npm run build

# ---- Stage 2: Build backend ----
# Alpine 切换：~1.3GB → ~400MB 构建期体积，build cache 命中率不变。
# CGO_ENABLED=0 + 现代c Go toolchain，build 出来的二进制是纯静态，runtime 不需要 libc。
FROM golang:1.23-alpine AS backend

WORKDIR /app/backend

# Alpine 自带 git/ca-certificates/tzdata，go mod download 不需要再装。
COPY backend/go.mod backend/go.sum ./
COPY backend/vendor/ vendor/
RUN go mod download

COPY backend/cmd/ cmd/
COPY backend/internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 3: Runtime ----
# 留 debian:bookworm-slim：runtime 需要 ffmpeg/ffprobe/python3（爬虫 + 预览），
# distroless/static 不能跑子进程；alpine runtime 会逼着换 musl 版的 ffmpeg（麻烦且不通用）。
# 瘦身策略：移掉 curl/openssl/tar，openssl 改用 entrypoint 内置 python3 生成。
FROM debian:bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    python3 \
    python3-bs4 \
    python3-lxml \
    python3-requests \
    python3-socks \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# 启动期 sanity check：runtime 必需依赖都装好了
RUN python3 -c "import requests, bs4, lxml, socks" \
    && ffmpeg -version >/dev/null \
    && ffprobe -version >/dev/null

WORKDIR /opt/video-site-61

COPY --from=backend /out/server ./server
COPY --from=frontend /app/dist ./dist
COPY backend/config.example.yaml ./config.example.yaml
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

ARG VERSION=dev

ENV VIDEO_CONFIG=/opt/video-site-61/data/config.yaml \
    VIDEO_FRONTEND_DIR=/opt/video-site-61/dist \
    VIDEO_GITHUB_REPO=iwwater/61video \
    VIDEO_IMAGE_VERSION=${VERSION} \
    VIDEO_LISTEN_PORT=6191 \
    VIDEO_VERSION_FILE=/opt/video-site-61/data/.version

RUN chmod +x ./server /usr/local/bin/docker-entrypoint.sh

VOLUME ["/opt/video-site-61/data"]
EXPOSE 6191

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["./server"]
