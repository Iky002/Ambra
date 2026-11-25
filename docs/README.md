<p align="center">
  <img src="../assets/ambra-logo.svg" alt="Ambra Logo" width="88" />
</p>

# Ambra 技术文档中心

**Ambra** 是基于 **Go + SQLite** 从零构建、面向 Emby Server `4.10.2.0` 协议全量兼容的轻量级现代媒体服务器，已完成全部 **583 条 Emby HTTP API** 契约兼容与内嵌现代 Web 客户端实现。

---

## 文档导航

### 1. 整体架构设计（`architecture/`）
| 文档 | 核心内容 |
| :--- | :--- |
| [01. 系统总体架构与技术选型](architecture/01-system-overview.md) | 架构原则、Go 技术栈选型、分层架构图、Go 代码目录规划与进程模型 |
| [02. Emby API 兼容层设计](architecture/02-api-compatibility-layer.md) | `/emby` 双前缀路由、鉴权中间件、PascalCase 序列化、Query 大小写不敏感、GUID 归一化、WebSocket 协议 |
| [03. 数据架构与 SQLite 存储设计](architecture/03-data-and-storage.md) | WAL + 读写分离连接池、表结构分组规划、缓存与数据目录布局 |
| [04. 媒体处理管线设计](architecture/04-media-pipeline.md) | 扫库/刮削/NFO、直连与 302 重定向、FFmpeg 动态 HLS 转码、图片缓存、LiveTV 与 DLNA 概览 |
| [05. 统一插件内核与扩展架构](architecture/05-extensibility.md) | 刮削器/字幕/通知/Trakt 全部插件化（内置官方插件与外部 `wazero` WASM / HTTP 插件共用同一契约）及外部生态兼容 |

### 2. 核心子系统规格（`specs/`）
| 文档 | 核心内容 |
| :--- | :--- |
| [01. 数据库详细 Schema、动态查询与中文检索](specs/01-database-schema-and-query.md) | SQLite DDL、树形闭包表 `item_ancestors`、`/Items` 动态查询构造器、FTS5 中文分词/拼音/乱序检索算法与核心 DTO 规范 |
| [02. 认证、权限、会话与实时通信](specs/02-auth-sessions-and-realtime.md) | Token/API Key 鉴权流、`UserPolicy` 家长控制与媒体库白名单、`SessionManager` 播放状态机（90% 自动已看）、WebSocket 协议帧与 Parties 同步播放 |
| [03. 播放决策、动态 HLS 转码与字幕处理](specs/03-playback-transcode-and-subtitles.md) | `PlaybackInfo` 五级智能播放决策（含客户端零转码层）、STRM 302 策略、动态 HLS 虚拟分片数学模型与 Seek 重启、硬解/HDR 色调映射、简繁字幕增强 |
| [04. 全媒体类型、Live TV、Sync、DLNA 与 Web UI](specs/04-all-media-types-and-subsystems.md) | 音乐/InstantMix/有声书/照片/合集/播放列表、Live TV 多路流复用与录播调度、Sync 离线同步、DLNA SSDP/UPnP 与 GenericUI 方案 |
| [05. 契约测试、影子对比与性能基准](specs/05-testing-and-compatibility.md) | 全量路由自动化契约测试、官方 4.10.2.0 影子对比（Shadow Diff）、客户端验收矩阵与性能门槛 |
| [06. 现代 Web 客户端与零转码播放器](specs/06-web-client-and-player.md) | 内嵌现代网页客户端布局与交互、**JASSUB (ASS/SSA)** + **PGS Canvas** 客户端直出渲染、**`ffmpeg.wasm` / MSE** 解复用与自动降级策略 |

### 3. 协议兼容矩阵与 Emby 4.10.2.0 参考
| 文档 | 核心内容 |
| :--- | :--- |
| [Emby 4.10.2.0 API 兼容矩阵](roadmap/02-api-coverage-matrix.md) | 全部 7 大业务域、23 份 API 子文档的路由覆盖与契约验收清单 |
| [Emby 4.10.2.0 完整 HTTP API 参考](emby-4.10.2.0-api/README.md) | 从官方 `embyserver-netcore_4.10.2.0.zip` 提取的全量路由、参数、返回类型与鉴权规范 |
