# Webhooks 推送（Emby.Webhooks）

> 所属模块：**07. 内置插件 API（system/plugins）**  
> 路由前缀：`/Webhooks`  
> 接口总数：**2** 条

涵盖 Webhook 配置与事件触发测试。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Webhooks/Sample` *(隐藏)* | 认证 (`Admin`) | `NotificationSampleInfo` | Gets a notification sample request body |
| 2 | `POST` | `/Webhooks/Test` *(隐藏)* | 公开 | `void` | Receives a notification request, for testing only |

## 接口详情

### NotificationsApi（2 条）

#### `GET` `/Webhooks/Sample` `[隐藏接口]`

- **摘要**：Gets a notification sample request body
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`NotificationSampleInfo`
- **处理服务**：`Emby.Webhooks.NotificationsApi` (Emby.Webhooks.dll)
- **Request DTO**：`Emby.Webhooks.GetSampleJson` (`Emby.Webhooks.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnableMultipartFormData` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Webhooks/Test` `[隐藏接口]`

- **摘要**：Receives a notification request, for testing only
- **鉴权**：无需认证（公开接口）
- **返回类型**：`void`
- **处理服务**：`Emby.Webhooks.NotificationsApi` (Emby.Webhooks.dll)
- **Request DTO**：`Emby.Webhooks.ReceiveNotificationTest` (`Emby.Webhooks.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |
