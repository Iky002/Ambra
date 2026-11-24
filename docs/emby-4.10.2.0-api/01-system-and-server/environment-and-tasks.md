# 环境文件系统、计划任务、启动向导与安装包

> 所属模块：**01. 系统与服务器管理**  
> 路由前缀：`/Environment`, `/Packages`, `/ScheduledTasks`, `/Startup`  
> 接口总数：**26** 条

涵盖服务器磁盘/目录浏览、计划任务调度、初始化向导及软件/插件包管理。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Environment/DefaultDirectoryBrowser` | 认证 (`Admin`) | `DefaultDirectoryBrowserInfo` | Gets the parent path of a given path |
| 2 | `GET` | `/Environment/DirectoryContents` | 认证 (`Admin`) | `List<FileSystemEntryInfo>` | Gets the contents of a given directory in the file system |
| 3 | `POST` | `/Environment/DirectoryContents` | 认证 (`Admin`) | `List<FileSystemEntryInfo>` | Gets the contents of a given directory in the file system |
| 4 | `GET` | `/Environment/Drives` | 认证 (`Admin`) | `List<FileSystemEntryInfo>` | Gets available drives from the server's file system |
| 5 | `GET` | `/Environment/NetworkDevices` | 认证 (`Admin`) | `List<FileSystemEntryInfo>` | Gets a list of devices on the network |
| 6 | `GET` | `/Environment/NetworkShares` | 认证 (`Admin`) | `List<FileSystemEntryInfo>` | Gets shares from a network device |
| 7 | `GET` | `/Environment/ParentPath` | 认证 (`Admin`) | `string` | Gets the parent path of a given path |
| 8 | `POST` | `/Environment/ValidatePath` | 认证 (`Admin`) | `void` | Gets the contents of a given directory in the file system |
| 9 | `GET` | `/Packages` | 需认证 | `PackageInfo[]` | Gets available packages |
| 10 | `POST` | `/Packages/Installed/{Name}` | 认证 (`Admin`) | `void` | Installs a package |
| 11 | `DELETE` | `/Packages/Installing/{Id}` | 认证 (`Admin`) | `void` | Cancels a package installation |
| 12 | `POST` | `/Packages/Installing/{Id}/Delete` | 认证 (`Admin`) | `void` | Cancels a package installation |
| 13 | `GET` | `/Packages/Updates` | 认证 (`Admin`) | `PackageVersionInfo[]` | Gets available package updates for currently installed packages |
| 14 | `GET` | `/Packages/{Name}` | 需认证 | `PackageInfo` | Gets a package, by name or assembly guid |
| 15 | `GET` | `/ScheduledTasks` | 认证 (`Admin`) | `TaskInfo[]` | Gets scheduled tasks |
| 16 | `DELETE` | `/ScheduledTasks/Running/{Id}` | 认证 (`Admin`) | `void` | Stops a scheduled task |
| 17 | `POST` | `/ScheduledTasks/Running/{Id}` | 认证 (`Admin`) | `void` | Starts a scheduled task |
| 18 | `POST` | `/ScheduledTasks/Running/{Id}/Delete` | 认证 (`Admin`) | `void` | Stops a scheduled task |
| 19 | `GET` | `/ScheduledTasks/{Id}` | 认证 (`Admin`) | `TaskInfo` | Gets a scheduled task, by Id |
| 20 | `POST` | `/ScheduledTasks/{Id}/Triggers` | 认证 (`Admin`) | `void` | Updates the triggers for a scheduled task |
| 21 | `POST` | `/Startup/Complete` *(隐藏)* | 认证 (`Admin`) | `void` | Reports that the startup wizard has been completed |
| 22 | `GET` | `/Startup/Configuration` *(隐藏)* | 认证 (`Admin`) | `StartupConfiguration` | Gets initial server configuration |
| 23 | `POST` | `/Startup/Configuration` *(隐藏)* | 认证 (`Admin`) | `void` | Updates initial server configuration |
| 24 | `POST` | `/Startup/RemoteAccess` *(隐藏)* | 认证 (`Admin`) | `void` | Updates initial server configuration |
| 25 | `GET` | `/Startup/User` *(隐藏)* | 认证 (`Admin`) | `StartupUser` | Gets initial user info |
| 26 | `POST` | `/Startup/User` *(隐藏)* | 认证 (`Admin`) | `UpdateStartupUserResult` | Updates initial user info |

## 接口详情

### EnvironmentService（8 条）

#### `GET` `/Environment/DefaultDirectoryBrowser`

- **摘要**：Gets the parent path of a given path
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`DefaultDirectoryBrowserInfo`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetDefaultDirectoryBrowser` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Environment/DirectoryContents`

- **摘要**：Gets the contents of a given directory in the file system
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`List<FileSystemEntryInfo>`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetDirectoryContents` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Path` | `query` | `string` | 是 | 否 | - |
| `IncludeDirectories` | `query` | `bool` | 否 | 否 | An optional filter to include or exclude folders from the results. true/false |
| `IncludeFiles` | `query` | `bool` | 否 | 否 | An optional filter to include or exclude files from the results. true/false |
| `Password` | `query` | `string` | 否 | 否 | - |
| `Username` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Environment/DirectoryContents`

