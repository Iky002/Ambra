# 视频流、HLS 分片、字幕提取与 LiveStreams

> 所属模块：**04. 音视频流媒体、转码与图片**  
> 路由前缀：`/LiveStreams`, `/Videos`  
> 接口总数：**32** 条

涵盖视频直连流、HLS 主/子播放列表、TS/MP4 分片、附加视频、外挂字幕与实时流会话。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `POST` | `/LiveStreams/Close` | 需认证 | `void` | Closes a media source |
| 2 | `POST` | `/LiveStreams/MediaInfo` | 需认证 | `object` | Gets media info for a live stream |
| 3 | `POST` | `/LiveStreams/Open` | 需认证 | `LiveStreamResponse` | Opens a media source |
| 4 | `DELETE` | `/Videos/ActiveEncodings` | 需认证 | `void` | - |
| 5 | `POST` | `/Videos/ActiveEncodings/Delete` | 需认证 | `void` | - |
| 6 | `POST` | `/Videos/MergeVersions` | 认证 (`Admin`) | `void` | Merges videos into a single record |
| 7 | `GET` | `/Videos/{Id}/AdditionalParts` | 需认证 | `QueryResult<BaseItemDto>` | Gets additional parts for a video. |
| 8 | `DELETE` | `/Videos/{Id}/AlternateSources` | 认证 (`Admin`) | `void` | Removes alternate video sources. |
| 9 | `POST` | `/Videos/{Id}/AlternateSources/Delete` | 认证 (`Admin`) | `void` | Removes alternate video sources. |
| 10 | `GET` | `/Videos/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 11 | `GET` | `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 12 | `HEAD` | `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 13 | `GET` | `/Videos/{Id}/index.bif` | 公开 | `object` | - |
| 14 | `GET` | `/Videos/{Id}/live.m3u8` | 需认证 | `object` | - |
| 15 | `GET` | `/Videos/{Id}/live_subtitles.m3u8` | 需认证 | `-` | Gets an HLS subtitle playlist. |
| 16 | `GET` | `/Videos/{Id}/main.m3u8` | 需认证 | `object` | Gets a video stream using HTTP live streaming. |
| 17 | `GET` | `/Videos/{Id}/master.m3u8` | 需认证 | `object` | Gets a video stream using HTTP live streaming. |
| 18 | `HEAD` | `/Videos/{Id}/master.m3u8` | 需认证 | `object` | Gets a video stream using HTTP live streaming. |
| 19 | `GET` | `/Videos/{Id}/stream` | 需认证 | `object` | Gets a video stream |
| 20 | `HEAD` | `/Videos/{Id}/stream` | 需认证 | `object` | Gets a video stream |
| 21 | `GET` | `/Videos/{Id}/stream.{Container}` | 需认证 | `object` | Gets a video stream |
| 22 | `HEAD` | `/Videos/{Id}/stream.{Container}` | 需认证 | `object` | Gets a video stream |
| 23 | `GET` | `/Videos/{Id}/subtitles.m3u8` | 需认证 | `object` | Gets an HLS subtitle playlist. |
| 24 | `DELETE` | `/Videos/{Id}/Subtitles/{Index}` | 认证 (`ManageSubtitles`) | `void` | Deletes an external subtitle file |
| 25 | `POST` | `/Videos/{Id}/Subtitles/{Index}/Delete` | 认证 (`ManageSubtitles`) | `void` | Deletes an external subtitle file |
| 26 | `GET` | `/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream` | 公开 | `object` | Gets subtitles in a specified format. |
| 27 | `GET` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 28 | `HEAD` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 29 | `GET` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 30 | `HEAD` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 31 | `GET` | `/Videos/{Id}/{StreamFileName}` | 需认证 | `object` | Gets a video stream |
| 32 | `HEAD` | `/Videos/{Id}/{StreamFileName}` | 需认证 | `object` | Gets a video stream |

## 接口详情

### BifService（1 条）

#### `GET` `/Videos/{Id}/index.bif`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`RokuMetadata.Api.BifService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`RokuMetadata.Api.GetBifFile` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `Width` | `query` | `int` | 是 | 否 | - |

### DynamicHlsService（7 条）

#### `GET` `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetHlsSegment` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `PlaylistId` | `path` | `string` | 否 | 否 | - |
| `SegmentContainer` | `path` | `string` | 否 | 否 | - |
| `SegmentId` | `path` | `string` | 否 | 否 | - |
| `CurrentSubtitleStreamIndex` | `query` | `int` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetHlsSegment` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `PlaylistId` | `path` | `string` | 否 | 否 | - |
| `SegmentContainer` | `path` | `string` | 否 | 否 | - |
| `SegmentId` | `path` | `string` | 否 | 否 | - |
| `CurrentSubtitleStreamIndex` | `query` | `int` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Videos/{Id}/live.m3u8`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetLiveHlsStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `GET` `/Videos/{Id}/main.m3u8`

