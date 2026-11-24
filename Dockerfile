# syntax=docker/dockerfile:1

# ==============================================================================
# Stage 1: Build embedded React Web UI (web/dist)
# ==============================================================================
FROM node:24-alpine AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ==============================================================================
# Stage 2: Build pure-Go Ambra binary (CGO_ENABLED=0)
# ==============================================================================
FROM golang:1.26-bookworm AS go-builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG AMBRA_VERSION=v0.5.0

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=web-builder /src/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X github.com/ambra-media/ambra/internal/config.AmbraVersion=${AMBRA_VERSION}" -o /out/ambra ./cmd/ambra

# ==============================================================================
# Stage 3A: Ultra-lightweight runtime WITHOUT FFmpeg/FFprobe (target: runtime-lite)
# Ideal for STRM/302 direct-play or external/mounted FFmpeg setups (~35MB)
# ==============================================================================
FROM alpine:3.21 AS runtime-lite

ENV TZ=Asia/Shanghai \
    AMBRA_HTTP_ADDR=:8096

RUN apk add --no-cache ca-certificates curl tzdata \
    && mkdir -p /data

COPY --from=go-builder /out/ambra /usr/local/bin/ambra

EXPOSE 8096/tcp 1900/udp
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8096/System/Ping || exit 1

ENTRYPOINT ["/usr/local/bin/ambra"]
CMD ["-data", "/data"]

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
    && mkdir -p /data

COPY --from=go-builder /out/ambra /usr/local/bin/ambra

EXPOSE 8096/tcp 1900/udp
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8096/System/Ping || exit 1

ENTRYPOINT ["/usr/local/bin/ambra"]
CMD ["-data", "/data"]
