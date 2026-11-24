# 插件管理（Plugins）

> 所属模块：**06. 同步、通知与插件管理**  
> 路由前缀：`/Plugins`  
> 接口总数：**9** 条

涵盖插件列表、插件配置读写、插件图标与卸载。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Plugins` | 认证 (`Admin`) | `PluginInfo[]` | Gets a list of currently installed plugins |
| 2 | `GET` | `/Plugins/RegistrationRecords/{Name}` *(隐藏)* | 需认证 | `object` | Gets registration status for a feature |
| 3 | `GET` | `/Plugins/SecurityInfo` *(隐藏)* | 认证 (`Admin`) | `PluginSecurityInfo` | Gets plugin registration information |
| 4 | `POST` | `/Plugins/SecurityInfo` *(隐藏)* | 认证 (`Admin`) | `void` | Updates plugin registration information |
| 5 | `DELETE` | `/Plugins/{Id}` | 认证 (`Admin`) | `void` | Uninstalls a plugin |
| 6 | `GET` | `/Plugins/{Id}/Configuration` | 认证 (`Admin`) | `object` | Gets a plugin's configuration |
| 7 | `POST` | `/Plugins/{Id}/Configuration` | 认证 (`Admin`) | `void` | Updates a plugin's configuration |
| 8 | `POST` | `/Plugins/{Id}/Delete` | 认证 (`Admin`) | `void` | Uninstalls a plugin |
| 9 | `GET` | `/Plugins/{Id}/Thumb` | 公开 | `object` | Gets a plugin thumb image |

## 接口详情

### PluginService（9 条）

#### `GET` `/Plugins`

- **摘要**：Gets a list of currently installed plugins
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`PluginInfo[]`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPlugins` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Plugins/RegistrationRecords/{Name}` `[隐藏接口]`

- **摘要**：Gets registration status for a feature
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetRegistrationStatus` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Feature Name |

#### `GET` `/Plugins/SecurityInfo` `[隐藏接口]`

- **摘要**：Gets plugin registration information
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`PluginSecurityInfo`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPluginSecurityInfo` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Plugins/SecurityInfo` `[隐藏接口]`

- **摘要**：Updates plugin registration information
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdatePluginSecurityInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsMBSupporter` | `query` | `bool` | 否 | 否 | - |
| `SupporterKey` | `query` | `string` | 否 | 否 | - |

#### `DELETE` `/Plugins/{Id}`

- **摘要**：Uninstalls a plugin
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UninstallPlugin` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Plugin Id |

#### `GET` `/Plugins/{Id}/Configuration`

- **摘要**：Gets a plugin's configuration
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPluginConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Plugin Id |

#### `POST` `/Plugins/{Id}/Configuration`

- **摘要**：Updates a plugin's configuration
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdatePluginConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Plugin Id |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Plugins/{Id}/Delete`

- **摘要**：Uninstalls a plugin
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UninstallPlugin` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Plugin Id |

#### `GET` `/Plugins/{Id}/Thumb`

- **摘要**：Gets a plugin thumb image
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.PluginService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPluginThumbImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `Guid` | 是 | 否 | Plugin Id |
