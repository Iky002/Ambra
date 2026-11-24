# 04. 媒体处理管线设计

[← 返回文档中心](../README.md)

媒体处理管线是 Ambra 的核心引擎，涵盖 **扫库刮削、播放决策（直连/STRM 302/转码）、字幕、图片、Live TV 与 DLNA** 六大链路。

---

## 1. 媒体库扫描与刮削管线

```mermaid
flowchart LR
    FS["本地目录 / 挂载盘 / STRM"] --> Walk["增量遍历 (mtime/size 对比)"]
    Walk --> Parse["文件名解析 (季/集/年份/分辨率/多版本)"]
    Parse --> Probe["元数据探测 (优先读本地 NFO; 本地文件按需 ffprobe; STRM 延迟/可选探测)"]
    Probe --> Scrape["远端刮削 (TMDB / TVDB / 豆瓣 / 图片下载)"]
    Scrape --> Index["批量事务入库 SQLite + 更新 FTS5 中文搜索索引"]
    Index --> Notify["发布 LibraryChanged 事件 → WebSocket & Webhooks"]
```

- **多版本合并**：自动识别同目录下的多画质/多版本文件（如 `Movie - 1080p.mkv` 与 `Movie - 4K HDR.mkv`），归入同一 `Item` 的多个 `MediaSources`。
- **STRM 友好策略**：默认不强行全量 `ffprobe` 远程 `.strm` 链接（避免触发网盘风控），支持“读取本地 `.nfo` 中的 `<fileinfo>` + 首次播放时按需回填媒体信息”。
- **简繁中文字幕识别**：解析外挂文件名（`.chs`/`.cht`/`.sc`/`.tc`/`.zh-Hans`/`.zh-Hant`）与内封轨道标题，在 `MediaStream.DisplayTitle` 中清晰区分 **简体中文 / 繁体中文 / 双语字幕**。

---

## 2. 播放决策与流媒体管线（`04-streaming-and-media`）

客户端发起播放的标准流程：
1. **`POST /Items/{Id}/PlaybackInfo`**：客户端上报自身的 `DeviceProfile`（支持的容器、编解码器、最大码率、字幕格式）。
2. **决策引擎（DirectPlay / DirectStream / Transcode / 302 Redirect）**：
   - **STRM 302 直链**：当媒体源为 `.strm` 且开启 302 策略时，在 `/Videos/{Id}/stream` 直接返回 `302 Found`（支持自定义 UA / 鉴权头签名规则），零服务器带宽消耗。
   - **DirectPlay（原画直连）**：通过 `http.ServeContent` 提供标准 HTTP `Range` 断点续传与 `ETag`。
   - **DirectStream（仅封装复用）**：音视频编码兼容但容器不支持时，用 FFmpeg `-c copy` 极速转封装为 HLS/MP4。
   - **Transcode（动态转码）**：按需生成 HLS `master.m3u8` → `main.m3u8` → 按切片编号动态拉起/Seek FFmpeg 生成 `.ts` / `.m4s` 分片。

### FFmpeg 会话生命周期管理
- 每个 `PlaySessionId` 对应一个受控转码任务，支持：
  - 客户端拖动进度条（Seek）时平滑重启 FFmpeg 到新时间点。
  - 心跳超时（如 60 秒无分片请求）或收到 `DELETE /Videos/ActiveEncodings` 时立即终止进程并删除临时切片。
  - 自动探测并支持硬件加速：Intel QSV、VAAPI、NVIDIA NVENC、Apple VideoToolbox（对应 `/Encoding/*` 配置接口）。

---

## 3. 字幕处理管线

- **外挂字幕**：直接按请求格式（`.vtt` / `.srt` / `.ass`）输出，若客户端需要 WebVTT 而源文件为 SRT/ASS，内置轻量转换器实时转换。
- **内封字幕**：按需调用 FFmpeg 提取并缓存到 `data/cache/subtitles/`，避免重复抽轨。
- **远端字幕搜索与下载**：支持 `/Items/{Id}/RemoteSearch/Subtitles/{Language}` 对接 OpenSubtitles 等字幕源。

---

## 4. 图片处理与缓存管线

Emby 图片路由分布在 `/Items/{Id}/Images/{Type}`、`/Users/{Id}/Images/{Type}`、`/Artists/...` 等多处，统一收口到同一个 `ImageService`：
- 支持全部 Emby 查询参数：`maxWidth`, `maxHeight`, `width`, `height`, `quality`, `format`（`webp`/`jpg`/`png`）, `fillWidth`, `fillHeight`, `blur`, `tag`。
- **多级缓存**：以 `(item_id, image_type, index, transform_params_hash)` 为键落盘缓存，响应携带强 `ETag` 与 `Cache-Control`，命中直接零 CPU 零内存分配 `sendfile` 返回。

---

## 5. Live TV、Sync 与 DLNA 管线

- **Live TV（`05-live-tv`）**：内置 M3U 播放列表源解析、XMLTV EPG 节目单定时同步、基于 FFmpeg 的直播流代理/时移与定时录播（支持单集 Timer 与系列 SeriesTimer 调度）。
- **Sync（`06-sync-and-notifications`）**：异步转码下载任务队列，把媒体预转码为目标设备规格并暂存，供客户端拉取后清理。
- **DLNA（`07-builtin-plugins`）**：内置 SSDP 广播发现、UPnP ContentDirectory / ConnectionManager SOAP 控制与 `SUBSCRIBE`/`UNSUBSCRIBE` 事件推送，兼容局域网电视与老式播放器。
