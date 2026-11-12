# 跨用户拖动联调

## 准备两个身份

手机保持原来的 `ribo` 账号；Windows 暂时连接第二个用户。先执行：

```powershell
pwsh -NoProfile -File scripts/prepare-peer-account.ps1
(Get-Content data/local/peer-account.json | ConvertFrom-Json).invite
```

Windows 如已配对，先点击右上角登出，再填写第二个配对码并点击“配对”，地址仍为 `http://127.0.0.1:18080`。两端设备名仅用英文字母、数字，例如 `PC`、`Phone`，同一用户内不能重复。手机保持应用前台，连接灯变绿后，两端在线用户区应显示自己和对方；用户列表每 5 秒刷新。

## 手机 → Windows

1. 手机分享文字/网页或单张图片到本应用，成为自己的远程当前内容。
2. 此时 Windows 属于另一个用户，**不应**自动收到该普通上传。
3. 手机长按最近 5 条中的目标内容卡片，拖到 `peer` 用户卡片上松手。
4. 应直接发送成功，Windows 自动更新剪贴板，可到记事本/画图粘贴。

## Windows → 手机

1. Windows 普通复制文字或图片，再按发送快捷键上传到 `peer` 的远程剪贴板。
2. 手机仍属于 `ribo`，不应收到这次普通上传。
3. 在 Windows 客户端将目标内容卡片拖到 `ribo` 用户卡片上。
4. 手机前台应出现新内容，复制或系统分享取用。两端都不弹确认框。

每个方向各测试一次文本和图片。发送后发送者的当前条目不应被对方条目覆盖；接收者的历史里保留之前的内容。接收端离线时内容仍保存在其远程剪贴板；Windows 重连不会自动写本地剪贴板，GUI 手动刷新可查看。

## 恢复同一用户

```powershell
(Get-Content data/local/dev-account.json | ConvertFrom-Json).invite
```

Windows 点击右上角登出后，填入原 8 位配对码重新配对。正常登出会释放原设备名；若使用此前遗留的同名设备，可通过 `device delete ribo-PC --db data/local/dev.db` 删除，或换用新的设备名。`device list --db data/local/dev.db` 查看所有设备。

## 验收状态

服务端已自动测试用户隔离、转发新条目、图片文件复用和实时通知。用户已确认客户端真实跨用户拖动操作正确；未单独反馈的离线、历史及其他边界场景不视为全部通过。
