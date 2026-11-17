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
pwsh -NoProfile -File scripts/check-public.ps1
pwsh -NoProfile -File scripts/check-server.ps1
pwsh -NoProfile -File scripts/check-windows.ps1
pwsh -NoProfile -File scripts/check-harmony.ps1
pwsh -NoProfile -File scripts/build-harmony.ps1 -BuildMode release
```

服务端检查包含 vet、测试和 Windows 构建。Windows 检查构建客户端，并用隔离剪贴板数据和本地 HTTP 服务验证图片读取、下载、取消及缓存；不会修改系统剪贴板。鸿蒙检查使用 DevEco Node/TypeScript 和模拟文件、网络接口验证下载、缓存及上传会话，不代替 ArkTS 构建或真机验收。Go 修改另用 `gofmt` 格式化。

全量发布：

```powershell
pwsh -NoProfile -File scripts/build-all.ps1
```

全量流程清理、检查、构建、签名打包三端并校验 SHA-256。单独打包使用 `package-server.ps1`、`package-windows.ps1 -FrameworkDependent`、`package-harmony.ps1`。Windows 省略 `-FrameworkDependent` 可携带运行时；Windows `-Runtime win-arm64`、服务端 `-Architecture arm64` 可切换架构，交付前需在目标设备验收。

`VERSION` 控制 Windows 及服务端包版本；鸿蒙 `AppScope/app.json5` 和两个 `oh-package.json5` 版本保持一致，发布递增 `versionCode`。产物在 `build/` 与 `dist/`；`clean-build.ps1` 清理中间文件，`-IncludeOutputs` 同时清理产物，保留运行数据与签名配置。

鸿蒙打包同时输出 `*-signed.hap` 和 `*-unsigned.hap` 及各自 SHA-256。GitHub Release 使用未签名 HAP，下载者须自行签名安装；签名 HAP 用于对应证书/Profile 允许的设备。

## 鸿蒙签名与运行

DevEco 打开 `clients/harmony`，为 Bundle `com.ribo.pasteanywhere` 配置证书和设备 Profile。完整本机配置保存为被忽略的 `build-profile.local.json5`；本机配置的每个产品必须通过 `signingConfig` 引用 `signingConfigs` 中存在的名称。公共配置的 `signingConfigs` 保持空数组且产品不引用签名。`package-harmony.ps1` 临时合入本机签名，结束后恢复公共配置；无签名可构建 unsigned HAP，但不能生成签名发布包。

手机启用开发者调试并授权电脑后，用 DevEco 运行或用配套 HDC 安装 `dist/` 中的签名 HAP。连接本地服务可执行 `hdc rport tcp:18080 tcp:18080`，手机地址填 `http://127.0.0.1:18080`；公网连接直接使用 HTTPS 域名。

剪贴板读取仅在系统 `PasteButton` 授权成功后执行，无需 `READ_PASTEBOARD` ACL；按钮样式须满足安全组件约束。

Push Kit 使用系统 `@kit.PushKit` 与 `@kit.NotificationKit`，无第三方 SDK。`module.json5` 配置 `client_id=6917621976564647397`，签名公钥指纹需登记到同一个 AGC 应用。服务端本地联调前设置 `HUAWEI_PUSH_KEY_FILE` 为服务账号 JSON 路径，详情见 [部署](deployment.md#push-kit可选)。自动测试使用临时生成的 RSA 密钥和本地 HTTP 模拟响应，不读取真实推送私钥、不调用华为。

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

- 行为变更更新相关测试和当前约定；保留鉴权、事务、同步顺序、数据隔离及平台边界的回归覆盖。
- 按改动范围执行服务端检查与客户端构建；使用 [验收清单](validation.md) 验证交互与平台能力。
- 数据库、附件、凭证、证书、Profile、签名配置及产物不提交。
- `check-public.ps1` 检查已跟踪/未忽略文件及 Git 暂存内容中的敏感路径、常见私钥和密码模式，输出只含文件名；全量构建首先执行该检查。它不能替代人工审查。服务账号 JSON 放在被忽略的 `secrets/` 或仓库外。App ID / Client ID 和包名是公开应用标识，不是鉴权秘密。
