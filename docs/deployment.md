# 服务端部署

需要 Linux VPS、Docker Engine/Compose、指向 VPS 的域名和开放的 80/443 端口。上传对应架构的服务端包并解压，或使用源码仓库；以下命令均在包含 `deploy/` 的目录执行。

## 启动与配置

```sh
cp deploy/.env.example deploy/.env
# 填写 CLIPBOARD_DOMAIN、ACME_EMAIL
docker compose --project-directory deploy -f deploy/compose.yaml up -d --build
docker compose --project-directory deploy -f deploy/compose.yaml logs --tail=50 server caddy
```

分发包使用预编译程序，源码仓库使用多阶段编译。Caddy 自动签发 HTTPS 证书并代理 HTTP/WebSocket；客户端填写 `https://你的域名`。8080 不对外发布。

| `.env` 配置 | 默认值 / 含义 |
| --- | --- |
| `CLIPBOARD_DOMAIN` | 必填，服务域名 |
| `ACME_EMAIL` | 证书联系邮箱 |
| `HISTORY_LIMIT` / `RETENTION` | 20 条 / 168h |
| `MAX_TEXT_BYTES` / `MAX_IMAGE_BYTES` | 1048576 / 16777216 |
| `DISPLAY_TIMEZONE` | Asia/Shanghai，CLI 时间显示时区 |

修改 `.env` 后执行 `up -d` 更新容器环境。客户端图片上限为 16 MiB，服务端像素上限为 4000 万。

## 用户与设备

容器设置 `PASTE_ANYWHERE_DB=/data/clipboard.db`，CLI 自动使用同一数据库。命令前缀为：

```sh
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user create Alice
```

将上例的 `user create Alice` 替换为所需操作：

| 命令 | 行为 |
| --- | --- |
| `user create Alice` | 创建用户，返回 `{ "user": { "name": "Alice" }, "invite": "配对码" }` |
| `user list` | 用户表格 |
| `user reset Alice` | 原子重置配对码并删除该用户全部设备，保留账号及在线内容，返回新码 |
| `user delete Alice` | 删除用户、其设备和收到的内容 |
| `device list` | 设备表格 |
| `device delete Alice-PC` | 撤销设备，释放设备名 |

用户名和设备名仅英文字母、数字，1–128 字符，区分大小写；设备名在用户内唯一。管理输出不展示内部 ID。配对码仅创建/重置时返回，服务端保存摘要。重置后的旧码及设备凭证失效，已连接 WebSocket 最迟在下一次凭证检查（每 20 秒）关闭。

- 用户列：用户名、设备数、在线设备数、最近内容时间、创建时间。
- 设备列：设备标识、平台、客户端版本、在线状态、最近 IP、最后活跃时间、配对时间。
- 行按用户名、设备名先忽略大小写排序，再以大小写打破平局；数字按文本排序。
- 时间如 `2026-11-13 18:30:00 (+8)`，支持 IANA 时区及夏令时；缺失信息为 `—`。最近内容时间只统计未过期保留内容。
- 一个数据库对应一个服务进程。在线快照每秒更新，超过 10 秒未更新或正常停服后为“未知”，同设备多连接去重。最后活跃记录已鉴权请求及成功 WebSocket ping。
- 平台/版本由客户端上报。Compose 设置 `TRUST_PROXY=true`，Caddy 覆盖 `X-Real-IP` 为连接来源；直接运行默认使用 TCP 对端 IP，仅在可信代理覆盖该头时启用代理模式。

直接运行 CLI 时可用 `--db` 或 `PASTE_ANYWHERE_DB` 指定数据库，用 `DISPLAY_TIMEZONE` 设置时区，`--help` 查看参数。

## 数据与进程

只支持空数据库或当前结构 `user_version=3`，不迁移其他格式。SQLite、WAL/SHM 与附件目录均保存在 `paste-anywhere_clipboard_data` 持久卷；Caddy 证书单独持久化。

使用 Compose `stop/start/restart` 管理容器，`up -d --build` 重建程序；`down` 保留数据，`down -v` 删除数据卷。

一致性备份需停止写入并保存整个数据目录：

```sh
mkdir -p backups
docker compose --project-directory deploy -f deploy/compose.yaml stop server
docker run --rm -v paste-anywhere_clipboard_data:/data:ro -v "$PWD/backups:/backup" alpine:3 tar czf /backup/clipboard-backup.tar.gz -C /data .
docker compose --project-directory deploy -f deploy/compose.yaml start server
```

恢复时停止服务，将当前格式备份解压到数据卷并保留文件所有权，再启动；数据库和附件必须一起恢复。更改 Compose 项目名时，相应更改卷名。

## Push Kit（可选）

1. AGC 应用 `com.ribo.pasteanywhere` 开通推送服务、自分类「工作事项提醒」（`WORK`），配置实际安装包的签名公钥指纹。工程 Client ID 为 `6917621976564647397`。
2. 在开发者联盟 API Console 创建应用所属项目的服务账号凭证，选择具备推送权限的「开发」角色，生成并下载 JSON。服务端需要 `project_id`、`key_id`、`sub_account`、PKCS8 RSA `private_key`。接口 URL 使用项目 ID，不是 App ID；JWT 使用 PS256，服务器时间需要准确。
3. 将 JSON 放在服务器私有目录，在 `deploy/.env` 设置 `HUAWEI_PUSH_KEY_HOST_FILE=/绝对路径/huawei-push.json`。容器以 UID/GID `65532:65532` 运行，文件需允许该用户读取（例如文件所有者设为 `65532:65532`，权限 `0400`）。仅服务端读取，不放入客户端或仓库。
4. 启用附加 Compose 配置：

```sh
docker compose --project-directory deploy -f deploy/compose.yaml -f deploy/compose.push.yaml up -d --build
```

后续更新容器时继续带上两个 `-f`。密钥只读挂载到 `/run/secrets/huawei-push.json`，镜像包含用于校验华为 HTTPS 连接的系统根证书。启动日志出现 `HarmonyOS Push Kit enabled` 表示配置已加载，不代表真机推送已验证。密钥无效时启动失败；更新密钥后重启服务。直接运行程序可设置 `HUAWEI_PUSH_KEY_FILE` 为程序可读取的文件路径，不设置则关闭发送功能。

手机配对并允许通知后自动登记 Push Token；可在系统设置修改通知权限，回到应用重新同步。发送失败日志记录 HTTP 状态及华为业务响应码，不打印令牌、私钥或剪贴板正文。鉴权/权益错误应核对项目、角色、`WORK` 权益及系统时间；推送成功仅表示华为接收请求，实际送达需真机验证。
