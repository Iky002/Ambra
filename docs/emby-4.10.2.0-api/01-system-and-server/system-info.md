# 系统信息、配置、许可与本地化

> 所属模块：**01. 系统与服务器管理**  
> 路由前缀：`/Appstore`, `/Branding`, `/Features`, `/Localization`, `/Registrations`, `/System`  
> 接口总数：**32** 条

涵盖系统状态、活动日志、配置读写、功能许可（Premiere）、品牌定制与本地化选项。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `POST` | `/Appstore/Register` *(隐藏)* | 需认证 | `void` | Registers an appstore sale |
| 2 | `GET` | `/Branding/Configuration` | 公开 | `BrandingOptions` | Gets branding configuration |
| 3 | `GET` | `/Branding/Css` | 公开 | `object` | Gets custom css |
| 4 | `GET` | `/Branding/Css.css` | 公开 | `object` | Gets custom css |
| 5 | `GET` | `/Features` | 认证 (`admin`) | `FeatureInfo[]` | Gets a list of installed features |
| 6 | `GET` | `/Localization/Countries` | 需认证 | `CountryInfo[]` | Gets known countries |
| 7 | `GET` | `/Localization/Cultures` | 需认证 | `CultureDto[]` | Gets known cultures |
| 8 | `GET` | `/Localization/Options` | 需认证 | `LocalizatonOption[]` | Gets localization options |
| 9 | `GET` | `/Localization/ParentalRatings` | 需认证 | `ParentalRating[]` | Gets known parental ratings |
| 10 | `GET` | `/Registrations/{Name}` *(隐藏)* | 需认证 | `RegistrationInfo` | Gets registration status for a feature |
| 11 | `GET` | `/System/ActivityLog/Entries` | 认证 (`Admin`) | `QueryResult<ActivityLogEntry>` | Gets activity log entries |
| 12 | `GET` | `/System/Configuration` | 需认证 | `ServerConfiguration` | Gets application configuration |
| 13 | `POST` | `/System/Configuration` | 认证 (`Admin`) | `void` | Updates application configuration |
| 14 | `POST` | `/System/Configuration/Partial` | 认证 (`Admin`) | `void` | Updates application configuration |
| 15 | `GET` | `/System/Configuration/{Key}` | 需认证 | `object` | Gets a named configuration |
| 16 | `POST` | `/System/Configuration/{Key}` | 认证 (`Admin`) | `void` | Updates named configuration |
| 17 | `GET` | `/System/Endpoint` | 需认证 | `EndPointInfo` | Gets information about the request endpoint |
| 18 | `GET` | `/System/Info` | 需认证 | `SystemInfo` | Gets information about the server |
| 19 | `GET` | `/System/Info/Public` | 公开 | `PublicSystemInfo` | Gets public information about the server |
| 20 | `GET` | `/System/Logs` *(隐藏)* | 认证 (`Admin`) | `LogFile[]` | Gets a list of available server log files |
| 21 | `GET` | `/System/Logs/Log` *(隐藏)* | 认证 (`Admin`) | `object` | Gets a log file |
| 22 | `GET` | `/System/Logs/Query` | 认证 (`Admin`) | `QueryResult<LogFile>` | Gets a list of available server log files |
| 23 | `GET` | `/System/Logs/{Name}` | 认证 (`Admin`) | `object` | Gets a log file |
| 24 | `GET` | `/System/Logs/{Name}/Lines` | 认证 (`Admin`) | `QueryResult<string>` | Gets a log file |
| 25 | `GET` | `/System/Ping` | 公开 | `void` | - |
| 26 | `HEAD` | `/System/Ping` | 公开 | `void` | - |
| 27 | `POST` | `/System/Ping` | 公开 | `void` | - |
| 28 | `GET` | `/System/ReleaseNotes` | 需认证 | `PackageVersionInfo` | Gets release notes |
| 29 | `GET` | `/System/ReleaseNotes/Versions` | 需认证 | `PackageVersionInfo[]` | Gets release notes |
| 30 | `POST` | `/System/Restart` | 认证 (`Admin`) | `void` | Restarts the application, if needed |
| 31 | `POST` | `/System/Shutdown` | 认证 (`Admin`) | `void` | Shuts down the application |
| 32 | `GET` | `/System/WakeOnLanInfo` | 需认证 | `WakeOnLanInfo[]` | Gets wake on lan information |

## 接口详情

### ActivityLogService（1 条）

#### `GET` `/System/ActivityLog/Entries`

