# Linux 原生服务端安装

此压缩包包含静态 Go 服务端、systemd 单元与 Caddy 配置示例。选择与 VPS 架构一致的 `linux-amd64` 或 `linux-arm64` 包。

在解压目录中，以具有 sudo 权限的账号执行：

```sh
sudo useradd --system --home-dir /var/lib/paste-anywhere --shell /usr/sbin/nologin paste-anywhere
sudo install -d /opt/paste-anywhere
sudo install -m 755 paste-anywhere /opt/paste-anywhere/paste-anywhere
sudo install -m 644 paste-anywhere.service /etc/systemd/system/paste-anywhere.service
sudo systemctl daemon-reload
sudo systemctl enable --now paste-anywhere
```

账号已存在时跳过 `useradd`。systemd 自动创建 `/var/lib/paste-anywhere`，数据库为 `clipboard.db`，附件目录为 `clipboard.db.attachments`。

创建用户及查看日志：

```sh
sudo -u paste-anywhere /opt/paste-anywhere/paste-anywhere user-create -db /var/lib/paste-anywhere/clipboard.db -name ribo
sudo journalctl -u paste-anywhere -n 50 --no-pager
curl http://127.0.0.1:8080/healthz
```

安装 Caddy 后，参考 `Caddyfile.native.example` 将真实域名反向代理到 `127.0.0.1:8080`，域名解析到 VPS 并开放 80/443。客户端使用该域名的 HTTPS 地址。Caddy 自动代理 WebSocket 升级。

升级先 `sudo systemctl stop paste-anywhere`，再用 `install -m 755` 覆盖二进制并启动服务；用户数据位于独立的 `/var/lib/paste-anywhere`，不要删除该目录。备份时停服并打包整个数据目录，包括 WAL 文件和附件。
