# 图片服务（全局图片入口）

> 所属模块：**04. 音视频流媒体、转码与图片**  
> 路由前缀：`/Images`  
> 接口总数：**1** 条

涵盖 `/Images` 顶级图片接口（注：各资源专属图片如 `/Items/{Id}/Images/...`、`/Users/{Id}/Images/...` 归类在对应资源文档中）。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Images/Remote` | 认证 (`Admin`) | `object` | Gets a remote image |

## 接口详情

### RemoteImageService（1 条）

#### `GET` `/Images/Remote`

- **摘要**：Gets a remote image
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.RemoteImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetRemoteImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ImageUrl` | `query` | `string` | 是 | 否 | The image url |