- **摘要**：Gets a video stream using HTTP live streaming.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetVariantHlsVideoPlaylist` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `GET` `/Videos/{Id}/master.m3u8`

- **摘要**：Gets a video stream using HTTP live streaming.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetMasterHlsVideoPlaylist` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `HEAD` `/Videos/{Id}/master.m3u8`

- **摘要**：Gets a video stream using HTTP live streaming.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetMasterHlsVideoPlaylist` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `GET` `/Videos/{Id}/subtitles.m3u8`

- **摘要**：Gets an HLS subtitle playlist.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetSubtitlePlaylist` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CurrentSubtitleStreamIndex` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `PlaySessionId` | `path` | `string` | 是 | 否 | PlaySessionId |
| `LiveStreamId` | `path` | `string` | 否 | 否 | LiveStreamId |
| `ManifestSubtitles` | `query` | `string` | 是 | 否 | The subtitle segment format |
| `SegmentLength` | `query` | `int` | 是 | 否 | The subtitle segment length |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

### Emby.Server.MediaEncoding (无直接 Service 绑定)（1 条）

#### `GET` `/Videos/{Id}/live_subtitles.m3u8`

- **摘要**：Gets an HLS subtitle playlist.
- **鉴权**：需要认证
- **返回类型**：`void / Stream`
- **处理服务**：未匹配直接方法
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetLiveSubtitlePlaylist` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CurrentSubtitleStreamIndex` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `PlaySessionId` | `path` | `string` | 是 | 否 | PlaySessionId |
| `LiveStreamId` | `path` | `string` | 否 | 否 | LiveStreamId |
| `ManifestSubtitles` | `query` | `string` | 是 | 否 | The subtitle segment format |
| `SegmentLength` | `query` | `int` | 是 | 否 | The subtitle segment length |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

### HlsSegmentService（2 条）

#### `DELETE` `/Videos/ActiveEncodings`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.HlsSegmentService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.StopEncodingProcess` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DeviceId` | `query` | `string` | 是 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `PlaySessionId` | `query` | `string` | 是 | 否 | The play session id |

#### `POST` `/Videos/ActiveEncodings/Delete`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.HlsSegmentService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.StopEncodingProcess` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DeviceId` | `query` | `string` | 是 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `PlaySessionId` | `query` | `string` | 是 | 否 | The play session id |

### MediaInfoService（3 条）

#### `POST` `/LiveStreams/Close`

- **摘要**：Closes a media source
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.CloseMediaSource` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `LiveStreamId` | `query` | `string` | 是 | 否 | LiveStreamId |
| `PlaySessionId` | `query` | `string` | 是 | 否 | PlaySessionId |

#### `POST` `/LiveStreams/MediaInfo`

- **摘要**：Gets media info for a live stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetLiveStreamMediaInfo` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `LiveStreamId` | `query` | `string` | 是 | 否 | LiveStreamId |

#### `POST` `/LiveStreams/Open`

- **摘要**：Opens a media source
- **鉴权**：需要认证
- **返回类型**：`LiveStreamResponse`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.OpenMediaSource` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `ConsumerId` | `query` | `string` | 否 | 否 | - |
| `DeviceProfile` | `query` | `DeviceProfile` | 否 | 否 | - |
| `EnableDirectPlay` | `query` | `bool` | 否 | 否 | - |
| `EnableDirectStream` | `query` | `bool` | 否 | 否 | - |
| `EnableTranscoding` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `OpenToken` | `query` | `string` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

### SubtitleService（7 条）

#### `DELETE` `/Videos/{Id}/Subtitles/{Index}`

- **摘要**：Deletes an external subtitle file
- **鉴权**：需要认证；角色要求: `ManageSubtitles`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.DeleteSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |

#### `POST` `/Videos/{Id}/Subtitles/{Index}/Delete`

- **摘要**：Deletes an external subtitle file
- **鉴权**：需要认证；角色要求: `ManageSubtitles`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.DeleteSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |

#### `GET` `/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetAttachment` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |

#### `GET` `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

### UserLibraryService（1 条）

#### `GET` `/Videos/{Id}/AdditionalParts`