- **摘要**：Gets activity log entries
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<ActivityLogEntry>`
- **处理服务**：`Emby.ActivityLog.Api.ActivityLogService` (Emby.ActivityLog.dll)
- **Request DTO**：`Emby.ActivityLog.Api.GetActivityLogs` (`Emby.ActivityLog.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `HasUserId` | `query` | `bool?` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `MinDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum date. Format = ISO |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

### BrandingService（3 条）

#### `GET` `/Branding/Configuration`

- **摘要**：Gets branding configuration
- **鉴权**：无需认证（公开接口）
- **返回类型**：`BrandingOptions`
- **处理服务**：`Emby.Api.BrandingService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetBrandingOptions` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Branding/Css`

- **摘要**：Gets custom css
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.BrandingService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetBrandingCss` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Branding/Css.css`

- **摘要**：Gets custom css
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.BrandingService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetBrandingCss` (`Emby.Api.dll`)

_无请求参数。_

### ConfigurationService（5 条）

#### `GET` `/System/Configuration`

- **摘要**：Gets application configuration
- **鉴权**：需要认证
- **返回类型**：`ServerConfiguration`
- **处理服务**：`Emby.Api.ConfigurationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetConfiguration` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/System/Configuration`

- **摘要**：Updates application configuration
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ConfigurationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AllowLegacyLocalNetworkPassword` | `query` | `bool` | 否 | 否 | - |
| `AutoRunWebApp` | `query` | `bool` | 否 | 否 | - |
| `BannerText` | `query` | `string` | 否 | 否 | - |
| `CachePath` | `query` | `string` | 否 | 否 | - |
| `CertificatePassword` | `query` | `string` | 否 | 否 | - |
| `CertificatePath` | `query` | `string` | 否 | 否 | - |
| `CollapseVideoFolders` | `query` | `bool` | 否 | 否 | - |
| `DashboardSourcePath` | `query` | `string` | 否 | 否 | - |
| `DatabaseAnalysisLimit` | `query` | `int` | 否 | 否 | - |
| `DatabaseCacheSizeMB` | `query` | `int` | 否 | 否 | - |
| `DisableAsyncIO` | `query` | `bool` | 否 | 否 | - |
| `DisableOutgoingIPv6` | `query` | `bool` | 否 | 否 | - |
| `EnableAutomaticRestart` | `query` | `bool` | 否 | 否 | - |
| `EnableAutoUpdate` | `query` | `bool` | 否 | 否 | - |
| `EnableCaseSensitiveItemIds` | `query` | `bool` | 否 | 否 | - |
| `EnableDashboardResponseCaching` | `query` | `bool` | 否 | 否 | - |
| `EnableDebugLevelLogging` | `query` | `bool` | 否 | 否 | - |
| `EnableExternalContentInSuggestions` | `query` | `bool` | 否 | 否 | - |
| `EnableHttps` | `query` | `bool` | 否 | 否 | - |
| `EnableOriginalTrackTitles` | `query` | `bool` | 否 | 否 | - |
| `EnablePeopleLetterSubFolders` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteAccess` | `query` | `bool` | 否 | 否 | - |
| `EnableSavedMetadataForPeople` | `query` | `bool` | 否 | 否 | - |
| `EnableSqLiteMmio` | `query` | `bool` | 否 | 否 | - |
| `EnableUPnP` | `query` | `bool` | 否 | 否 | - |
| `HttpServerPortNumber` | `query` | `int` | 否 | 否 | - |
| `HttpsPortNumber` | `query` | `int` | 否 | 否 | - |
| `ImageExtractionTimeoutMs` | `query` | `int` | 否 | 否 | - |
| `ImageExtractorUpgraded1` | `query` | `bool` | 否 | 否 | - |
| `ImageSavingConvention` | `query` | `ImageSavingConvention` | 否 | 否 | - |
| `IsBehindProxy` | `query` | `bool` | 否 | 否 | - |
| `IsInMaintenanceMode` | `query` | `bool` | 否 | 否 | - |
| `IsPortAuthorized` | `query` | `bool` | 否 | 否 | - |
| `IsRemoteIPFilterBlacklist` | `query` | `bool` | 否 | 否 | - |
| `IsStartupWizardCompleted` | `query` | `bool` | 否 | 否 | - |
| `LibraryMonitorDelaySeconds` | `query` | `int` | 否 | 否 | - |
| `LocalNetworkAddresses` | `query` | `string[]` | 否 | 否 | - |
| `LocalNetworkSubnets` | `query` | `string[]` | 否 | 否 | - |
| `LogAllQueryTimes` | `query` | `bool` | 否 | 否 | - |
| `LogFileRetentionDays` | `query` | `int` | 否 | 否 | - |
| `MaintenanceModeMessage` | `query` | `string` | 否 | 否 | - |
| `MaxAuthDbConnections` | `query` | `int` | 否 | 否 | - |
| `MaxLibraryDatabaseConnections` | `query` | `int` | 否 | 否 | - |
| `MaxOtherDbConnections` | `query` | `int` | 否 | 否 | - |
| `MetadataCountryCode` | `query` | `string` | 否 | 否 | - |
| `MetadataNetworkPath` | `query` | `string` | 否 | 否 | - |
| `MetadataPath` | `query` | `string` | 否 | 否 | - |
| `MigratedLibraryOptionsToDb` | `query` | `bool` | 否 | 否 | - |
| `MigratedToUserItemShares8` | `query` | `bool` | 否 | 否 | - |
| `OptimizeDatabaseOnShutdown` | `query` | `bool` | 否 | 否 | - |
| `PathSubstitutions` | `query` | `PathSubstitution[]` | 否 | 否 | - |
| `PlaylistsUpgradedToM3U` | `query` | `bool` | 否 | 否 | - |
| `PreferredDetectedRemoteAddressFamily` | `query` | `AddressFamily` | 否 | 否 | - |
| `PreferredMetadataLanguage` | `query` | `string` | 否 | 否 | - |
| `ProxyHeaderMode` | `query` | `ProxyHeaderMode` | 否 | 否 | - |
| `PublicHttpsPort` | `query` | `int` | 否 | 否 | - |
| `PublicPort` | `query` | `int` | 否 | 否 | - |
| `RemoteClientBitrateLimit` | `query` | `int` | 否 | 否 | - |
| `RemoteIPFilter` | `query` | `string[]` | 否 | 否 | - |
| `RequireHttps` | `query` | `bool` | 否 | 否 | - |
| `RevertDebugLogging` | `query` | `string` | 否 | 否 | - |
| `RunAtStartup` | `query` | `bool` | 否 | 否 | - |
| `ServerName` | `query` | `string` | 否 | 否 | - |
| `SimultaneousStreamLimit` | `query` | `int` | 否 | 否 | - |
| `SortRemoveWords` | `query` | `string[]` | 否 | 否 | - |
| `TvChannelsRefreshed` | `query` | `bool` | 否 | 否 | - |
| `UICulture` | `query` | `string` | 否 | 否 | - |
| `UninstalledPlugins` | `query` | `string[]` | 否 | 否 | - |
| `VacuumDatabaseOnStartup` | `query` | `bool` | 否 | 否 | - |
| `ValidateImageTags` | `query` | `bool` | 否 | 否 | - |
| `WanDdns` | `query` | `string` | 否 | 否 | - |

