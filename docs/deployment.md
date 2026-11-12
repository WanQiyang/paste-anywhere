# VPS 部署（Docker Compose + Caddy）

需要 Linux VPS、Docker Engine 与 Compose 插件、指向 VPS 的域名，以及可访问的 80/443 端口。服务端的 Linux 支持不代表提供 Linux 客户端。

## 启动

将仓库上传到 VPS，例如 `/opt/paste-anywhere-src`。在仓库目录执行：

```sh
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env，填写真实 CLIPBOARD_DOMAIN 和 ACME_EMAIL
docker compose --project-directory deploy -f deploy/compose.yaml up -d --build
docker compose --project-directory deploy -f deploy/compose.yaml logs --tail=50 server caddy
```

Caddy 自动申请 HTTPS 证书并代理 HTTP / WebSocket。客户端地址为 `https://你的域名`，无需填写 `/v1`，也无需 HDC 端口转发。服务端 8080 端口仅供容器内部使用。

Compose 使用持久卷 `paste-anywhere_clipboard_data` 保存数据库与附件；Caddy 证书也有独立持久卷。`.env` 中的限制对应服务启动参数，目前客户端图片上限仍为 16 MiB。

## 用户和设备管理

下列命令均在仓库目录执行，使用运行服务的同一个数据库：

```sh
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user-create -db /data/clipboard.db -name ribo
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere user-list -db /data/clipboard.db
docker compose --project-directory deploy -f deploy/compose.yaml exec server /paste-anywhere device-list -db /data/clipboard.db
```

`user-create` 返回用户和邀请码。`user-reset -id 用户ID` 会生成新邀请码并撤销该用户全部旧设备；`device-delete -id 设备ID` 只撤销单台设备。以上命令都需带 `-db /data/clipboard.db`。

## 升级与备份

升级使用同一个目录和 Compose 项目名执行 `up -d --build`，保留数据卷。普通停机使用 `down`，**不要使用 `down -v`，它会删除数据卷**。

需要一致性备份时，先停止写入，再打包整个数据目录（包括 SQLite WAL 及附件）：

```sh
mkdir -p backups
docker compose --project-directory deploy -f deploy/compose.yaml stop server
docker run --rm -v paste-anywhere_clipboard_data:/data:ro -v "$PWD/backups:/backup" alpine:3 tar czf /backup/clipboard-backup.tar.gz -C /data .
docker compose --project-directory deploy -f deploy/compose.yaml start server
```

如修改了 Compose 顶层 `name`，相应调整数据卷名称。恢复时停止服务，将备份解压到该卷并保留文件所有权，再启动。原账号凭证与历史只有在数据库和附件一起恢复时才完整。

## 不使用 Docker

可通过 `scripts/package-server.ps1` 在 Windows 交叉编译静态 Linux 服务端，解压后按包内 README / [原生部署说明](deployment-native.md) 配置 systemd 和 Caddy。

## 验证边界

仓库提供部署配置和可交叉编译的服务端；当前开发机没有 Docker，未实际执行 Docker 构建或 VPS HTTPS 签发。拿到 VPS 环境后需验证镜像拉取、端口、域名解析与证书签发，再进行公网端到端验收。
