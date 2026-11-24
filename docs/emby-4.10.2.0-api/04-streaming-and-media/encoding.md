# 编码与硬件加速配置

> 所属模块：**04. 音视频流媒体、转码与图片**  
> 路由前缀：`/Encoding`  
> 接口总数：**13** 条

涵盖 FFmpeg 编码参数、硬件加速编解码器检测、色调映射与转码配置。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Encoding/CodecConfiguration/Defaults` | 认证 (`admin`) | `CodecConfiguration[]` | Gets default codec configurations |
| 2 | `GET` | `/Encoding/CodecInformation/Video` | 认证 (`admin`) | `VideoCodecBase[]` | Gets details about available video encoders and decoders |
| 3 | `GET` | `/Encoding/CodecParameters` | 需认证 | `EditObjectContainer` | Gets the parameters for a specified codec. |
| 4 | `POST` | `/Encoding/CodecParameters` | 认证 (`Admin`) | `void` | Updates the parameters for a specified codec. |
| 5 | `GET` | `/Encoding/FfmpegOptions` | 需认证 | `EditObjectContainer` | Gets the ffmpeg options |
| 6 | `POST` | `/Encoding/FfmpegOptions` | 认证 (`Admin`) | `void` | Updates the ffmpeg options |
| 7 | `GET` | `/Encoding/FullToneMapOptions` | 需认证 | `EditObjectContainer` | Gets the tone mapping options |
| 8 | `POST` | `/Encoding/FullToneMapOptions` | 认证 (`Admin`) | `void` | Updates the tone mapping options |
| 9 | `GET` | `/Encoding/PublicToneMapOptions` | 需认证 | `EditObjectContainer` | Gets the tone mapping options |
| 10 | `POST` | `/Encoding/PublicToneMapOptions` | 认证 (`Admin`) | `void` | Updates the tone mapping options |
| 11 | `GET` | `/Encoding/SubtitleOptions` | 需认证 | `EditObjectContainer` | Gets the subtitle options |
| 12 | `POST` | `/Encoding/SubtitleOptions` | 认证 (`Admin`) | `void` | Updates the subtitle options |
| 13 | `GET` | `/Encoding/ToneMapOptions` | 认证 (`admin`) | `ToneMapOptionsVisibility` | Gets available tone mapping options |

## 接口详情

### CodecParameterService（2 条）

#### `GET` `/Encoding/CodecParameters`

- **摘要**：Gets the parameters for a specified codec.
- **鉴权**：需要认证
- **返回类型**：`EditObjectContainer`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Codecs.CodecParameterService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Codecs.GetCodecParameters` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CodecId` | `query` | `string` | 是 | 否 | Codec Id |
| `ParameterContext` | `query` | `CodecParameterContext` | 是 | 否 | Parameter Context |

#### `POST` `/Encoding/CodecParameters`

- **摘要**：Updates the parameters for a specified codec.
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Codecs.CodecParameterService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Codecs.UpdateCodecParameters` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CodecId` | `query` | `string` | 是 | 否 | Codec Id |
| `ParameterContext` | `query` | `CodecParameterContext` | 是 | 否 | Parameter Context |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### EncodingInfoService（3 条）

#### `GET` `/Encoding/CodecConfiguration/Defaults`

- **摘要**：Gets default codec configurations
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`CodecConfiguration[]`
- **处理服务**：`Emby.Server.MediaEncoding.Api.EncodingInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetDefaultCodecConfigurations` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `GET` `/Encoding/CodecInformation/Video`

- **摘要**：Gets details about available video encoders and decoders
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`VideoCodecBase[]`
- **处理服务**：`Emby.Server.MediaEncoding.Api.EncodingInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetVideoCodecInformation` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `GET` `/Encoding/ToneMapOptions`

- **摘要**：Gets available tone mapping options
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`ToneMapOptionsVisibility`
- **处理服务**：`Emby.Server.MediaEncoding.Api.EncodingInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetToneMapOptions` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

### FfmpegOptionsService（2 条）

#### `GET` `/Encoding/FfmpegOptions`

- **摘要**：Gets the ffmpeg options
- **鉴权**：需要认证
- **返回类型**：`EditObjectContainer`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Ffmpeg.FfmpegOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Ffmpeg.GetFfmpegOptions` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `POST` `/Encoding/FfmpegOptions`

- **摘要**：Updates the ffmpeg options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Ffmpeg.FfmpegOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Ffmpeg.UpdateFfmpegOptions` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### SubtitleOptionsService（2 条）

#### `GET` `/Encoding/SubtitleOptions`

- **摘要**：Gets the subtitle options
- **鉴权**：需要认证
- **返回类型**：`EditObjectContainer`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Subtitles.SubtitleOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Subtitles.GetSubtitleOptions` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `POST` `/Encoding/SubtitleOptions`

- **摘要**：Updates the subtitle options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Subtitles.SubtitleOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Subtitles.UpdateSubtitleOptions` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### ToneMapOptionsService（4 条）

#### `GET` `/Encoding/FullToneMapOptions`

- **摘要**：Gets the tone mapping options
- **鉴权**：需要认证
- **返回类型**：`EditObjectContainer`
- **处理服务**：`Emby.Server.MediaEncoding.Api.ToneMap.ToneMapOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ToneMap.GetToneMapOptions` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `POST` `/Encoding/FullToneMapOptions`

- **摘要**：Updates the tone mapping options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.ToneMap.ToneMapOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ToneMap.UpdateToneMapOptions` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `GET` `/Encoding/PublicToneMapOptions`

- **摘要**：Gets the tone mapping options
- **鉴权**：需要认证
- **返回类型**：`EditObjectContainer`
- **处理服务**：`Emby.Server.MediaEncoding.Api.ToneMap.ToneMapOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ToneMap.GetToneMapOptions` (`Emby.Server.MediaEncoding.dll`)

_无请求参数。_

#### `POST` `/Encoding/PublicToneMapOptions`

- **摘要**：Updates the tone mapping options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.ToneMap.ToneMapOptionsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ToneMap.UpdateToneMapOptions` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |
