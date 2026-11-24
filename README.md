<p align="center">
  <img src="assets/ambra-logo.svg" alt="Ambra Logo" width="110" />
</p>

<h1 align="center">Ambra Media Server</h1>

<p align="center">
  基于 <strong>Go + SQLite</strong> 从零构建、面向 <strong>Emby Server 4.10.2.0</strong> 全量协议兼容的轻量级现代媒体服务器
</p>

<p align="center">
  <a href="https://github.com/Iky002/Ambra/actions/workflows/ci.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Iky002/Ambra/actions/workflows/release.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/release.yml/badge.svg" alt="Release" /></a>
  <a href="https://iky002.github.io/Ambra/"><img src="https://img.shields.io/badge/Live%20Demo-GitHub%20Pages-f59e0b" alt="Live Demo" /></a>
  <img src="https://img.shields.io/badge/Emby%204.10.2.0%20API-583%2F583%20(100%25)-10b981" alt="583/583 APIs" />
  <img src="https://img.shields.io/badge/Go%20Coverage-94.8%25-3b82f6" alt="Go Coverage" />
</p>

---

## 核心特性

- **零依赖单文件 / 单容器部署**：纯 Go（`CGO_ENABLED=0`）结合内嵌 SQLite（WAL 模式 + 读写分离连接池）与 `go:embed` 现代 React Web 客户端，单二进制仅 ~20 MB，极低内存与 CPU 占用。
- **Emby 4.10.2.0 全量兼容（583 / 583 路由）**：100% 通过 Emby `4.10.2.0` 全量路由自动化契约测试，完整支持 `/emby` 与根路径双前缀、335 个静态路径段大小写不敏感、PascalCase JSON、GUID 归一化与 WebSocket 实时事件推送。
- **原生跨目录多版本自动合并**：不同目录下的电影或剧集只要拥有相同 `TMDB ID` / `IMDB ID` 即可自动合并为同一条目的多版本 `MediaSources`（支持无 TMDB ID 同目录命名合并与手动 `MergeVersions` / 拆分）。
- **原生中文体验增强**：内置 SQLite FTS5 中文分词与多关键字乱序模糊搜索；`ffprobe` 深度探测 + 外挂字幕扫描智能区分简体/繁体/双语中文字幕（`CHS`/`CHT`/`SC`/`TC`）。
- **五级智能播放与零转码客户端**：STRM 302 直链 → 原生 DirectPlay → **Web 端 JASSUB（ASS/SSA 特效）+ Blu-ray PGS Canvas 渲染 + `ffmpeg.wasm` 轻量解复用** → 服务端 DirectStream → 动态 HLS 硬解转码（支持 Apple VideoToolbox / NVIDIA NVENC / Intel QSV / Linux VAAPI / Windows AMD AMF）。
- **现代内嵌 Web 客户端与完整管理控制台**：内置暗色影院界面、6 大管理控制台面板（实时会话、用户与 RBAC 权限管理、媒体库与本地目录浏览、设备与 API Key、插件配置、计划任务与日志）以及 **Serverless Demo 模式**（零后端即可在线体验）。
- **统一插件内核与全媒体子系统**：内置官方插件（NFO / TMDB / STRM）与外部扩展（`wazero` 纯 Go `.wasm` 沙箱 + HTTP 进程外插件）共用同一契约；覆盖合集、播放列表、音乐与 LRC 歌词、Live TV M3U/DVR 真实录播、DLNA SSDP/UPnP、离线 Sync、Webhooks 与 SQLite 热备份。

---

## Serverless 在线体验（无需后端）

Ambra Web 客户端内置 **Serverless Demo Mode**（真实 TMDB 海报与剧照、离线演示视频与字幕、全内存可写管理控制台与一键切换管理员/普通用户视角）：

- **GitHub Pages**：推送后开启 `Settings → Pages → Source: GitHub Actions`，自动部署至 `https://iky002.github.io/Ambra/`
- **Vercel**：导入仓库后将 Root Directory 设为 `web`（已内置 `web/vercel.json`）
- **Cloudflare Pages**：Root directory `web`，Build command `npm run build:demo`，Output directory `dist-demo`

---

## 快速启动

### Docker 镜像与 Release 双版本选择（Full vs Lite）

| 版本 | Docker 镜像标签（GHCR） | GitHub Release 产物 | 说明 |
| :--- | :--- | :--- | :--- |
| **Full 完整版（默认）** | `ghcr.io/iky002/ambra:latest` / `:full` | `ambra-full-<os>-<arch>.(tar.gz\|zip)` | **内置 `ffmpeg` + `ffprobe`**，开箱即用，支持 `ffprobe` 深度探测、HLS 硬解/软解转码与 Live TV 录播 |
| **Lite 轻量版** | `ghcr.io/iky002/ambra:lite` | `ambra-lite-<os>-<arch>(.exe)` | **不内置 FFmpeg**（二进制 ~20MB、Alpine 镜像 ~35MB），适合 STRM 302 直链或自定义 `jellyfin-ffmpeg` |