- **摘要**：Gets additional parts for a video.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetAdditionalParts` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### VideoHlsService（1 条）

#### `GET` `/Videos/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.VideoHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetHlsVideoSegmentLegacy` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `PlaylistId` | `path` | `string` | 否 | 否 | - |
| `SegmentContainer` | `path` | `string` | 否 | 否 | - |
| `SegmentId` | `path` | `string` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |

### VideoService（6 条）

#### `GET` `/Videos/{Id}/stream`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `HEAD` `/Videos/{Id}/stream`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `GET` `/Videos/{Id}/stream.{Container}`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `HEAD` `/Videos/{Id}/stream.{Container}`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `GET` `/Videos/{Id}/{StreamFileName}`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

#### `HEAD` `/Videos/{Id}/{StreamFileName}`

- **摘要**：Gets a video stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetVideoStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `SegmentContainer` | `path` | `string` | 否 | 否 | SegmentContainer |
| `SegmentId` | `path` | `string` | 否 | 否 | SegmentId |
| `StreamFileName` | `path` | `string` | 否 | 否 | - |
| `Container` | `query` | `string` | 是 | 否 | Container |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify an audio bitrate to encode to, e.g. 128000. If omitted this will be left to encoder defaults. |
| `AudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a specific number of audio channels to encode to, e.g. 2 |
| `AudioCodec` | `query` | `string` | 否 | 否 | Optional. Specify a audio codec to encode to, e.g. mp3. If omitted the server will auto-select using the url's extension. Options: aac, mp3, vorbis, wma. |
| `AudioSampleRate` | `query` | `int?` | 否 | 否 | Optional. Specify a specific audio sample rate, e.g. 44100 |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the audio stream to use. If omitted the first audio stream will be used. |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Context` | `query` | `EncodingContext` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | Whether or not to copy timestamps when transcoding with an offset. Defaults to false. |
| `CpuCoreLimit` | `query` | `int?` | 否 | 否 | - |
| `DeleteOutputOnCancellation` | `query` | `bool` | 否 | 否 | - |
| `DeleteOutputOnException` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `DeviceProfileId` | `query` | `string` | 否 | 否 | Optional. The dlna device profile id to utilize. |
| `DisableSoftwareFallback` | `query` | `bool` | 否 | 否 | - |
| `DurationTicks` | `query` | `long?` | 否 | 否 | - |
| `EnableAutoStreamCopy` | `query` | `bool` | 否 | 否 | Whether or not to allow automatic stream copy if requested values match the original source. Defaults to true. |
| `EnableMpegtsM2TsMode` | `query` | `bool` | 否 | 否 | - |
| `EstimateContentLength` | `query` | `bool` | 否 | 否 | - |
| `FillEmptySubtitleSegments` | `query` | `bool` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | Optional. The fixed vertical resolution of the encoded video. |
| `IsResolutionCapped` | `query` | `bool` | 否 | 否 | - |
| `IsTranscodeTestJob` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `ManifestSubtitles` | `query` | `string` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | Optional. Specify a maximum number of audio channels to encode to, e.g. 2 |
| `MaxHeight` | `query` | `int?` | 否 | 否 | Optional. The maximum vertical resolution of the encoded video. |
| `MaxVideoBitDepth` | `query` | `int?` | 否 | 否 | Optional. |
| `MaxWidth` | `query` | `int?` | 否 | 否 | Optional. The maximum horizontal resolution of the encoded video. |
| `MinSegments` | `query` | `int?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `RequireNonAnamorphic` | `query` | `bool` | 否 | 否 | - |
| `SegmentLength` | `query` | `int?` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `Static` | `query` | `bool` | 否 | 否 | Optional. If true, the original file will be streamed statically without any encoding. Use either no url extension or the original file extension. true/false |
| `StreamOptions` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `SubtitleCodec` | `query` | `string` | 否 | 否 | - |
| `SubtitleMethod` | `query` | `SubtitleDeliveryMethod` | 否 | 否 | Optional. Specify the subtitle delivery method. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the subtitle stream to use. If omitted no subtitles will be used. |
| `SubtitleStreamIndexes` | `query` | `string` | 否 | 否 | - |
| `Tag` | `query` | `string` | 否 | 否 | - |
| `TranscodeReasons` | `query` | `string` | 否 | 否 | - |
| `TranscodeSeekInfo` | `query` | `TranscodeSeekInfo` | 否 | 否 | - |
| `TranscodingMaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `VideoBitRate` | `query` | `int?` | 否 | 否 | Optional. Specify a video bitrate to encode to, e.g. 500000. If omitted this will be left to encoder defaults. |
| `VideoCodec` | `query` | `string` | 否 | 否 | Optional. Specify a video codec to encode to, e.g. h264. If omitted the server will auto-select using the url's extension. Options: h264, mpeg4, theora, vpx, wmv. |
| `VideoSideDataSubs` | `query` | `string` | 否 | 否 | - |
| `VideoStreamIndex` | `query` | `int?` | 否 | 否 | Optional. The index of the video stream to use. If omitted the first video stream will be used. |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | Optional. The fixed horizontal resolution of the encoded video. |

### VideosService（3 条）

#### `POST` `/Videos/MergeVersions`

- **摘要**：Merges videos into a single record
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.VideosService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.MergeVersions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Ids` | `query` | `string` | 否 | 是 | Item id list. This allows multiple, comma delimited. |

#### `DELETE` `/Videos/{Id}/AlternateSources`

- **摘要**：Removes alternate video sources.
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.VideosService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.DeleteAlternateSources` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `POST` `/Videos/{Id}/AlternateSources/Delete`

- **摘要**：Removes alternate video sources.
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.VideosService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.DeleteAlternateSources` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
