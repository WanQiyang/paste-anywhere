# Paste Anywhere

面向少量互相信任用户的在线剪贴板：Go 服务端、Windows WPF 客户端、HarmonyOS NEXT 原生客户端。支持纯文本、单张图片、同账号多设备同步和跨用户拖动发送。

## 使用

1. 按 [部署说明](docs/deployment.md) 启动服务，用 `user create <用户名>` 获取配对码。
2. 客户端填写服务器 HTTPS 地址、设备名和配对码。同一用户设备名不能重复。
3. 两端点击“粘贴”上传当前系统剪贴板；Windows 也可按 `Ctrl+Win+Alt+C`，鸿蒙也可从其他应用分享文本或单图。
4. 查看最近 5 条内容，点击卡片预览文本或图片，再选择复制（鸿蒙还可分享），拖到用户卡片发送。长文本可滚动，图片等比显示。普通复制本身不上传。

Windows 在线收到新通知会自动写入系统剪贴板，启动/重连不会覆盖本地内容。鸿蒙仅前台连接接收，点击系统“粘贴”按钮临时授权读取。登出和清空均需确认；清空不修改系统剪贴板。

## 安装与构建

- Windows：解压运行 `PasteAnywhere.exe`，精简包需 .NET 10 Windows Desktop Runtime x64。详见 [Windows 使用](docs/windows-release.md)。
- HarmonyOS NEXT：API 26；HAP 需本机签名，安装设备范围由签名 Profile 决定。
- 源码构建：见 [开发指南](docs/development.md)。版本以 `VERSION` 为准；完成打包后分发包及 SHA-256 校验文件位于 `dist/`。

## 仓库

- `server/`、`clients/windows/`、`clients/harmony/`：三端源码。
- `deploy/`、`scripts/`：部署与构建工具。
- `build/`、`dist/`、`data/local/`：编译、分发、本地运行数据，不提交。
- [协议](docs/protocol.md)：HTTP、WebSocket、数据生命周期。
- [验收清单](docs/validation.md)：当前功能的人工检查项。
- [开发基线](PROJECT_PLAN.md)：功能约定与增量开发约束。
