# Paste Anywhere

面向可信小群体的跨平台远程剪贴板。目标平台：Windows、HarmonyOS NEXT，iOS 后续可选。

## 当前进度

当前支持纯文本与单张图片，三端构建及服务端集成测试通过。双向文本和图片已获用户真机确认；Windows 快捷键现改为发送当前剪贴板。

- `server/`：Go + SQLite + 磁盘图片，用户/设备 CLI、鉴权、条目/历史/转发、WebSocket 通知及附件清理。
- `clients/windows/`：WPF，配对/登出、DPAPI 凭证、发送当前剪贴板快捷键、在线自动接收、托盘、在线用户卡片和最近 5 条拖动发送。
- `clients/harmony/`：分享文本/单图自动上传、前台远程更新、在线用户卡片、最近 5 条预览、主动复制/分享及长按拖动发送。
- `PROJECT_PLAN.md`：需求、技术决策、工具链版本与当前进度。

## 构建

以下命令在仓库根目录使用 PowerShell 执行。

### Go 服务端

安装 Go `1.27.4`：

```powershell
pwsh -NoProfile -File scripts/check-server.ps1
./build/server/windows-amd64/paste-anywhere.exe serve
```

默认监听 `127.0.0.1:8080`。访问 `http://127.0.0.1:8080/healthz` 可检查启动状态。创建账号：

```powershell
./build/server/windows-amd64/paste-anywhere.exe user create ribo
./build/server/windows-amd64/paste-anywhere.exe user list
./build/server/windows-amd64/paste-anywhere.exe device list
./build/server/windows-amd64/paste-anywhere.exe device delete ribo-PC
./build/server/windows-amd64/paste-anywhere.exe user delete ribo
```

用户名和设备名仅允许英文字母、数字，长度 1–128，区分大小写。用户名全局唯一，设备名在同一用户内唯一。创建用户返回 8 位配对码；重复设备名配对返回 409，需换名或先删除旧设备。管理输出为 JSON，不展示内部 ID；无 `reset-code`、安装或卸载指令。

CLI 和服务默认使用当前目录的 `data/paste-anywhere.db`，可设置环境变量 `PASTE_ANYWHERE_DB` 或用 `--db` 指定同一文件（命令行优先，选项可放在名称前后）。历史默认 20 条/7 天，可通过 `serve --history 20 --retention 168h --max-text 1048576` 调整。`--help` 查看帮助。

本地手机/Windows 联调见 [文本同步指南](docs/text-validation.md)，接口见 [协议文档](docs/protocol.md)。公网部署需配置 HTTPS 反向代理。

图片联调见 [图片同步指南](docs/image-validation.md)。默认图片上限 16 MiB / 4000 万像素，服务端文件保存在数据库旁的 `.attachments` 目录。鸿蒙图片优先通过系统分享输出，微信/QQ 图片粘贴兼容性仍待验证。

### Windows

安装 .NET SDK `10.0.404`，根目录 `global.json` 禁止自动选择其他版本。无需 Visual Studio。

```powershell
dotnet build clients/windows/PasteAnywhere.sln --configuration Release
dotnet run --project clients/windows/PasteAnywhere/PasteAnywhere.csproj
```

### HarmonyOS NEXT

安装 DevEco Studio `26.0.0.865` 及配套 SDK：

```powershell
pwsh -NoProfile -File scripts/build-harmony.ps1
```

非默认安装路径通过 `-DevEcoHome '你的安装目录'` 或 `DEVECO_HOME` 环境变量指定。脚本校验工具版本、使用 IDE 内置工具，仅在子进程环境设置路径。

完整操作与真机验收见 [鸿蒙验证指南](docs/harmony-validation.md)。

## 统一编译目录

```text
build/
├── server/
│   ├── windows-amd64/paste-anywhere.exe
│   └── linux-amd64/paste-anywhere
├── windows/
│   ├── Release/net10.0-windows/              # 本机客户端编译输出
│   └── publish/PasteAnywhere-版本-win-x64-standalone/
└── harmony/release/                         # 签名/未签名 HAP、映射文件
dist/                                       # ZIP、tar.gz、签名 HAP 和 SHA-256
data/local/                                 # 本地联调数据库、附件和账号文件
```

`scripts/build-all.ps1` 清理旧输出，执行服务端检查与测试、Windows 客户端 Release 编译及三端发布打包，校验发布包后清理中间目录。默认目标为 Windows x64、Linux amd64 和 HarmonyOS Release。签名 HAP 需要本机已有有效签名配置。

```powershell
pwsh -NoProfile -File scripts/build-all.ps1
```

单独构建时，.NET 中间文件集中在 `build/intermediate/`，鸿蒙工具链暂存于工程内 `entry/build/`、`.hvigor/`，打包暂存于 `build/.staging/`。使用 `scripts/clean-build.ps1` 清除这些中间文件；加 `-IncludeOutputs` 同时删除 `build/` 与 `dist/`。不删除运行数据、签名配置和全局工具缓存。不再使用 `.local/`。

## 跨用户、部署与打包

- [跨用户拖动验收](docs/cross-user-validation.md)：第二个本地测试账号与双向文本/图片发送步骤。
- [VPS 部署](docs/deployment.md)：Docker Compose + Caddy HTTPS，含 CLI 管理与备份。

在仓库根目录打包，产物及 SHA-256 文件写入被 Git 忽略的 `dist/`：

```powershell
pwsh -NoProfile -File scripts/package-windows.ps1
pwsh -NoProfile -File scripts/package-server.ps1
pwsh -NoProfile -File scripts/package-harmony.ps1 -BuildMode release
```

Windows 默认生成自带 .NET 运行时的 x64 ZIP，解压运行即可；可用 `-FrameworkDependent` 生成依赖运行时的小包，或 `-Runtime win-arm64` 指定 ARM64。Linux 默认 amd64，可用 `-Architecture arm64` 切换；包内含预编译程序、运行镜像 Dockerfile、Compose、Caddy 与环境配置示例，不需要在 VPS 安装 Go。当前已实际生成并校验 x64 Windows、amd64 Linux 和 Release 签名 HAP；ARM64 尚未构建验证。

HAP 是否能安装到其他手机取决于签名证书、Profile 和设备资格，Release 构建模式不会自动改变签名分发资格。当前本机使用既有开发者签名配置。
