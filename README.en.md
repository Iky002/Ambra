<p align="center">
  <img src="assets/ambra-logo.svg" alt="Ambra Logo" width="110" />
</p>

<h1 align="center">Ambra Media Server</h1>

<p align="center">
  A lightweight, fast, batteries-included modern personal cinema server built with <strong>Go + SQLite</strong>, 100% compatible with the <strong>Emby Server 4.10.2.0</strong> ecosystem.
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <strong>English</strong>
</p>

<p align="center">
  <a href="https://github.com/Iky002/Ambra/releases/latest"><img src="https://img.shields.io/github/v/release/Iky002/Ambra?label=Latest&color=f59e0b" alt="Latest Release" /></a>
  <a href="https://github.com/Iky002/Ambra/actions/workflows/ci.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Iky002/Ambra/actions/workflows/release.yml"><img src="https://github.com/Iky002/Ambra/actions/workflows/release.yml/badge.svg" alt="Release" /></a>
  <a href="https://iky002.github.io/Ambra/"><img src="https://img.shields.io/badge/Live%20Demo-Try%20Online-10b981" alt="Live Demo" /></a>
  <img src="https://img.shields.io/badge/Emby%204.10.2.0%20API-583%2F583%20(100%25)-0ea5e9" alt="583/583 APIs" />
  <img src="https://img.shields.io/badge/Go%20Coverage-%E2%89%A590%25-6366f1" alt="Go Coverage" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-94a3b8" alt="MIT License" /></a>
</p>

---

## Why Ambra?

- **Ultra-lightweight single-binary / single-container deployment**: Pure Go (`CGO_ENABLED=0`) with embedded SQLite (WAL mode + read/write connection pools) and an embedded modern React Web UI. Runs effortlessly on NAS devices and low-power VPS instances with minimal memory footprint.
- **Full Emby 4.10.2.0 client compatibility (583 / 583 routes)**: 100% verified by automated dual-prefix contract tests. Works out of the box with **Infuse, VidHub, SenPlayer, Yamby, Hills, official Emby apps, Kodi**, and automation tools like **MoviePilot / Sonarr / Radarr**.
- **Built-in 10-language internationalization (i18n)**: Native UI and backend localization for **Simplified Chinese, Traditional Chinese, English, Japanese, Korean, Spanish, German, French, Portuguese, and Thai** with automatic browser detection and instant switching.
- **Enhanced CJK & multi-language media library experience**:
  - SQLite FTS5 fuzzy search with Chinese segmentation, full/initial Pinyin matching, Traditional/Simplified normalization, and out-of-order multi-keyword search;
  - A–Z library jump bar with native Chinese Pinyin initial support;
  - Smart subtitle/audio track detection distinguishing Simplified, Traditional, and Bilingual Chinese (`CHS` / `CHT` / `SC` / `TC`);
  - MoviePilot-inspired filename/folder recognition (accurately handles year-in-title films like *The Legend of 1900 (1998)* and *Blade Runner 2049 (2017)*, inline `{tmdb-*}` tags, and local `.nfo` / artwork saving).
- **Modern Web Cinema & zero-transcode playback**:
  - Home dashboard featuring **Hero Carousel, Continue Watching, Next Up, Trending Charts (TMDB / Douban / IMDb), and Latest Added**;
  - **Cross-directory multi-version auto-merging**, **TMDB Auto Box Sets (Collections)**, playlist queue auto-play, and global Music Player with LRC lyrics;
  - Client-side **ASS/SSA subtitle rendering (JASSUB)** and **Blu-ray PGS Canvas rendering**, **STRM 302 direct redirect** with automatic proxy/HLS fallback, streaming MKV remuxing, and hardware-accelerated HLS transcoding (VideoToolbox / NVENC / QSV / VAAPI / AMF).
- **Complete home server & power-user features**: Multi-user RBAC & Parental Controls (rating/tag blocking), Quick Connect login, Live TV (M3U + XMLTV EPG + DVR recording), DLNA UPnP/SSDP casting, Notification Center, Webhooks, Scheduled Tasks, and one-click SQLite hot Backup & Restore.

---

## Live Demo (No Backend Required)

Try the full dark-cinema Web UI and interactive Admin Console right in your browser (including one-click Admin / Member role switching):

