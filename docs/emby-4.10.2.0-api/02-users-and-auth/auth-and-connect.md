# API 密钥认证与 Emby Connect

> 所属模块：**02. 用户、认证、设备与会话**  
> 路由前缀：`/Auth`, `/Connect`  
> 接口总数：**7** 条

涵盖 API Keys、认证提供商与 Emby Connect 账号绑定。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Auth/Keys` | 认证 (`Admin`) | `object` | - |
| 2 | `POST` | `/Auth/Keys` | 认证 (`Admin`) | `void` | - |
| 3 | `DELETE` | `/Auth/Keys/{Key}` | 认证 (`Admin`) | `void` | - |
| 4 | `POST` | `/Auth/Keys/{Key}/Delete` | 认证 (`Admin`) | `void` | - |
| 5 | `GET` | `/Auth/Providers` | 认证 (`Admin`) | `NameIdPair[]` | - |
| 6 | `GET` | `/Connect/Exchange` | 需认证 | `ConnectAuthenticationExchangeResult` | Gets the corresponding local user from a connect user id |
| 7 | `GET` | `/Connect/Pending` | 认证 (`Admin`) | `object` | Creates a Connect link for a user |

## 接口详情

### ConnectService（2 条）

#### `GET` `/Connect/Exchange`

- **摘要**：Gets the corresponding local user from a connect user id
- **鉴权**：需要认证
- **返回类型**：`ConnectAuthenticationExchangeResult`
- **处理服务**：`Emby.Server.Connect.Api.ConnectService` (Emby.Server.Connect.dll)
- **Request DTO**：`Emby.Server.Connect.Api.GetLocalUser` (`Emby.Server.Connect.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ConnectUserId` | `query` | `string` | 是 | 否 | ConnectUserId |

#### `GET` `/Connect/Pending`

- **摘要**：Creates a Connect link for a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Server.Connect.Api.ConnectService` (Emby.Server.Connect.dll)
- **Request DTO**：`Emby.Server.Connect.Api.GetPendingGuests` (`Emby.Server.Connect.dll`)

_无请求参数。_

### SessionsService（5 条）

#### `GET` `/Auth/Keys`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetApiKeys` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `POST` `/Auth/Keys`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.CreateKey` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `App` | `query` | `string` | 是 | 否 | App |

#### `DELETE` `/Auth/Keys/{Key}`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.RevokeKey` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Auth Key |

#### `POST` `/Auth/Keys/{Key}/Delete`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.RevokeKey` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Auth Key |

#### `GET` `/Auth/Providers`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`NameIdPair[]`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetAuthProviders` (`Emby.Api.dll`)

_无请求参数。_
