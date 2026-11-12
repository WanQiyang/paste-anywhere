# 剪贴板同步协议 v1

## HTTP 与身份

公网部署使用 HTTPS；本地开发通过 loopback / HDC 隧道使用 HTTP。设备连接后使用 `Authorization: Bearer <token>`，邀请码仅用于创建新的设备凭证。服务端只保存邀请码和凭证的 SHA-256 摘要。

所有 ID 在 JSON 中编码为十进制字符串（设备 ID 为随机字符串），避免 ArkTS/JavaScript 整数精度丢失。`createdAt` 为 Unix 毫秒。

| 方法 / 路径 | 请求 | 响应 |
| --- | --- | --- |
| GET `/healthz` | 无鉴权 | `{ "status": "ok" }` |
| POST `/v1/connect` | `{ "invite": "...", "deviceName": "..." }` | 201，`{ device: {id,userId,name}, user: {id,name}, token }` |
| GET `/v1/users` | 设备鉴权 | 所有用户的 `{id,name}` 数组 |
| POST `/v1/entries` | `{ "text": "..." }` | 201，新条目，更新自己远程剪贴板 |
| POST `/v1/images` | PNG/JPEG 原始二进制请求体（不使用 Base64 / multipart） | 201，图片条目，更新自己远程剪贴板 |
| GET `/v1/clipboard` | 设备鉴权 | 自己最新有效条目，空时为 JSON `null` |
| GET `/v1/history` | 设备鉴权 | 自己的有效历史，ID 降序 |
| GET `/v1/entries/{id}` | 设备鉴权 | 仅能读取自己的条目 |
| GET `/v1/entries/{id}/image` | 设备鉴权 | 自己有效图片条目的原始二进制，支持 Range |
| POST `/v1/entries/{id}/send` | `{ "userId": "接收者ID" }` | 201，在对方账户下创建新条目 |

条目结构：

```json
{
  "id": "42",
  "userId": "1",
  "senderId": "2",
  "sourceDeviceId": "随机设备ID",
  "kind": "text",
  "text": "示例文本",
  "createdAt": 1794390000000
}
```

图片条目的 `kind` 为 `image`，`text` 为空，增加如下附件元数据：

```json
{
  "attachment": {
    "id": "随机附件标识",
    "name": "image.png",
    "mime": "image/png",
    "size": 12345,
    "width": 800,
    "height": 600
  }
}
```

附件没有公开静态 URL，必须通过拥有该条目的用户凭证下载。转发创建新条目 ID，复用原附件文件。`name`、`mime`、`size` 为以后扩展任意文件预留，目前仅支持图片。

错误为 `{ "error": "说明" }`。401 表示凭证无效，404 表示不可访问或过期条目/目标不存在，400 表示输入无效，413 表示图片字节超限，415 表示不支持的图片格式，500 表示服务端故障。

默认文本上限为 1 MiB UTF-8 字节，图片上限 16 MiB，历史保留 20 条/7 天，可通过 `-max-text`、`-max-image`、`-history`、`-retention` 调整。当前客户端图片上限固定 16 MiB。图片最大 4000 万像素，服务端检查格式、尺寸和完整解码后再入库；图片解码串行执行以限制峰值内存。文本保留空格和换行，不接受空字符串。

图片保存在 `<数据库路径>.attachments/`，数据库仅记录元数据与引用。升级增加 `blobs`、`entry_files` 表，保留旧用户、设备、文本及自增序列。历史超限/过期或用户删除后，清理任务只删除不再被任何条目引用的附件；崩溃遗留的孤立文件超过 1 小时后清理。备份/部署迁移时需同时保留数据库及附件目录。

Windows 发送快捷键读取现有剪贴板，将位图编码为 PNG，也支持已复制的单个 PNG/JPEG 文件；不模拟复制，不自动监听上传。鸿蒙保留 PNG/JPEG 原字节，其余可解码格式转换为质量 95 的 JPEG（不保留动画/透明通道）。鸿蒙下载到应用缓存后再预览、分享；清理超过 7 天或最近 20 个以外的图片缓存，保留当前图片及 5 分钟内新文件。

## 实时通知

以设备鉴权连接 `GET /v1/events`，协议升级为 WebSocket。服务端登记订阅后先发送：

```json
{ "type": "ready" }
```

之后仅推送在线期间发生的更新：

```json
{ "type": "clipboard.updated", "itemId": "42", "sourceDeviceId": "设备ID" }
```

- 按接收用户隔离，上传来源设备不推送；发送给其他用户不会更新发送者的剪贴板。
- 条目提交、通知发布和订阅登记串行化；无离线回放、无客户端 cursor 参数。
- 客户端按 ID 去重，收到更大 ID 后旧下载不得覆盖新内容；相同账户重连保留本次进程的水位。
- Windows 收到通知才下载并写本地剪贴板。启动/重连的 `ready` 不拉取条目；手动刷新 GUI 仅展示，不自动写本地剪贴板。
- 手机仅前台连接，`ready` 后获取最新条目（先订阅再查询避免窗口期丢更新），后台关闭连接。只有复制按钮写剪贴板。
- 每条通知下载失败可提示用户手动刷新；不保证掉线、过载期间投递。慢客户端队列满时关闭连接，不补发。
- 鸿蒙 API 26 使用 `supportOriginPort: true`，避免默认 Origin 丢失非默认端口而被服务端拒绝。服务端仍执行标准同源校验；旧系统兼容性尚待验证。
- 服务端每 20 秒检查已连接设备凭证并发送 ping。撤销设备后 HTTP 立即拒绝，已有 WebSocket 最迟在下次检查时关闭。
- Windows 使用 DPAPI 保存设备配置；鸿蒙存储在应用私有 Preferences。邀请码不持久化到客户端。

## 用户管理

CLI 与服务端使用同一个 SQLite 文件（明确指定 `-db`）。命令为 `user-create`、`user-list`、`user-reset`、`user-delete`、`device-list`、`device-delete`。

重置邀请码会撤销该用户全部旧设备凭证；删除用户同时删除其设备和收到的条目。过去发送给其他用户的条目保留发送者 ID，不联动删除对方内容。
