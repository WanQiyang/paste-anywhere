# 跨用户拖动联调

## 准备两个身份

手机保持原来的 `ribo` 账号；Windows 暂时连接第二个用户。先执行：

```powershell
pwsh -NoProfile -File scripts/prepare-peer-account.ps1
(Get-Content .local/peer-account.json | ConvertFrom-Json).invite
```

在 Windows 的邀请码框填第二个邀请码，点击“连接并保存”，地址仍为 `http://127.0.0.1:18080`。两端设备名任意且便于区分。手机点“刷新远程内容”更新用户列表，应看到“联调用户”；Windows 列表应看到 `ribo`。

## 手机 → Windows

1. 手机分享文字/网页或单张图片到本应用，成为自己的远程当前内容。
2. 此时 Windows 属于另一个用户，**不应**自动收到该普通上传。
3. 手机长按当前内容卡片，拖到“联调用户”名字上松手。
4. 应直接发送成功，Windows 自动更新剪贴板，可到记事本/画图粘贴。

## Windows → 手机

1. Windows 普通复制文字或图片，再按发送快捷键上传到“联调用户”的远程剪贴板。
2. 手机仍属于 `ribo`，不应收到这次普通上传。
3. 在 Windows 客户端将当前卡片拖到 `ribo` 名字上。
4. 手机前台应出现新内容，复制或系统分享取用。两端都不弹确认框。

每个方向各测试一次文本和图片。发送后发送者的当前条目不应被对方条目覆盖；接收者的历史里保留之前的内容。接收端离线时内容仍保存在其远程剪贴板；Windows 重连不会自动写本地剪贴板，GUI 手动刷新可查看。

## 恢复同一用户

```powershell
(Get-Content .local/dev-account.json | ConvertFrom-Json).invite
```

将原邀请码填回 Windows 并连接即可。切换账号会生成新的设备凭证，原设备可通过 `device-list` / `device-delete` 清理，不需要删除测试用户才能恢复。

## 验收状态

服务端已自动测试用户隔离、转发新条目、图片文件复用和实时通知。用户已确认客户端真实跨用户拖动操作正确；未单独反馈的离线、历史及其他边界场景不视为全部通过。