- **摘要**：Gets the contents of a given directory in the file system
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`List<FileSystemEntryInfo>`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetDirectoryContents` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Path` | `query` | `string` | 是 | 否 | - |
| `IncludeDirectories` | `query` | `bool` | 否 | 否 | An optional filter to include or exclude folders from the results. true/false |
| `IncludeFiles` | `query` | `bool` | 否 | 否 | An optional filter to include or exclude files from the results. true/false |
| `Password` | `query` | `string` | 否 | 否 | - |
| `Username` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Environment/Drives`

- **摘要**：Gets available drives from the server's file system
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`List<FileSystemEntryInfo>`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetDrives` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Environment/NetworkDevices`

- **摘要**：Gets a list of devices on the network
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`List<FileSystemEntryInfo>`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetNetworkDevices` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Environment/NetworkShares`

- **摘要**：Gets shares from a network device
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`List<FileSystemEntryInfo>`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetNetworkShares` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Path` | `query` | `string` | 是 | 否 | - |

#### `GET` `/Environment/ParentPath`

- **摘要**：Gets the parent path of a given path
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`string`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetParentPath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Path` | `query` | `string` | 是 | 否 | - |

#### `POST` `/Environment/ValidatePath`

- **摘要**：Gets the contents of a given directory in the file system
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.EnvironmentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ValidatePath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Path` | `query` | `string` | 是 | 否 | - |
| `IsFile` | `query` | `bool?` | 否 | 否 | - |
| `Password` | `query` | `string` | 否 | 否 | - |
| `Username` | `query` | `string` | 否 | 否 | - |
| `ValidateWriteable` | `query` | `bool` | 否 | 否 | - |

### PackageService（6 条）

#### `GET` `/Packages`

- **摘要**：Gets available packages
- **鉴权**：需要认证
- **返回类型**：`PackageInfo[]`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPackages` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsAdult` | `query` | `bool?` | 否 | 否 | Optional. Filter by package that contain adult content. |
| `IsAppStoreEnabled` | `query` | `bool?` | 否 | 否 | - |
| `IsPremium` | `query` | `bool?` | 否 | 否 | Optional. Filter by premium status |
| `PackageType` | `query` | `string` | 否 | 否 | Optional package type filter (System/UserInstalled) |
| `TargetSystems` | `query` | `string` | 否 | 是 | Optional. Filter by target system type. Allows multiple, comma delimited. |

#### `POST` `/Packages/Installed/{Name}`

- **摘要**：Installs a package
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.InstallPackage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Package name |
| `AssemblyGuid` | `query` | `string` | 否 | 否 | Guid of the associated assembly |
| `UpdateClass` | `query` | `PackageVersionClass` | 否 | 否 | Optional update class (Dev, Beta, Release). Defaults to Release. |
| `Version` | `query` | `string` | 否 | 否 | Optional version. Defaults to latest version. |

#### `DELETE` `/Packages/Installing/{Id}`

- **摘要**：Cancels a package installation
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.CancelPackageInstallation` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Installation Id |

#### `POST` `/Packages/Installing/{Id}/Delete`

- **摘要**：Cancels a package installation
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.CancelPackageInstallation` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Installation Id |

#### `GET` `/Packages/Updates`

- **摘要**：Gets available package updates for currently installed packages
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`PackageVersionInfo[]`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPackageVersionUpdates` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `PackageType` | `query` | `string` | 是 | 否 | Package type filter (System/UserInstalled) |

#### `GET` `/Packages/{Name}`

- **摘要**：Gets a package, by name or assembly guid
- **鉴权**：需要认证
- **返回类型**：`PackageInfo`
- **处理服务**：`Emby.Api.PackageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPackage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The name of the package |
| `AssemblyGuid` | `query` | `string` | 否 | 否 | The guid of the associated assembly |

### ScheduledTaskService（6 条）

#### `GET` `/ScheduledTasks`

- **摘要**：Gets scheduled tasks
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`TaskInfo[]`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.GetScheduledTasks` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsEnabled` | `query` | `bool?` | 否 | 否 | Optional filter tasks that are enabled, or not. |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional filter tasks that are hidden, or not. |

#### `DELETE` `/ScheduledTasks/Running/{Id}`

- **摘要**：Stops a scheduled task
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.StopScheduledTask` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/ScheduledTasks/Running/{Id}`

- **摘要**：Starts a scheduled task
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.StartScheduledTask` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/ScheduledTasks/Running/{Id}/Delete`

- **摘要**：Stops a scheduled task
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.StopScheduledTask` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | - |

#### `GET` `/ScheduledTasks/{Id}`

- **摘要**：Gets a scheduled task, by Id
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`TaskInfo`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.GetScheduledTask` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/ScheduledTasks/{Id}/Triggers`

- **摘要**：Updates the triggers for a scheduled task
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ScheduledTasks.ScheduledTaskService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ScheduledTasks.UpdateScheduledTaskTriggers` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | - |
| `Capacity` | `query` | `int` | 否 | 否 | - |
| `Count` | `query` | `int` | 否 | 否 | - |
| `Item` | `query` | `TaskTriggerInfo` | 否 | 否 | - |

### StartupWizardService（6 条）

#### `POST` `/Startup/Complete` `[隐藏接口]`

- **摘要**：Reports that the startup wizard has been completed
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ReportStartupWizardComplete` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Startup/Configuration` `[隐藏接口]`

- **摘要**：Gets initial server configuration
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`StartupConfiguration`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetStartupConfiguration` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Startup/Configuration` `[隐藏接口]`

- **摘要**：Updates initial server configuration
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateStartupConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UICulture` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Startup/RemoteAccess` `[隐藏接口]`

- **摘要**：Updates initial server configuration
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateRemoteAccessConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnableAutomaticPortMapping` | `query` | `bool` | 否 | 否 | - |

#### `GET` `/Startup/User` `[隐藏接口]`

- **摘要**：Gets initial user info
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`StartupUser`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetStartupUser` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Startup/User` `[隐藏接口]`

- **摘要**：Updates initial user info
- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`UpdateStartupUserResult`
- **处理服务**：`Emby.Api.StartupWizardService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateStartupUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Password` | `query` | `string` | 否 | 否 | - |
