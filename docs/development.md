# 开发指南

## 工具链

开发主机为 Windows x64，脚本使用 PowerShell 7。在仓库根目录执行命令。

| 工具 | 版本 |
| --- | --- |
| Go / GOTOOLCHAIN | 1.27.4 / local |
| .NET SDK / TFM | 10.0.404 / net10.0-windows |
| DevEco Studio / 构建号 | 26.0.0.865 / 261.23567.138.36.2600865 |
| HarmonyOS SDK | 26.0.0.105，最低及目标 API 26 |
| Hvigor / OHPM | 6.26.9 / 26.0.0.630 |
| DevEco Node.js / JBR | 24.14.1 / 25.0.2+1-329.117-nomod |

使用 DevEco 配套 Node/JBR，无需额外安装 Node 或 Visual Studio。默认路径为 `C:\Program Files\Huawei\DevEco Studio`，可通过 `DEVECO_HOME` 或脚本 `-DevEcoHome` 覆盖。依赖版本见各端清单；工具链变更同步脚本与本文。

## 检查与打包

```powershell
pwsh -NoProfile -File scripts/check-server.ps1
dotnet build clients/windows/PasteAnywhere.sln -c Release
pwsh -NoProfile -File scripts/build-harmony.ps1 -BuildMode release
```

服务端脚本执行 vet、测试和 Windows 构建；Go 修改另用 `gofmt` 格式化。全量发布执行：

```powershell
pwsh -NoProfile -File scripts/build-all.ps1
```

全量流程清理、检查、构建、签名打包三端并校验 SHA-256。单独打包使用 `package-server.ps1`、`package-windows.ps1 -FrameworkDependent`、`package-harmony.ps1`。Windows 省略 `-FrameworkDependent` 可携带运行时；Windows `-Runtime win-arm64`、服务端 `-Architecture arm64` 可切换架构，交付前需在目标设备验收。

`VERSION` 控制 Windows 及服务端包版本；鸿蒙 `AppScope/app.json5` 和两个 `oh-package.json5` 版本保持一致，发布递增 `versionCode`。产物在 `build/` 与 `dist/`；`clean-build.ps1` 清理中间文件，`-IncludeOutputs` 同时清理产物，保留运行数据与签名配置。

## 鸿蒙签名与运行

DevEco 打开 `clients/harmony`，为 Bundle `com.ribo.pasteanywhere` 配置证书和设备 Profile。完整本机配置保存为被忽略的 `build-profile.local.json5`；公共配置的 `signingConfigs` 保持空数组。`package-harmony.ps1` 临时合入本机签名，结束后恢复公共配置；无签名可构建 unsigned HAP，但不能生成签名发布包。

手机启用开发者调试并授权电脑后，用 DevEco 运行或用配套 HDC 安装 `dist/` 中的签名 HAP。连接本地服务可执行 `hdc rport tcp:18080 tcp:18080`，手机地址填 `http://127.0.0.1:18080`；公网连接直接使用 HTTPS 域名。

## 本地联调

服务端构建后，新建隔离数据库和两个测试用户，保存命令输出中的配对码：

```powershell
$server = './build/server/windows-amd64/paste-anywhere.exe'
& $server user create Alice --db data/local/dev.db
& $server user create Bob --db data/local/dev.db
& $server serve --db data/local/dev.db --listen 127.0.0.1:18080
```

另开终端运行 `dotnet run --project clients/windows/PasteAnywhere/PasteAnywhere.csproj -c Release`。同账号使用不同设备名检查同步，不同账号检查转发。复用数据库时不重复创建用户；可通过 `user reset` 获取新码并撤销该用户全部设备。一个数据库只运行一个服务，Windows 客户端也只运行一份以避免快捷键冲突。

## 变更检查

- 只针对当前结构与协议开发，不添加历史数据迁移或兼容分支。
- 行为变更更新相关测试；鉴权、事务回滚、同步顺序和数据隔离应有回归覆盖。不要用构建结果代替真机验收。
- 按改动范围执行服务端检查与客户端构建；使用 [验收清单](validation.md) 验证交互与平台能力。
- 数据库、附件、凭证、证书、Profile、签名配置及产物不提交。
