# syntax=docker/dockerfile:1

# ==============================================================================
# Stage 1: Build embedded React Web UI (web/dist) on native BUILDPLATFORM
# ==============================================================================
FROM --platform=$BUILDPLATFORM node:24-alpine AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY web/ ./
RUN npm run build

# ==============================================================================
# Stage 2: Cross-compile pure-Go Ambra binary natively on BUILDPLATFORM (CGO_ENABLED=0)
# ==============================================================================
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS go-builder

ARG TARGETOS
ARG TARGETARCH
ARG AMBRA_VERSION=v0.12.0

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=web-builder /src/web/dist ./web/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w -X github.com/ambra-media/ambra/internal/config.AmbraVersion=${AMBRA_VERSION}" -o /out/ambra ./cmd/ambra

# ==============================================================================
# Stage 3A: Ultra-lightweight runtime WITHOUT FFmpeg/FFprobe (target: runtime-lite)
# Ideal for STRM/302 direct-play or external/mounted FFmpeg setups (~35MB)
# ==============================================================================
FROM alpine:3.21 AS runtime-lite

ENV TZ=Asia/Shanghai \
    AMBRA_HTTP_ADDR=:8096

RUN apk add --no-cache ca-certificates curl setpriv tzdata \
    && addgroup -g 1000 -S ambra \
    && adduser -u 1000 -S -G ambra -h /app -s /sbin/nologin ambra \
    && mkdir -p /app /data \
    && chown -R ambra:ambra /app /data

WORKDIR /app
COPY --from=go-builder /out/ambra /usr/local/bin/ambra
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh

EXPOSE 8096/tcp 1900/udp
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD port="${AMBRA_HTTP_ADDR##*:}"; case "$port" in ''|*[!0-9]*) port=8096 ;; esac; curl -fsS "http://127.0.0.1:${port}/System/Ping" || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/ambra", "-data", "/data"]

# ==============================================================================
# Stage 3B: Full runtime WITH bundled FFmpeg/FFprobe (target: runtime-full, default)
# Ready out-of-the-box for ffprobe deep scan, HLS transcode, DVR & /dev/dri VAAPI
# ==============================================================================
FROM debian:bookworm-slim AS runtime-full

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Asia/Shanghai \
    AMBRA_HTTP_ADDR=:8096 \
    AMBRA_FFMPEG_PATH=/usr/bin/ffmpeg \
    AMBRA_FFPROBE_PATH=/usr/bin/ffprobe

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    ffmpeg \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 1000 -r ambra \
    && useradd -u 1000 -r -g ambra -d /app -s /usr/sbin/nologin ambra \
    && mkdir -p /app /data \
    && chown -R ambra:ambra /app /data

WORKDIR /app
COPY --from=go-builder /out/ambra /usr/local/bin/ambra
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh

EXPOSE 8096/tcp 1900/udp
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD port="${AMBRA_HTTP_ADDR##*:}"; case "$port" in ''|*[!0-9]*) port=8096 ;; esac; curl -fsS "http://127.0.0.1:${port}/System/Ping" || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/ambra", "-data", "/data"]