#### `POST` `/System/Configuration/Partial`

- **摘要**：Updates application configuration
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ConfigurationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdatePartialConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AllowLegacyLocalNetworkPassword` | `query` | `bool` | 否 | 否 | - |
| `AutoRunWebApp` | `query` | `bool` | 否 | 否 | - |
| `BannerText` | `query` | `string` | 否 | 否 | - |
| `CachePath` | `query` | `string` | 否 | 否 | - |
| `CertificatePassword` | `query` | `string` | 否 | 否 | - |
| `CertificatePath` | `query` | `string` | 否 | 否 | - |
| `CollapseVideoFolders` | `query` | `bool` | 否 | 否 | - |
| `DashboardSourcePath` | `query` | `string` | 否 | 否 | - |
| `DatabaseAnalysisLimit` | `query` | `int` | 否 | 否 | - |
| `DatabaseCacheSizeMB` | `query` | `int` | 否 | 否 | - |
| `DisableAsyncIO` | `query` | `bool` | 否 | 否 | - |
| `DisableOutgoingIPv6` | `query` | `bool` | 否 | 否 | - |
| `EnableAutomaticRestart` | `query` | `bool` | 否 | 否 | - |
| `EnableAutoUpdate` | `query` | `bool` | 否 | 否 | - |
| `EnableCaseSensitiveItemIds` | `query` | `bool` | 否 | 否 | - |
| `EnableDashboardResponseCaching` | `query` | `bool` | 否 | 否 | - |
| `EnableDebugLevelLogging` | `query` | `bool` | 否 | 否 | - |
| `EnableExternalContentInSuggestions` | `query` | `bool` | 否 | 否 | - |
| `EnableHttps` | `query` | `bool` | 否 | 否 | - |
| `EnableOriginalTrackTitles` | `query` | `bool` | 否 | 否 | - |
| `EnablePeopleLetterSubFolders` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteAccess` | `query` | `bool` | 否 | 否 | - |
| `EnableSavedMetadataForPeople` | `query` | `bool` | 否 | 否 | - |
| `EnableSqLiteMmio` | `query` | `bool` | 否 | 否 | - |
| `EnableUPnP` | `query` | `bool` | 否 | 否 | - |
| `HttpServerPortNumber` | `query` | `int` | 否 | 否 | - |
| `HttpsPortNumber` | `query` | `int` | 否 | 否 | - |
| `ImageExtractionTimeoutMs` | `query` | `int` | 否 | 否 | - |
| `ImageExtractorUpgraded1` | `query` | `bool` | 否 | 否 | - |
| `ImageSavingConvention` | `query` | `ImageSavingConvention` | 否 | 否 | - |
| `IsBehindProxy` | `query` | `bool` | 否 | 否 | - |
| `IsInMaintenanceMode` | `query` | `bool` | 否 | 否 | - |
| `IsPortAuthorized` | `query` | `bool` | 否 | 否 | - |
| `IsRemoteIPFilterBlacklist` | `query` | `bool` | 否 | 否 | - |
| `IsStartupWizardCompleted` | `query` | `bool` | 否 | 否 | - |
| `LibraryMonitorDelaySeconds` | `query` | `int` | 否 | 否 | - |
| `LocalNetworkAddresses` | `query` | `string[]` | 否 | 否 | - |
| `LocalNetworkSubnets` | `query` | `string[]` | 否 | 否 | - |
| `LogAllQueryTimes` | `query` | `bool` | 否 | 否 | - |
| `LogFileRetentionDays` | `query` | `int` | 否 | 否 | - |
| `MaintenanceModeMessage` | `query` | `string` | 否 | 否 | - |
| `MaxAuthDbConnections` | `query` | `int` | 否 | 否 | - |
| `MaxLibraryDatabaseConnections` | `query` | `int` | 否 | 否 | - |
| `MaxOtherDbConnections` | `query` | `int` | 否 | 否 | - |
| `MetadataCountryCode` | `query` | `string` | 否 | 否 | - |
| `MetadataNetworkPath` | `query` | `string` | 否 | 否 | - |
| `MetadataPath` | `query` | `string` | 否 | 否 | - |
| `MigratedLibraryOptionsToDb` | `query` | `bool` | 否 | 否 | - |
| `MigratedToUserItemShares8` | `query` | `bool` | 否 | 否 | - |
| `OptimizeDatabaseOnShutdown` | `query` | `bool` | 否 | 否 | - |
| `PathSubstitutions` | `query` | `PathSubstitution[]` | 否 | 否 | - |
| `PlaylistsUpgradedToM3U` | `query` | `bool` | 否 | 否 | - |
| `PreferredDetectedRemoteAddressFamily` | `query` | `AddressFamily` | 否 | 否 | - |
| `PreferredMetadataLanguage` | `query` | `string` | 否 | 否 | - |
| `ProxyHeaderMode` | `query` | `ProxyHeaderMode` | 否 | 否 | - |
| `PublicHttpsPort` | `query` | `int` | 否 | 否 | - |
| `PublicPort` | `query` | `int` | 否 | 否 | - |
| `RemoteClientBitrateLimit` | `query` | `int` | 否 | 否 | - |
| `RemoteIPFilter` | `query` | `string[]` | 否 | 否 | - |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |
| `RequireHttps` | `query` | `bool` | 否 | 否 | - |
| `RevertDebugLogging` | `query` | `string` | 否 | 否 | - |
| `RunAtStartup` | `query` | `bool` | 否 | 否 | - |
| `ServerName` | `query` | `string` | 否 | 否 | - |
| `SimultaneousStreamLimit` | `query` | `int` | 否 | 否 | - |
| `SortRemoveWords` | `query` | `string[]` | 否 | 否 | - |
| `TvChannelsRefreshed` | `query` | `bool` | 否 | 否 | - |
| `UICulture` | `query` | `string` | 否 | 否 | - |
| `UninstalledPlugins` | `query` | `string[]` | 否 | 否 | - |
| `VacuumDatabaseOnStartup` | `query` | `bool` | 否 | 否 | - |
| `ValidateImageTags` | `query` | `bool` | 否 | 否 | - |
| `WanDdns` | `query` | `string` | 否 | 否 | - |

