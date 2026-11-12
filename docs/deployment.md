# VPS 部署（Docker Compose + Caddy）

需要 Linux VPS、Docker Engine 与 Compose 插件、指向 VPS 的域名，以及可访问的 80/443 端口。服务端的 Linux 支持不代表提供 Linux 客户端。

## 启动

上传与 VPS 架构一致的 `paste-anywhere-版本-linux-amd64.tar.gz`（或 arm64）并解压，在包含 `deploy/` 的解压目录执行以下命令。包内已有静态程序，镜像构建仅复制文件，不需要 Go 环境。也可上传源码仓库，在仓库根目录执行相同命令，由多阶段 Dockerfile 编译。

```sh
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env，填写真实 CLIPBOARD_DOMAIN 和 ACME_EMAIL
docker compose --project-directory deploy -f deploy/compose.yaml up -d --build
docker compose --project-directory deploy -f deploy/compose.yaml logs --tail=50 server caddy
```

Caddy 自动申请 HTTPS 证书并代理 HTTP / WebSocket。客户端地址为 `https://你的域名`，无需填写 `/v1`，也无需 HDC 端口转发。服务端 8080 端口仅供容器内部使用。

Compose 使用持久卷 `paste-anywhere_clipboard_data` 保存数据库与附件；Caddy 证书也有独立持久卷。`.env` 中的限制对应服务启动参数，目前客户端图片上限仍为 16 MiB。

## 用户和设备管理

下列命令均在包含 `deploy/` 的目录执行。容器已设置 `PASTE_ANYWHERE_DB=/data/clipboard.db`，管理指令自动使用与服务相同的数据库：

```sh
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user create ribo
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user list
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere device list
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere device delete ribo-PC
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user delete ribo
```

`user create` 返回用户名和 8 位配对码，例如 `{"user":{"name":"ribo"},"invite":"ABCD2345"}`。用户名和设备名仅允许英文字母、数字，长度 1–128，区分大小写；同一用户设备名唯一，重名配对返回 409。设备列表格式为 `[{"name":"ribo-PC","user":"ribo","device":"PC"}]`。

`device delete` 只撤销指定设备，删除后名称可再次配对。`user delete` 删除用户及其设备、收到的内容；更换配对码需删除重建，不提供重置命令。管理输出不展示内部 ID。

仅支持当前数据库格式，不提供旧数据库、旧名称或旧配对码的兼容和迁移。首次部署使用空数据目录；如果数据卷来自此前测试版本，需清空旧服务端数据后重新创建用户并配对。当前格式的数据可以正常持久化并在重启后继续使用。

数据库、SQLite WAL/SHM 文件和 `clipboard.db.attachments/` 均位于 `/data` 持久卷。日志输出到容器标准输出/错误，通过 Compose 的 `logs` 查看。

## 升级与备份

升级使用同一个目录和 Compose 项目名执行 `up -d --build`，保留数据卷。普通停机使用 `down`，**不要使用 `down -v`，它会删除数据卷**。

客户端与服务端统一使用新版 REST 接口：`POST /v1/devices` 配对、`DELETE /v1/devices/{用户名}-{设备名}` 登出、`GET/POST/DELETE /v1/entries` 查询/上传/清空、`POST /v1/transfers` 转发；用户在线状态由 `onlineDevices` 判断。旧路径已移除，服务端和两端客户端需要同步更新。替换部署包后执行 `up -d --build` 重建容器；本次接口更新不改变当前数据库格式。

需要一致性备份时，先停止写入，再打包整个数据目录（包括 SQLite WAL 及附件）：

```sh
mkdir -p backups
docker compose --project-directory deploy -f deploy/compose.yaml stop server
docker run --rm -v paste-anywhere_clipboard_data:/data:ro -v "$PWD/backups:/backup" alpine:3 tar czf /backup/clipboard-backup.tar.gz -C /data .
docker compose --project-directory deploy -f deploy/compose.yaml start server
```

如修改了 Compose 顶层 `name`，相应调整数据卷名称。恢复时停止服务，将备份解压到该卷并保留文件所有权，再启动。原账号凭证与历史只有在数据库和附件一起恢复时才完整。

## 程序与进程管理

程序仅提供 `serve` 前台运行及用户/设备管理，不包含安装、卸载、后台启停指令。通过 `docker compose --project-directory deploy -f deploy/compose.yaml stop/start/restart` 管理进程（选择其中一个子命令）；Caddy 管理证书和续期。

## 验证边界

仓库提供部署配置和可交叉编译的服务端；当前开发机没有 Docker，未实际执行 Docker 构建或 VPS HTTPS 签发。拿到 VPS 环境后需验证镜像拉取、端口、域名解析与证书签发，再进行公网端到端验收。
