# 音频流与通用音频转码

> 所属模块：**04. 音视频流媒体、转码与图片**  
> 路由前缀：`/Audio`  
> 接口总数：**17** 条

涵盖音频直连流、通用音频转码、HLS 音频流与歌词/音轨获取。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Audio/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 2 | `GET` | `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 3 | `HEAD` | `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | 公开 | `object` | - |
| 4 | `GET` | `/Audio/{Id}/live.m3u8` | 需认证 | `object` | - |
| 5 | `GET` | `/Audio/{Id}/main.m3u8` | 需认证 | `object` | Gets an audio stream using HTTP live streaming. |
| 6 | `GET` | `/Audio/{Id}/master.m3u8` | 需认证 | `object` | Gets an audio stream using HTTP live streaming. |
| 7 | `HEAD` | `/Audio/{Id}/master.m3u8` | 需认证 | `object` | Gets an audio stream using HTTP live streaming. |
| 8 | `GET` | `/Audio/{Id}/stream` | 需认证 | `object` | Gets an audio stream |
| 9 | `HEAD` | `/Audio/{Id}/stream` | 需认证 | `object` | Gets an audio stream |
| 10 | `GET` | `/Audio/{Id}/stream.{Container}` | 需认证 | `object` | Gets an audio stream |
| 11 | `HEAD` | `/Audio/{Id}/stream.{Container}` | 需认证 | `object` | Gets an audio stream |
| 12 | `GET` | `/Audio/{Id}/universal` | 需认证 | `object` | Gets an audio stream |
| 13 | `HEAD` | `/Audio/{Id}/universal` | 需认证 | `object` | Gets an audio stream |
| 14 | `GET` | `/Audio/{Id}/universal.{Container}` | 需认证 | `object` | Gets an audio stream |
| 15 | `HEAD` | `/Audio/{Id}/universal.{Container}` | 需认证 | `object` | Gets an audio stream |
| 16 | `GET` | `/Audio/{Id}/{StreamFileName}` | 需认证 | `object` | Gets an audio stream |
| 17 | `HEAD` | `/Audio/{Id}/{StreamFileName}` | 需认证 | `object` | Gets an audio stream |

## 接口详情

### AudioService（6 条）

#### `GET` `/Audio/{Id}/stream`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

#### `HEAD` `/Audio/{Id}/stream`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

#### `GET` `/Audio/{Id}/stream.{Container}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

#### `HEAD` `/Audio/{Id}/stream.{Container}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

#### `GET` `/Audio/{Id}/{StreamFileName}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

#### `HEAD` `/Audio/{Id}/{StreamFileName}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.AudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetProgressiveAudioStream` (`Emby.Server.MediaEncoding.dll`)

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

### DynamicHlsService（6 条）

#### `GET` `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`

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

#### `HEAD` `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`

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

#### `GET` `/Audio/{Id}/live.m3u8`

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

#### `GET` `/Audio/{Id}/main.m3u8`

- **摘要**：Gets an audio stream using HTTP live streaming.
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

#### `GET` `/Audio/{Id}/master.m3u8`

- **摘要**：Gets an audio stream using HTTP live streaming.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetMasterHlsAudioPlaylist` (`Emby.Server.MediaEncoding.dll`)

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

#### `HEAD` `/Audio/{Id}/master.m3u8`

- **摘要**：Gets an audio stream using HTTP live streaming.
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Hls.DynamicHlsService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Hls.GetMasterHlsAudioPlaylist` (`Emby.Server.MediaEncoding.dll`)

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

### UniversalAudioService（4 条）

#### `GET` `/Audio/{Id}/universal`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.UniversalAudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetUniversalAudioStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Container` | `query` | `string` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `EnableRedirection` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteMedia` | `query` | `bool` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioSampleRate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `TranscodingAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `TranscodingContainer` | `query` | `string` | 否 | 否 | - |
| `TranscodingProtocol` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Audio/{Id}/universal`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.UniversalAudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetUniversalAudioStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `Container` | `query` | `string` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `EnableRedirection` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteMedia` | `query` | `bool` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioSampleRate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `TranscodingAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `TranscodingContainer` | `query` | `string` | 否 | 否 | - |
| `TranscodingProtocol` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Audio/{Id}/universal.{Container}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.UniversalAudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetUniversalAudioStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `Container` | `path` | `string` | 否 | 否 | - |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `EnableRedirection` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteMedia` | `query` | `bool` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioSampleRate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `TranscodingAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `TranscodingContainer` | `query` | `string` | 否 | 否 | - |
| `TranscodingProtocol` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Audio/{Id}/universal.{Container}`

- **摘要**：Gets an audio stream
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.UniversalAudioService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetUniversalAudioStream` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `Container` | `path` | `string` | 否 | 否 | - |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `BreakOnNonKeyFrames` | `query` | `bool` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | The device id of the client requesting. Used to stop encoding processes when needed. |
| `EnableRedirection` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteMedia` | `query` | `bool` | 否 | 否 | - |
| `MaxAudioBitDepth` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxAudioSampleRate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | Optional. Specify a starting offset, in ticks. 1ms = 10000 ticks. |
| `TranscodingAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `TranscodingContainer` | `query` | `string` | 否 | 否 | - |
| `TranscodingProtocol` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

### VideoHlsService（1 条）

#### `GET` `/Audio/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}`

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