#### `GET` `/System/Configuration/{Key}`

- **摘要**：Gets a named configuration
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`object`
- **处理服务**：`Emby.Api.ConfigurationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetNamedConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Key |

#### `POST` `/System/Configuration/{Key}`

- **摘要**：Updates named configuration
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ConfigurationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateNamedConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Key |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### FeatureService（1 条）

#### `GET` `/Features`

- **摘要**：Gets a list of installed features
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`FeatureInfo[]`
- **处理服务**：`Emby.Api.FeatureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetFeatures` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `FeatureType` | `query` | `FeatureType?` | 否 | 否 | - |

### LocalizationService（4 条）

#### `GET` `/Localization/Countries`

- **摘要**：Gets known countries
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`CountryInfo[]`
- **处理服务**：`Emby.Api.LocalizationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetCountries` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Localization/Cultures`

- **摘要**：Gets known cultures
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`CultureDto[]`
- **处理服务**：`Emby.Api.LocalizationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetCultures` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Localization/Options`

- **摘要**：Gets localization options
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`LocalizatonOption[]`
- **处理服务**：`Emby.Api.LocalizationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetLocalizationOptions` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Localization/ParentalRatings`

- **摘要**：Gets known parental ratings
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`ParentalRating[]`
- **处理服务**：`Emby.Api.LocalizationService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetParentalRatings` (`Emby.Api.dll`)