- 👉 **[Open Ambra Live Demo](https://iky002.github.io/Ambra/)**

> You can also self-host the Serverless Demo on Vercel (Root Directory: `web`) or Cloudflare Pages (Root Directory: `web`, Build Command: `npm run build:demo`, Output Directory: `dist-demo`).

---

## Quick Start

### 1. Choose Your Edition (Full vs Lite)

| Edition | Docker Image (GHCR) | GitHub Release Artifact | Best For |
| :--- | :--- | :--- | :--- |
| **Full (Recommended)** | `ghcr.io/iky002/ambra:latest` / `:full` | `ambra-full-<os>-<arch>.(tar.gz\|zip)` | **Bundled `ffmpeg` + `ffprobe`**, ready out of the box for deep probing, hardware/software HLS transcoding, and Live TV DVR |
| **Lite** | `ghcr.io/iky002/ambra:lite` | `ambra-lite-<os>-<arch>(.exe)` | **No bundled FFmpeg** (~35 MB Alpine image), ideal for STRM 302 setups or custom FFmpeg mounts |

### 2. Docker Compose (Recommended)

Download [docker-compose.yml](docker-compose.yml), uncomment your media volume mounts (and optional GPU passthrough), then run:

```bash
docker compose up -d
```

### 3. Docker CLI

```bash
docker run -d \
  --name ambra \
  --restart unless-stopped \
  -p 8096:8096 \
  -p 1900:1900/udp \
  -e TZ=UTC \
  -v $(pwd)/data:/data \
  -v /path/to/media:/media:ro \
  ghcr.io/iky002/ambra:latest
```

### 4. First-Time Setup

Open **`http://<your-ip>:8096/web/`** in your browser:
1. Complete the **Setup Wizard** (if skipped, the default admin account is `admin` / `admin` — please change it promptly);
2. Go to **Admin Console → Libraries** to mount your movies, TV shows, music, or `.strm` folders;
3. Add your `TMDB API Key` in **Settings / Plugins** to automatically fetch posters, backdrops, cast, and collections.

### 5. Build from Source (Optional)

```bash
make web          # Build embedded Web UI (web/dist)
make build        # Build pure-Go binary (bin/ambra)
./bin/ambra -data ./data -addr :8096
```

---

## Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `TZ` | Container timezone (for logs & scheduled tasks) | `Asia/Shanghai` |
| `AMBRA_HTTP_ADDR` | HTTP listen address | `:8096` |
| `AMBRA_TMDB_API_KEY` | TMDB API Key for metadata & artwork scraping | `""` |
| `AMBRA_WEB_DIR` | External Web UI directory override (empty = embedded UI) | `""` |
| `AMBRA_FFMPEG_PATH` | Custom `ffmpeg` binary path | Auto-detect |
| `AMBRA_FFPROBE_PATH` | Custom `ffprobe` binary path | Auto-detect |

> **FFmpeg detection order**: Environment variable → Config file → Embedded binary (`-tags embed_ffmpeg`) → Same directory as `ambra` → `<DataDir>/bin/` → System `PATH`.

---

## Architecture & Documentation

Full technical documentation is available in [docs/README.md](docs/README.md) (in Chinese):
- **Architecture**: [System Overview](docs/architecture/01-system-overview.md) · [Emby API Compatibility Layer](docs/architecture/02-api-compatibility-layer.md) · [SQLite Storage](docs/architecture/03-data-and-storage.md) · [Media Pipeline](docs/architecture/04-media-pipeline.md) · [Plugin Kernel](docs/architecture/05-extensibility.md)
- **Specs**: [Database & Search](docs/specs/01-database-schema-and-query.md) · [Auth & WebSocket](docs/specs/02-auth-sessions-and-realtime.md) · [Playback & Transcode](docs/specs/03-playback-transcode-and-subtitles.md) · [Media Subsystems](docs/specs/04-all-media-types-and-subsystems.md) · [Contract Testing](docs/specs/05-testing-and-compatibility.md) · [Web Client](docs/specs/06-web-client-and-player.md)
- **Protocol Reference**: [Emby 4.10.2.0 API Coverage Matrix](docs/roadmap/02-api-coverage-matrix.md) · [Emby 4.10.2.0 API Reference](docs/emby-4.10.2.0-api/README.md)

---

## License

Ambra is open-source software licensed under the [MIT License](LICENSE).
