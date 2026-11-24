# Web 前端资源、通用 UI 与 OpenAPI/Swagger

> 所属模块：**01. 系统与服务器管理**  
> 路由前缀：`/favicon.ico`, `/openapi`, `/openapi.json`, `/robots.txt`, `/swagger`, `/swagger.json`, `/UI`, `/web`  
> 接口总数：**13** 条

涵盖 Dashboard Web 资源、GenericUI 配置界面、OpenAPI/Swagger 文档入口与静态元数据文件。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/favicon.ico` *(隐藏)* | 公开 | `object` | - |
| 2 | `GET` | `/openapi` | 公开 | `string` | Gets the OpenAPI 3 specifications |
| 3 | `GET` | `/openapi.json` | 公开 | `string` | Gets OpenAPI 3 specifications |
| 4 | `GET` | `/robots.txt` *(隐藏)* | 公开 | `object` | - |
| 5 | `GET` | `/swagger` | 公开 | `string` | Gets the swagger specifications |
| 6 | `GET` | `/swagger.json` | 公开 | `string` | Gets the swagger specifications |
| 7 | `POST` | `/UI/Command` | 认证 (`admin`) | `UIViewInfo` | Execute a command in the context of tv setup |
| 8 | `GET` | `/UI/View` | 认证 (`admin`) | `UIViewInfo` | Gets UI view data |
| 9 | `GET` | `/web/ConfigurationPage` | 公开 | `object` | - |
| 10 | `GET` | `/web/ConfigurationPages` | 需认证 | `List<ConfigurationPageInfo>` | - |
| 11 | `GET` | `/web/strings` | 公开 | `object` | - |
| 12 | `GET` | `/web/stringset` | 公开 | `string[]` | - |
| 13 | `GET` | `/web/{ResourceName*}` *(隐藏)* | 公开 | `object` | - |

## 接口详情

### GenericUIApiService（2 条）

#### `POST` `/UI/Command`

- **摘要**：Execute a command in the context of tv setup
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`UIViewInfo`
- **处理服务**：`Emby.Web.GenericUI.Api.GenericUIApiService` (Emby.Web.GenericUI.dll)
- **Request DTO**：`Emby.Web.GenericUI.Api.Endpoints.RunUICommand` (`Emby.Web.GenericUI.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ClientLocale` | `body` | `string` | 是 | 否 | Locale identifier of the client |
| `CommandId` | `body` | `string` | 是 | 否 | Command id |
| `ItemId` | `body` | `string` | 是 | 否 | Item id |
| `PageId` | `body` | `string` | 是 | 否 | Id of the page controller |
| `Data` | `body` | `string` | 否 | 否 | Command data |

#### `GET` `/UI/View`

- **摘要**：Gets UI view data
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`UIViewInfo`
- **处理服务**：`Emby.Web.GenericUI.Api.GenericUIApiService` (Emby.Web.GenericUI.dll)
- **Request DTO**：`Emby.Web.GenericUI.Api.Endpoints.GetUIViewData` (`Emby.Web.GenericUI.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ClientLocale` | `query` | `string` | 是 | 否 | Locale identifier of the client |
| `PageId` | `query` | `string` | 是 | 否 | Id of the page controller |

### OpenApiService（4 条）

#### `GET` `/openapi`

- **摘要**：Gets the OpenAPI 3 specifications
- **鉴权**：无需认证（公开接口）
- **返回类型**：`string`
- **处理服务**：`Emby.Server.Implementations.Services.OpenApi.OpenApiService` (Emby.Server.Implementations.dll)
- **Request DTO**：`Emby.Server.Implementations.Services.OpenApi.GetOpenApi3Spec` (`Emby.Server.Implementations.dll`)

_无请求参数。_

#### `GET` `/openapi.json`

- **摘要**：Gets OpenAPI 3 specifications
- **鉴权**：无需认证（公开接口）
- **返回类型**：`string`
- **处理服务**：`Emby.Server.Implementations.Services.OpenApi.OpenApiService` (Emby.Server.Implementations.dll)
- **Request DTO**：`Emby.Server.Implementations.Services.OpenApi.GetOpenApi3Spec` (`Emby.Server.Implementations.dll`)

_无请求参数。_

#### `GET` `/swagger`

- **摘要**：Gets the swagger specifications
- **鉴权**：无需认证（公开接口）
- **返回类型**：`string`
- **处理服务**：`Emby.Server.Implementations.Services.OpenApi.OpenApiService` (Emby.Server.Implementations.dll)
- **Request DTO**：`Emby.Server.Implementations.Services.OpenApi.GetOpenApi2Spec` (`Emby.Server.Implementations.dll`)

_无请求参数。_

#### `GET` `/swagger.json`

- **摘要**：Gets the swagger specifications
- **鉴权**：无需认证（公开接口）
- **返回类型**：`string`
- **处理服务**：`Emby.Server.Implementations.Services.OpenApi.OpenApiService` (Emby.Server.Implementations.dll)
- **Request DTO**：`Emby.Server.Implementations.Services.OpenApi.GetOpenApi2Spec` (`Emby.Server.Implementations.dll`)

_无请求参数。_

### WebAppService（7 条）

#### `GET` `/favicon.ico` `[隐藏接口]`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetFavIcon` (`Emby.Web.dll`)

_无请求参数。_

#### `GET` `/robots.txt` `[隐藏接口]`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetRobotsTxt` (`Emby.Web.dll`)

_无请求参数。_

#### `GET` `/web/ConfigurationPage`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetDashboardConfigurationPage` (`Emby.Web.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `query` | `string` | 否 | 否 | - |

#### `GET` `/web/ConfigurationPages`

- **鉴权**：需要认证
- **返回类型**：`List<ConfigurationPageInfo>`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetDashboardConfigurationPages` (`Emby.Web.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnableInMainMenu` | `query` | `bool?` | 否 | 否 | - |
| `EnableInUserMenu` | `query` | `bool?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `PageType` | `query` | `ConfigurationPageType?` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/web/strings`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetStrings` (`Emby.Web.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Locale` | `query` | `string` | 否 | 否 | - |
| `PluginId` | `query` | `Guid` | 否 | 否 | - |

#### `GET` `/web/stringset`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`string[]`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetAllPluginStrings` (`Emby.Web.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `PluginId` | `query` | `Guid` | 否 | 否 | - |

#### `GET` `/web/{ResourceName*}` `[隐藏接口]`

- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Web.Api.WebAppService` (Emby.Web.dll)
- **Request DTO**：`Emby.Web.Api.GetDashboardResource` (`Emby.Web.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ResourceName` | `query` | `string` | 否 | 否 | - |
| `V` | `query` | `string` | 否 | 否 | - |
