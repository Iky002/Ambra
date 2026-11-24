# 配置备份与还原（MBBackup）

> 所属模块：**07. 内置插件 API（system/plugins）**  
> 路由前缀：`/BackupRestore`  
> 接口总数：**3** 条

涵盖 Emby 服务器配置备份信息查询、恢复操作与备份历史。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/BackupRestore/BackupInfo` | 认证 (`Admin`) | `AllBackupsInfo` | - |
| 2 | `POST` | `/BackupRestore/Restore` | 认证 (`Admin`) | `void` | - |
| 3 | `POST` | `/BackupRestore/RestoreData` | 认证 (`Admin`) | `void` | - |

## 接口详情

### BackupApi（3 条）

#### `GET` `/BackupRestore/BackupInfo`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`AllBackupsInfo`
- **处理服务**：`MBBackup.Api.BackupApi` (MBBackup.dll)
- **Request DTO**：`MBBackup.Api.GetBackupInfo` (`MBBackup.dll`)

_无请求参数。_

#### `POST` `/BackupRestore/Restore`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`MBBackup.Api.BackupApi` (MBBackup.dll)
- **Request DTO**：`MBBackup.Api.RestoreFromBackup` (`MBBackup.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `RestoreServerId` | `query` | `bool` | 否 | 否 | - |
| `UseFiles` | `query` | `string` | 否 | 否 | - |

#### `POST` `/BackupRestore/RestoreData`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`MBBackup.Api.BackupApi` (MBBackup.dll)
- **Request DTO**：`MBBackup.Api.RestoreDataFromBackup` (`MBBackup.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Users` | `query` | `UserRestoreInfo[]` | 否 | 否 | - |