_无请求参数。_

### PluginService（2 条）

#### `POST` `/Appstore/Register` `[隐藏接口]`

- **摘要**：Registers an appstore sale
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.RegisterAppstoreSale` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Parameters` | `query` | `string` | 是 | 否 | Java representation of parameters to pass through to admin server |

#### `GET` `/Registrations/{Name}` `[隐藏接口]`

- **摘要**：Gets registration status for a feature
- **鉴权**：需要认证
- **返回类型**：`RegistrationInfo`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetRegistration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Feature Name |

### SystemService（16 条）

#### `GET` `/System/Endpoint`

- **摘要**：Gets information about the request endpoint
- **鉴权**：需要认证
- **返回类型**：`EndPointInfo`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetEndpointInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Endpoint` | `query` | `string` | 否 | 否 | - |

#### `GET` `/System/Info`

- **摘要**：Gets information about the server
- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`SystemInfo`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetSystemInfo` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/System/Info/Public`

- **摘要**：Gets public information about the server
- **鉴权**：无需认证（公开接口）
- **返回类型**：`PublicSystemInfo`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetPublicSystemInfo` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/System/Logs` `[隐藏接口]`

- **摘要**：Gets a list of available server log files
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`LogFile[]`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetServerLogs` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/System/Logs/Log` `[隐藏接口]`

- **摘要**：Gets a log file
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetLogFileLegacy` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `query` | `string` | 是 | 是 | The log file name. |

#### `GET` `/System/Logs/Query`

- **摘要**：Gets a list of available server log files
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<LogFile>`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetServerLogsResult` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `GET` `/System/Logs/{Name}`

- **摘要**：Gets a log file
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetLogFile` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `query` | `string` | 是 | 否 | The log file name. |
| `Sanitize` | `query` | `bool` | 否 | 否 | Return sanitized log |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |

#### `GET` `/System/Logs/{Name}/Lines`

- **摘要**：Gets a log file
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<string>`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetLogLines` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `query` | `string` | 是 | 是 | The log file name. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `Limit` | `query` | `int` | 否 | 否 | - |
| `Sanitize` | `query` | `bool` | 否 | 否 | - |
| `StartIndex` | `query` | `int` | 否 | 否 | - |

#### `GET` `/System/Ping`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`void`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.PingSystem` (`Emby.Api.dll`)

_无请求参数。_

#### `HEAD` `/System/Ping`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`void`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.PingSystem` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/System/Ping`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`void`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.PingSystem` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/System/ReleaseNotes`

- **摘要**：Gets release notes
- **鉴权**：需要认证
- **返回类型**：`PackageVersionInfo`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetReleaseNotes` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Version` | `query` | `string` | 否 | 否 | - |

#### `GET` `/System/ReleaseNotes/Versions`

- **摘要**：Gets release notes
- **鉴权**：需要认证
- **返回类型**：`PackageVersionInfo[]`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetReleaseNotesVersions` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/System/Restart`

- **摘要**：Restarts the application, if needed
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.RestartApplication` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/System/Shutdown`

- **摘要**：Shuts down the application
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.ShutdownApplication` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/System/WakeOnLanInfo`

- **摘要**：Gets wake on lan information
- **鉴权**：需要认证
- **返回类型**：`WakeOnLanInfo[]`
- **处理服务**：`Emby.Api.System.SystemService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.System.GetWakeOnLanInfo` (`Emby.Api.dll`)

_无请求参数。_