> 如需自行编译**单文件硬内嵌 FFmpeg** 二进制，可将目标平台 `ffmpeg`/`ffprobe` 放入 `internal/core/ffmpegbin/bin/` 后使用 `-tags embed_ffmpeg` 构建。

### 方式一：Docker Compose（推荐）

```bash
docker compose up -d
```

启动后访问：`http://localhost:8096/web/`（默认管理员账号：`admin` / `admin`）

### 方式二：Docker 命令行

```bash
# Full 版（内置 FFmpeg）
docker run -d \
  --name ambra \
  -p 8096:8096 \
  -p 1900:1900/udp \
  -v $(pwd)/data:/data \
  -v /path/to/media:/media:ro \
  ghcr.io/iky002/ambra:latest
```

### 方式三：源码本地构建

```bash
make web          # 构建内嵌 Web UI (web/dist)
make build        # 构建当前平台纯 Go 二进制 (bin/ambra)
./bin/ambra -data ./data -addr :8096
```

交叉编译全平台发布包：

```bash
make release-all  # 输出 linux/amd64, linux/arm64, darwin/arm64, windows/amd64 + SHA256SUMS
```

---

## 环境变量与 FFmpeg 探测优先级

| 环境变量 | 说明 | 默认值 |
| :--- | :--- | :--- |
| `AMBRA_HTTP_ADDR` | HTTP 监听地址 | `:8096` |
| `AMBRA_WEB_DIR` | 外部 Web UI 目录（留空使用内嵌前端） | `""` |
| `AMBRA_TMDB_API_KEY` | TMDB API Key（元数据与海报刮削） | `""` |
| `AMBRA_FFMPEG_PATH` | 手动指定 `ffmpeg` 路径 | 自动探测 |
| `AMBRA_FFPROBE_PATH` | 手动指定 `ffprobe` 路径 | 自动探测 |

**探测优先级**：`AMBRA_FFMPEG_PATH` 环境变量 → 配置文件自定义路径 → `-tags embed_ffmpeg` 内嵌释放 → 同级目录 `./ffmpeg(.exe)` → `<DataDir>/bin/` → 系统 `PATH`。

---

## Git Flow 开发工作流

本项目遵循标准 **Git Flow** 分支模型进行协作与迭代：

| 分支 | 用途 | 规则 |
| :--- | :--- | :--- |
| `main` | 生产稳定发布分支 | 仅接受 `release/*` 与 `hotfix/*` 合并；每次发版打 `vX.Y.Z` Tag 自动触发 Release 与 GHCR 推送 |
| `develop` | 日常开发集成分支 | 所有新功能合并至此分支，自动触发完整 CI（Web 测试 + Go ≥90% 覆盖率 + 583 API 契约 + Docker 校验） |
| `feature/<name>` | 新功能开发分支 | 从 `develop` 切出，完成后通过 PR 合并回 `develop` |
| `release/vX.Y.Z` | 发布预备分支 | 从 `develop` 切出，仅做发布前修复与版本号更新，完成后合入 `main`（打 Tag）并回合并到 `develop` |
| `hotfix/<name>` | 线上紧急修复分支 | 从 `main` 切出，修复后同时合入 `main`（打补丁 Tag）与 `develop` |

---

## 文档导航

完整四层设计文档位于 [docs/README.md](docs/README.md)：
- **架构设计**：[系统总体架构](docs/architecture/01-system-overview.md) · [Emby API 兼容层](docs/architecture/02-api-compatibility-layer.md) · [SQLite 数据架构](docs/architecture/03-data-and-storage.md) · [媒体处理管线](docs/architecture/04-media-pipeline.md) · [统一插件内核](docs/architecture/05-extensibility.md)
- **核心规格**：[数据库与中文检索](docs/specs/01-database-schema-and-query.md) · [认证与实时通信](docs/specs/02-auth-sessions-and-realtime.md) · [播放与动态转码](docs/specs/03-playback-transcode-and-subtitles.md) · [全媒体与 Live TV / DLNA](docs/specs/04-all-media-types-and-subsystems.md) · [契约测试与基准](docs/specs/05-testing-and-compatibility.md) · [现代 Web 客户端](docs/specs/06-web-client-and-player.md)
- **路线与协议**：[分阶段开发计划](docs/roadmap/01-development-roadmap.md) · [582 条 API 兼容矩阵](docs/roadmap/02-api-coverage-matrix.md) · [Emby 4.10.2.0 完整 API 参考](docs/emby-4.10.2.0-api/README.md)
