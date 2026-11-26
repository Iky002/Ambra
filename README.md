<p align="center">
  <img src="assets/ambra-logo.svg" alt="Ambra Logo" width="110" />
</p>

<h1 align="center">Ambra Media Server</h1>

<p align="center">
  轻量、极速、开箱即用的现代个人影院服务器 · 基于 <strong>Go + SQLite</strong>，100% 兼容 <strong>Emby Server 4.10.2.0</strong> 生态
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="README.en.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/Iky002/Ambra/releases/latest"><img src="https://img.shields.io/github/v/release/Iky002/Ambra?label=Latest&color=f59e0b" alt="Latest Release" /></a>
  <a href="https://github.com/Iky002/Ambra/actions/workflows/ci.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Iky002/Ambra/actions/workflows/release.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/release.yml/badge.svg" alt="Release" /></a>
  <a href="https://iky002.github.io/Ambra/"><img src="https://img.shields.io/badge/Live%20Demo-在线体验-10b981" alt="Live Demo" /></a>
  <img src="https://img.shields.io/badge/Emby%204.10.2.0%20API-583%2F583%20(100%25)-0ea5e9" alt="583/583 APIs" />
  <img src="https://img.shields.io/badge/Go%20Coverage-%E2%89%A590%25-6366f1" alt="Go Coverage" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-94a3b8" alt="MIT License" /></a>
</p>

---

## 为什么选择 Ambra？

- **极轻部署，几乎零负担**：纯 Go 单文件 + 内嵌 SQLite + 内嵌现代 Web 客户端，单容器 / 单二进制即可运行，空载内存低、NAS 与轻量云服务器都能轻松驾驭。
- **全面兼容 Emby 客户端生态**：100% 通过 Emby `4.10.2.0` 全部 **583 条 API** 契约测试，直接适配 **Infuse、VidHub、SenPlayer、Yamby、Hills、Emby 官方客户端、Kodi** 以及 **MoviePilot / Sonarr / Radarr**。
- **内置 10 国语言国际化（i18n）**：前后端原生支持 **简体中文、繁體中文、English、日本語、한국어、Español、Deutsch、Français、Português、ไทย**，自动识别浏览器语言并支持顶栏/个人中心即时切换。
- **为中文影音库深度优化**：
  - 支持 **中文分词、拼音全拼/首字母、繁简互通与多关键字乱序搜索**；
  - 媒体库 **A–Z 索引原生支持中文拼音首字母**；
  - 智能识别中文字幕与音轨标签，清晰区分 **简体 / 繁体 / 双语**（`CHS` / `CHT` / `SC` / `TC`）；
  - 参考 MoviePilot 优化中英文及年份歧义文件名识别（如《海上钢琴师 1900 (1998)》《银翼杀手2049 (2017)》），并支持内联 `{tmdb-*}` 与本地 `.nfo` / 海报刮削。
- **更现代的内置 Web 影院与零转码体验**：
  - 首页聚合 **轮播推荐、继续观看、接下来播放 (Next Up)、热门趋势（TMDB / 豆瓣 / IMDb）与各库最新添加**；
  - 支持 **跨目录多版本自动合并**、**TMDB 自动合集 (BoxSet)**、片单队列连播、音乐播放器与 LRC 歌词；
  - 浏览器端原生支持 **ASS/SSA 特效字幕（JASSUB）与蓝光 PGS 图形字幕渲染**、**STRM 302 直链自动降级**、MKV 流式解复用与硬解 HLS 转码（VideoToolbox / NVENC / QSV / VAAPI / AMF）。
- **完整家庭与进阶能力**：支持多用户与家长控制（分级/标签屏蔽）、Quick Connect 快捷登录、Live TV（M3U + XMLTV EPG + DVR 录播）、DLNA 局域网投屏、通知中心、Webhooks、计划任务与 SQLite 一键热备份还原。

---

## 在线体验（无需安装）

无需部署后端，点击即可直接体验完整暗色影院界面与可交互管理控制台（支持一键切换管理员 / 普通用户视角）：

