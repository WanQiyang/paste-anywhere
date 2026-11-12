# 联调与验收

以下操作使用隔离的本地开发账号。已确认双向文本/图片、Windows 发送现有剪贴板及两端跨用户拖动；其余检查项不代表已经通过。

## 本地服务与账号

仓库根目录执行：

```powershell
pwsh -NoProfile -File scripts/check-server.ps1
pwsh -NoProfile -File scripts/start-local-server.ps1
```

监听 `http://127.0.0.1:18080`，创建 `data/local/dev.db`、`ribo` 用户及 `dev-account.json`。重复启动复用账号；识别同一数据库的现有健康服务时直接返回 PID，其他进程占用端口时报错。重新编译后需自行退出旧服务再启动。

客户端使用同一 8 位配对码和不同设备名（如 `PC` / `Phone`），名称仅英文字母、数字，区分大小写。配对码读取：

```powershell
(Get-Content data/local/dev-account.json | ConvertFrom-Json).invite
```

同一账号设备名不可重复；正常登出释放名称，或用 `build/server/windows-amd64/paste-anywhere.exe device delete ribo-PC --db data/local/dev.db` 删除旧设备。CLI 其他用法见 [部署文档](deployment.md)。

## 鸿蒙构建与安装

1. DevEco 打开 `clients/harmony`，同步工程，为手机启用开发者调试并授权电脑。
2. 用 DevEco 项目签名设置配置 Bundle `com.ribo.pasteanywhere` 的证书与设备 Profile。
3. 完整本机配置保存在被忽略的 `clients/harmony/build-profile.local.json5`；公共 `build-profile.json5` 的 `signingConfigs` 保持空数组，不提交密码、证书或 Profile。
4. 执行 `pwsh -NoProfile -File scripts/package-harmony.ps1`，自动合入本机签名并在结束后恢复公共配置。无签名只能用 `build-harmony.ps1` 构建 unsigned HAP，发布脚本会拒绝无签名产物。

默认安装路径的 HDC 命令：

```powershell
$hdc = 'C:\Program Files\Huawei\DevEco Studio\sdk\default\openharmony\toolchains\hdc.exe'
& $hdc list targets
& $hdc -t '<设备连接标识>' install -r dist/PasteAnywhere-0.2.0-harmony-release-signed.hap
& $hdc -t '<设备连接标识>' rport tcp:18080 tcp:18080
```

有 HDC 隧道时手机与 Windows 均填写 `http://127.0.0.1:18080`；HDC 断开后需恢复转发，改变服务 `-Port` 时同步改变转发。公网使用 VPS HTTPS 地址，无需 HDC 隧道。


## 文本与图片往返

先退出旧 Windows 托盘程序，再启动新版，避免两份客户端争抢快捷键：

```powershell
dotnet run --project clients/windows/PasteAnywhere/PasteAnywhere.csproj -c Release
```

| 操作 | 预期 |
| --- | --- |
| Windows 普通复制文字，再按 `Ctrl+Win+Alt+C` | 上传当前快照，手机前台最近 5 条出现文本；普通复制本身不上传 |
| 手机分享文字/网页给本应用 | 自动上传，Windows 在线时可直接 Ctrl+V；检查中文、换行和完整 URL |
| Windows 截图，或复制单个 PNG/JPEG 文件，再按发送快捷键 | 手机出现 `<image>` 卡片；点击“分享”，目标 App 收到图片 |
| 手机相册分享单图给本应用 | 自动上传，Windows 可在画图 Ctrl+V；点击图片卡片也可手动复制 |
| 手机点击文本/图片卡片 | 弹出复制/分享菜单，只有复制写系统剪贴板 |
| 分享上传失败 | 可重试当前内容；读取新分享失败不能误重试上一条内容 |

先验证 PNG/JPEG，再验证需转换的格式。客户端限制 16 MiB / 4000 万像素；转换规则见 [协议](protocol.md)。鸿蒙图片复制 API 成功不代表微信/QQ 等目标支持图片粘贴，优先验证系统分享。

## 跨用户发送

手机保留 `ribo`，创建第二用户：

```powershell
pwsh -NoProfile -File scripts/prepare-peer-account.ps1
(Get-Content data/local/peer-account.json | ConvertFrom-Json).invite
```

Windows 登出后用 `peer` 配对码重新配对。手机保持前台、两端连接灯绿色，在线卡片应出现对方。

1. 任一端普通上传文本/图片，另一个用户不应收到。
2. 将最近 5 条中的任意内容拖到对方用户卡片；手机使用长按。无需确认，对方收到新条目。
3. 两个方向分别验证文本和图片，接收者历史保留，发送者剪贴板不被反向覆盖。
4. 发给自己应创建新记录；离线接收者内容仍保留远端，Windows 重连不自动写本地剪贴板，打开界面可查看。
5. 如需恢复，Windows 登出后使用 `dev-account.json` 的配对码。

## 边界回归清单

- 配对页布局、字段顺序、请求时禁用、两端错误文案及底部 `v 0.2.0`；错误/缺字段响应不保存凭证。
- 多设备在线计数去重；自己始终首位，其他用户离线后移除。
- 5 张卡片及占位、短屏布局；Windows 短按复制、长按浮起、目标高亮、横向滚动、松手/Esc 清理；鸿蒙原生拖动手感。
- 清空通知到全部设备，不改变系统剪贴板和其他用户的转发副本；旧下载不回填，清空后的新内容保留。
- Windows 启动/重连不改本地剪贴板；来源设备上传后再普通复制，不被自身回写覆盖。
- 大图下载期间收到新内容，旧下载不能覆盖；手机冷/热启动连续分享，旧异步处理不能覆盖新分享。
- 手机后台断连、前台刷新；图片缓存可再次分享且按规则清理；多图及不支持类型明确拒绝。
- 登出释放设备名；网络失败保留配置；已撤销凭证仍能本机登出。
- Windows 默认不自启；勾选后真实注销/登录只显示托盘；移动目录后重新保存自启路径。

记录设备/系统、来源与目标 App 版本、操作及错误日志。服务端自动测试覆盖隔离、凭证撤销、通知、清空、转发、历史和附件清理；客户端及 VPS 的真实平台行为需按以上步骤人工确认。
