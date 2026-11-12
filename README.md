# Paste Anywhere

面向少量用户的跨平台远程剪贴板：Go 服务端、Windows WPF 客户端、HarmonyOS NEXT ArkUI 客户端。支持纯文本和单张图片、同账号多设备同步、跨用户拖动发送。

Windows 用 `Ctrl+Win+Alt+C` 发送当前系统剪贴板；在线新内容自动写入本机剪贴板，普通 `Ctrl+V` 使用。手机通过系统分享上传，前台查看最近 5 条，点击复制/分享。启动或重连不会自动覆盖 Windows 剪贴板。

## 构建 0.2.0

在仓库根目录使用 PowerShell。固定工具链：Go **1.27.4**、.NET SDK **10.0.404**、DevEco Studio **26.0.0.865** 及内置 SDK；完整基线见 [PROJECT_PLAN.md](PROJECT_PLAN.md)。

```powershell
# 检查、测试、构建三端、签名打包、校验 SHA-256
pwsh -NoProfile -File scripts/build-all.ps1
```

鸿蒙签名配置见 [验收指南](docs/validation.md#鸿蒙构建与安装)。DevEco 非默认路径可用 `-DevEcoHome` 或 `DEVECO_HOME` 指定。

产物位于 `dist/`：

- `PasteAnywhere-0.2.0-win-x64-framework-dependent.zip`：Windows 精简包，需要 **.NET 10 Windows Desktop Runtime x64**。
- `paste-anywhere-0.2.0-linux-amd64.tar.gz`：预编译服务端与 Docker Compose / Caddy 配置。
- `PasteAnywhere-0.2.0-harmony-release-signed.hap`：可安装设备范围取决于本机签名 Profile。
- 每个包对应 `.sha256`。

单独构建或打包：

```powershell
pwsh -NoProfile -File scripts/check-server.ps1
dotnet build clients/windows/PasteAnywhere.sln -c Release
pwsh -NoProfile -File scripts/build-harmony.ps1
pwsh -NoProfile -File scripts/package-windows.ps1 -FrameworkDependent
pwsh -NoProfile -File scripts/package-server.ps1
pwsh -NoProfile -File scripts/package-harmony.ps1
```

Windows 打包省略 `-FrameworkDependent` 可生成自带运行时的包。Windows `-Runtime win-arm64`、服务端 `-Architecture arm64` 可选，ARM64 尚未构建验收。

`VERSION` 控制 Windows 编译及服务端包版本；发布时同步鸿蒙 `AppScope/app.json5` 的 `versionName` 并递增 `versionCode`，全量构建会检查一致性。

## 目录与文档

- `server/`、`clients/windows/`、`clients/harmony/`：各端原生实现。
- `deploy/`、`scripts/`：部署配置与构建工具。
- `build/`：各端编译结果；`dist/`：分发包；`data/local/`：本地联调数据（按需创建）。
- [部署](docs/deployment.md)：VPS HTTPS、CLI 管理、备份。
- [Windows 使用](docs/windows-release.md)：快捷键、托盘、自启与升级。
- [联调与验收](docs/validation.md)：本地服务、HDC、签名与三端检查步骤。
- [协议](docs/protocol.md)：HTTP、WebSocket、同步顺序与数据生命周期。
- [项目决策](PROJECT_PLAN.md)：需求、约束、工具链和验收边界。

`scripts/clean-build.ps1` 清理中间文件；加 `-IncludeOutputs` 同时删除 `build/`、`dist/`，保留运行数据与本机签名配置。全量构建会自动清理中间文件。

双向文本、图片、现有剪贴板发送和跨用户拖动已获用户真机确认。Linux/Docker/VPS 公网部署、鸿蒙旧系统及图片粘贴兼容性仍待验证。