- 👉 **[立即打开 Ambra Live Demo](https://iky002.github.io/Ambra/)**

> 也支持一键自托管 Demo 版：Vercel（Root Directory 设为 `web`）或 Cloudflare Pages（Root Directory `web`，Build Command `npm run build:demo`，Output Directory `dist-demo`）。

---

## 快速开始

### 1. 选择适合的版本（Full vs Lite）

| 版本 | Docker 镜像（GHCR） | GitHub Release 产物 | 适用场景 |
| :--- | :--- | :--- | :--- |
| **Full 完整版（推荐）** | `ghcr.io/iky002/ambra:latest` / `:full` | `ambra-full-<os>-<arch>.(tar.gz\|zip)` | **内置 `ffmpeg` + `ffprobe`**，开箱即用，支持深度探测、HLS 硬解/软解转码与 Live TV 录播 |
| **Lite 轻量版** | `ghcr.io/iky002/ambra:lite` | `ambra-lite-<os>-<arch>(.exe)` | **不内置 FFmpeg**（镜像 ~35 MB），适合以 STRM 302 直链为主或自备 FFmpeg 的环境 |

### 2. Docker Compose 部署（推荐）

将仓库中的 [docker-compose.yml](docker-compose.yml) 下载到本地（按需取消注释媒体目录挂载、硬件加速设备，并按宿主机目录属主调整 `PUID` / `PGID`），执行：

```bash
docker compose up -d
```

### 3. Docker 命令行部署

```bash
docker run -d \
  --name ambra \
  --restart unless-stopped \
  -p 8096:8096 \
  -p 1900:1900/udp \
  -e TZ=Asia/Shanghai \
  -e PUID=1000 \
  -e PGID=1000 \
  -v $(pwd)/data:/data \
  -v /path/to/media:/media:ro \
  ghcr.io/iky002/ambra:latest
```

> **📁 NAS / 宿主机映射目录权限说明（重要）**
> - 容器默认以 **`PUID=1000` / `PGID=1000`** 非 root 身份运行，并在启动时自动修正 `/data` 目录权限（也兼容官方 Emby 的 `UID` / `GID` 环境变量名）。
> - 如遇到容器内无法访问宿主机/NAS 映射的媒体目录（`Permission denied`），请先在宿主机执行 `stat -c '%u:%g' /path/to/media` 或 `id` 查看目录属主，并将 **`PUID` / `PGID`** 改为对应的 UID/GID（例如群晖常见 `1026:100`、Unraid 常见 `99:100`）；需要访问多个用户组时可通过 **`GIDLIST`** 传入逗号分隔的附加组 ID（如 `-e GIDLIST=100,44`）。
> - **混合权限目录**：如果你同时挂载了多个不同属主的媒体文件夹，请通过环境变量设置 **`PUID=0` 与 `PGID=0`**（或 **`UID=0` 与 `GID=0`**）以 `0:0` root 模式运行即可访问所有映射目录。注意：Docker 的 `--user` / Compose `user:` 仅适合指定非 root UID/GID，不要用 `--user 0:0` 替代 `PUID=0 / PGID=0`（因为入口脚本在未设置 `PUID=0 / PGID=0` 时会自动降权到 `1000:1000`）。

### 4. 访问与初始化

启动后在浏览器打开 **`http://<你的IP>:8096/web/`**：
1. 首次启动会自动进入 **`/setup` 初始化向导**，引导创建首个管理员账号与密码（v0.10.5+ 不再预设弱口令 `admin / admin`；如需无人值守自动初始化，可在首次启动前设置环境变量 `AMBRA_ADMIN_PASS` 及可选的 `AMBRA_ADMIN_USER`）；
2. 在 **管理控制台 → 媒体库** 中添加本地影视/剧集/音乐或 `.strm` 目录；
3. 在 **插件 / 系统设置** 中填入 `TMDB API Key` 即可自动刮削中文海报、简介、演职员与合集。

### 5. 从源码构建（可选）

```bash
make web          # 构建内嵌 Web 客户端 (web/dist)
make build        # 构建当前平台纯 Go 二进制 (bin/ambra)
./bin/ambra -data ./data -addr :8096
```

---

## 常用环境变量

| 环境变量 | 说明 | 默认值 |
| :--- | :--- | :--- |
| `TZ` | 容器时区（影响日志与计划任务时间） | `Asia/Shanghai` |
| `PUID` / `PGID` | Docker 运行用户 UID / GID（兼容 `UID` / `GID`；混合权限目录可设为 `0` / `0`） | `1000` / `1000` |
| `GIDLIST` | Docker 附加用户组 ID 或组名列表，逗号分隔（如 `100,44,109`） | `""` |
| `UMASK` | Docker 新建文件默认权限掩码（如 `002`、`022`） | 继承系统 |
| `AMBRA_HTTP_ADDR` | HTTP 监听地址 | `:8096` |
| `AMBRA_ADMIN_USER` | 首次启动无人值守初始化管理员用户名（需配合 `AMBRA_ADMIN_PASS`） | `admin` |
| `AMBRA_ADMIN_PASS` | 首次启动无人值守初始化管理员密码（未设置时通过 `/setup` 向导创建） | `""` |
| `AMBRA_TMDB_API_KEY` | 自定义 TMDB API Key（留空自动使用内置默认密钥） | 内置默认密钥 |
| `AMBRA_WEB_DIR` | 外部 Web UI 目录（留空使用内嵌前端） | `""` |
| `AMBRA_FFMPEG_PATH` | 自定义 `ffmpeg` 路径 | 自动探测 |
| `AMBRA_FFPROBE_PATH` | 自定义 `ffprobe` 路径 | 自动探测 |

> **FFmpeg 自动探测顺序**：环境变量 → 配置文件 → 内嵌释放（`-tags embed_ffmpeg`）→ 程序同级目录 → `<DataDir>/bin/` → 系统 `PATH`。

---

## 架构与技术文档

面向开发者与二次定制用户的完整设计文档见 [docs/README.md](docs/README.md)：
- **架构设计**：[系统总体架构](docs/architecture/01-system-overview.md) · [Emby API 兼容层](docs/architecture/02-api-compatibility-layer.md) · [SQLite 数据架构](docs/architecture/03-data-and-storage.md) · [媒体处理管线](docs/architecture/04-media-pipeline.md) · [统一插件内核](docs/architecture/05-extensibility.md)
- **子系统规格**：[数据库与中文检索](docs/specs/01-database-schema-and-query.md) · [认证与实时通信](docs/specs/02-auth-sessions-and-realtime.md) · [播放与动态转码](docs/specs/03-playback-transcode-and-subtitles.md) · [全媒体与 Live TV / DLNA](docs/specs/04-all-media-types-and-subsystems.md) · [契约测试与基准](docs/specs/05-testing-and-compatibility.md) · [现代 Web 客户端](docs/specs/06-web-client-and-player.md)
- **协议参考**：[Emby 4.10.2.0 API 兼容矩阵](docs/roadmap/02-api-coverage-matrix.md) · [Emby 4.10.2.0 完整 API 参考](docs/emby-4.10.2.0-api/README.md)

---

## 开源协议

本项目基于 [MIT License](LICENSE) 开源。
