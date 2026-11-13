import { common } from '@kit.AbilityKit';
import { ClientMetadata } from '../networking/ClientMetadata';
import { ClipboardAccess } from '../platform/ClipboardAccess';
import { RemoteClipboard } from '../services/RemoteClipboard';
import { RemoteUser, RemoteEntry } from '../networking/RemoteTypes';

@Entry
@Component
struct Index {
  @StorageLink('localText') text: string = '';
  @StorageLink('localImage') imageUri: string = '';
  @StorageLink('operationStatus') @Watch('statusChanged') status: string = '';
  @State busy: boolean = false;
  @StorageLink('serverAddress') server: string = '';
  @StorageLink('deviceName') deviceName: string = '';
  @StorageLink('accountName') account: string = '';
  @StorageLink('accountId') accountId: string = '';
  @StorageLink('networkConnected') connected: boolean = false;
  @StorageLink('onlineDeviceCount') onlineDevices: number = -1;
  @StorageLink('remoteEntries') entries: RemoteEntry[] = [];
  @StorageLink('pendingUpload') pendingUpload: boolean = false;
  @StorageLink('remoteUsers') users: RemoteUser[] = [];
  @State invite: string = '';
  @State dropUserId: string = '';
  @State loginError: string = '';
  @State focusedField: string = '';
  @State appVersion: string = '';
  @State pageHeight: number = 800;

  aboutToAppear(): void {
    this.appVersion = `v ${ClientMetadata.version()}`;
  }

  private compactLayout(): boolean { return this.pageHeight < 720; }

  private statusChanged(): void {
    if (!this.account || !this.status || this.status.startsWith('正在') || this.status === '暂时无法刷新，请稍后重试') { return; }
    try { this.getUIContext().getPromptAction().showToast({ message: this.status, duration: 2000 }); }
    catch (_) { }
  }

  private canLogin(): boolean {
    return !this.busy && this.server.trim().length > 0 &&
      this.deviceName.trim().length > 0 && this.invite.trim().length > 0;
  }

  private async connect(): Promise<void> {
    if (!this.canLogin()) { return; }
    this.busy = true;
    this.loginError = '';
    try {
      await RemoteClipboard.connect(this.server.trim(), this.deviceName.trim(), this.invite.trim());
      this.invite = '';
    } catch (error) {
      const message: string = error.message || '';
      const description = /^[\u4e00-\u9fff]/.test(message) ? message : '配对未完成。';
      this.loginError = description.startsWith('配对失败') ? description : '配对失败：' + description;
    }
    finally { this.busy = false; }
  }

  private async upload(): Promise<void> {
    if (this.busy) { return; }
    this.busy = true;
    try { await RemoteClipboard.upload(this.text, this.imageUri); }
    catch (error) { this.status = `上传失败：${error.message}`; }
    finally { this.busy = false; }
  }

  private async logout(): Promise<void> {
    if (this.busy) { return; }
    this.busy = true;
    try {
      if (!await this.confirmOperation('确认登出', '登出后将撤销本设备的登录凭证，再次使用需要重新配对。', '登出')) { return; }
      await RemoteClipboard.logout();
      this.invite = ''; this.loginError = ''; this.focusedField = ''; this.dropUserId = '';
    } catch (error) { this.status = '登出失败，请检查连接后重试'; }
    finally { this.busy = false; }
  }
  private async clearClipboard(): Promise<void> {
    if (this.busy || !this.connected || this.entries.length === 0) { return; }
    this.busy = true;
    try {
      if (!await this.confirmOperation('清空在线剪贴板', '将删除当前账号的全部在线内容，无法恢复。本机系统剪贴板不受影响。', '清空')) { return; }
      if (!this.connected || this.entries.length === 0) { return; }
      await RemoteClipboard.clearClipboard();
    }
    catch (_) { this.status = '清空失败，请重试'; }
    finally { this.busy = false; }
  }

  private async confirmOperation(title: string, message: string, action: string): Promise<boolean> {
    try {
      const result = await this.getUIContext().getPromptAction().showDialog({
        title: title, message: message,
        buttons: [{ text: '取消', color: '#64738B' }, { text: action, color: '#D14343' }]
      });
      return result.index === 1;
    } catch (_) { return false; }
  }

  private async manualCopy(): Promise<void> {
    if (this.busy || !this.connected) { return; }
    this.busy = true;
    this.status = '正在读取系统剪贴板…';
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      await ClipboardAccess.captureAndUpload(context);
    } catch (error) { this.status = `手动复制失败：${error.message}`; }
    finally { this.busy = false; }
  }

  private previewText(index: number): string {
    if (index >= this.entries.length) { return '暂无内容'; }
    const entry = this.entries[index];
    return entry.kind === 'image' ? '<image>' : entry.text.slice(0, 240).replace(/[\r\n\t]/g, ' ');
  }
  private entryTime(index: number): string {
    if (index >= this.entries.length) { return ''; }
    const date = new Date(this.entries[index].createdAt);
    return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
  }
  private async entryActions(index: number): Promise<void> {
    if (this.busy || index >= this.entries.length) { return; }
    const entry = this.entries[index];
    let action = -1;
    try {
      const result = await this.getUIContext().getPromptAction().showActionMenu({
        buttons: [{ text: '复制', color: '#245CDB' }, { text: '分享', color: '#245CDB' }]
      });
      action = result.index;
    } catch (_) { return; }
    this.busy = true;
    try {
      const content = await RemoteClipboard.prepare(entry);
      if (action === 0) {
        await ClipboardAccess.copy(content.text, content.uri);
        this.status = entry.kind === 'image' ? '已写入剪贴板；图片粘贴取决于目标应用支持' : '已复制';
      } else {
        const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
        await ClipboardAccess.share(context, content.text, content.uri, content.utd);
        this.status = '已打开分享';
      }
    } catch (error) { this.status = '操作失败，请重试'; }
    finally { this.busy = false; }
  }

  @Builder
  dashboard() {
      Column() {
        Row() {
          Column({ space: 11 }) {
            Text('Paste Anywhere').fontSize(26).fontWeight(FontWeight.Medium).fontColor('#1C2940')
              .maxLines(1).minFontSize(20).maxFontSize(26)
            Row({ space: 8 }) {
              Text('').width(8).height(8).borderRadius(4).backgroundColor(this.connected ? '#22A675' : '#DF6363')
                .accessibilityText(this.connected ? '已连接' : '未连接')
              Text(`${this.account}-${this.deviceName}${this.connected && this.onlineDevices >= 0 ? ` (${this.onlineDevices} 台在线)` : ''}`).fontSize(13).fontColor('#7C899D')
                .maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis }).layoutWeight(1)
            }.width('100%')
          }.alignItems(HorizontalAlign.Start).layoutWeight(1)
          Button({ type: ButtonType.Normal }) {
            Image($r('app.media.logout')).width(22).height(22)
          }.width(40).height(40).borderRadius(10).backgroundColor(Color.Transparent)
            .margin({ left: 12 }).enabled(!this.busy).accessibilityText('登出')
            .onClick(() => { void this.logout(); })
        }.width('100%').alignItems(VerticalAlign.Top)

        Text('在线用户').fontSize(12).fontWeight(FontWeight.Medium).fontColor('#7C899D')
          .width('100%').margin({ top: this.compactLayout() ? 20 : 30, bottom: 12 })
        Scroll() {
          Row({ space: 8 }) {
            ForEach(this.users, (user: RemoteUser) => {
              Column({ space: 6 }) {
                Text(user.name.slice(0, 1).toUpperCase()).fontSize(27).fontWeight(FontWeight.Medium).fontColor('#48658E')
                Text(user.name).fontSize(11).fontColor('#53677F').width('100%').textAlign(TextAlign.Center)
                  .maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
              }.width(this.compactLayout() ? 76 : 86).height(this.compactLayout() ? 76 : 86)
                .justifyContent(FlexAlign.Center).padding(8).borderRadius(12)
                .backgroundColor(this.dropUserId === user.id ? '#DFEAFF' : user.id === this.accountId ? '#EDF3FF' : '#F6F8FC')
                .border({ width: this.dropUserId === user.id ? 2 : 1, color: this.dropUserId === user.id ? '#245CDB' : user.id === this.accountId ? '#D6E3FF' : '#F0F3F8' })
                .accessibilityText(user.name)
                .onDragEnter(() => { if (RemoteClipboard.dragId && this.connected) { this.dropUserId = user.id; } })
                .onDragMove(() => { if (RemoteClipboard.dragId && this.connected) { this.dropUserId = user.id; } })
                .onDragLeave(() => { this.dropUserId = ''; })
                .onDrop(() => { this.dropUserId = ''; if (this.connected) { void RemoteClipboard.send(user.id); } })
            }, (user: RemoteUser) => user.id)
          }
        }.width('100%').height(this.compactLayout() ? 102 : 112)
          .scrollable(ScrollDirection.Horizontal).scrollBar(BarState.Off).align(Alignment.Start)
          .padding(12).backgroundColor(Color.White).borderRadius(18).border({ width: 1, color: '#E7ECF4' })

        Row() {
          Text('在线剪贴板').fontSize(12).fontWeight(FontWeight.Medium).fontColor('#7C899D')
          Blank()
          Button('手动复制', { type: ButtonType.Normal }).fontSize(12).fontColor('#245CDB')
            .backgroundColor(Color.Transparent).height(24).padding({ left: 8, right: 8 })
            .enabled(this.connected && !this.busy)
            .onClick(() => { void this.manualCopy(); })
          Button('清空', { type: ButtonType.Normal }).fontSize(12).fontColor('#D14343')
            .backgroundColor(Color.Transparent).height(24).padding({ left: 8, right: 0 })
            .enabled(this.connected && !this.busy && this.entries.length > 0)
            .onClick(() => { void this.clearClipboard(); })
        }.width('100%').margin({ top: this.compactLayout() ? 18 : 28, bottom: 12 })
        Column({ space: 9 }) {
          ForEach([0, 1, 2, 3, 4], (index: number) => {
            Row({ space: 12 }) {
              Text(`0${index + 1}`).fontSize(11).fontColor('#B1BDCE').width(20)
              Column({ space: 5 }) {
                Text(this.previewText(index)).fontSize(14).fontColor('#34465F').width('100%')
                  .maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
                Text(this.entryTime(index)).fontSize(10).fontColor('#A0ABBC').width('100%')
              }.alignItems(HorizontalAlign.Start).layoutWeight(1)
            }.width('100%').layoutWeight(1).constraintSize({ maxHeight: 62 })
              .padding({ left: 16, right: 16, top: this.compactLayout() ? 6 : 9, bottom: this.compactLayout() ? 6 : 9 }).borderRadius(12)
              .backgroundColor(index < this.entries.length ? Color.White : '#F0F3F8')
              .border({ width: 1, color: index < this.entries.length ? '#E7ECF4' : '#E9EDF4' })
              .opacity(index < this.entries.length ? 1 : 0.6)
              .onClick(() => { void this.entryActions(index); })
              .draggable(this.connected && !this.busy && index < this.entries.length)
              .dragPreviewOptions({
                mode: [DragPreviewMode.ENABLE_DEFAULT_SHADOW, DragPreviewMode.ENABLE_DEFAULT_RADIUS],
                numberBadge: false
              }, { defaultAnimationBeforeLifting: true })
              .onDragStart(() => {
                const id = index < this.entries.length ? this.entries[index].id : '';
                RemoteClipboard.dragId = id;
                return { extraInfo: id };
              })
              .onDragEnd(() => { RemoteClipboard.dragId = ''; this.dropUserId = ''; void RemoteClipboard.refresh(); })
          }, (index: number) => String(index))
        }.width('100%').layoutWeight(1).constraintSize({ maxHeight: 346 })
        if (this.pendingUpload && (this.text || this.imageUri)) {
          Button('重试上传').height(28).fontSize(12).fontColor('#245CDB').backgroundColor(Color.Transparent)
            .enabled(!this.busy).onClick(() => { void this.upload(); })
        }
      }.width('100%').constraintSize({ maxWidth: 520 }).layoutWeight(1)
        .padding({ left: 24, right: 24, top: this.compactLayout() ? 20 : 28, bottom: 12 })
  }

  build() {
    Column() {
    if (!this.account) {
      Scroll() {
        Column() {
          Column({ space: 20 }) {
            Image($r('app.media.app_icon')).width(64).height(64)
              .interpolation(ImageInterpolation.High).draggable(false)
            Text('Paste Anywhere').fontSize(32).fontWeight(FontWeight.Medium)
              .fontColor('#1C2940').width('100%').textAlign(TextAlign.Center)
          }.width('100%').margin({ bottom: 48 })

          Column() {
            Text('服务器地址').fontSize(13).fontWeight(FontWeight.Medium).fontColor('#64738B')
              .width('100%').margin({ bottom: 9 })
            TextInput({ text: this.server, placeholder: 'https://clip.example.com' })
              .height(48).fontSize(15).fontColor('#1C2940').placeholderColor('#A0ABBC')
              .padding({ left: 14, right: 14 }).backgroundColor('#F8FAFD').borderRadius(10)
              .border({ width: 1, color: this.focusedField === 'server' ? '#245CDB' : '#E4EAF3' })
              .caretColor('#245CDB').enabled(!this.busy)
              .onFocus(() => { this.focusedField = 'server'; })
              .onBlur(() => { this.focusedField = ''; })
              .onChange((value: string) => { this.server = value; })

            Text('设备名').fontSize(13).fontWeight(FontWeight.Medium).fontColor('#64738B')
              .width('100%').margin({ top: 22, bottom: 9 })
            TextInput({ text: this.deviceName, placeholder: '英文字母或数字' })
              .height(48).fontSize(15).fontColor('#1C2940').placeholderColor('#A0ABBC')
              .padding({ left: 14, right: 14 }).backgroundColor('#F8FAFD').borderRadius(10)
              .border({ width: 1, color: this.focusedField === 'device' ? '#245CDB' : '#E4EAF3' })
              .caretColor('#245CDB').enabled(!this.busy)
              .onFocus(() => { this.focusedField = 'device'; })
              .onBlur(() => { this.focusedField = ''; })
              .onChange((value: string) => { this.deviceName = value; })

            Text('配对码').fontSize(13).fontWeight(FontWeight.Medium).fontColor('#64738B')
              .width('100%').margin({ top: 22, bottom: 9 })
            TextInput({ text: this.invite, placeholder: '8 位配对码' }).type(InputType.Password)
              .height(48).fontSize(15).fontColor('#1C2940').placeholderColor('#A0ABBC')
              .padding({ left: 14, right: 14 }).backgroundColor('#F8FAFD').borderRadius(10)
              .border({ width: 1, color: this.focusedField === 'invite' ? '#245CDB' : '#E4EAF3' })
              .caretColor('#245CDB').enabled(!this.busy)
              .onFocus(() => { this.focusedField = 'invite'; })
              .onBlur(() => { this.focusedField = ''; })
              .onChange((value: string) => { this.invite = value; })

            Button(this.busy ? '配对中…' : '配对').width('100%').height(48).borderRadius(10)
              .fontSize(16).fontWeight(FontWeight.Medium)
              .margin({ top: 30 }).enabled(this.canLogin())
              .backgroundColor(this.canLogin() ? '#245CDB' : '#E9EEF6')
              .fontColor(this.canLogin() ? '#FFFFFF' : '#9AA6BA')
              .onClick(() => { void this.connect(); })
          }.width('100%').padding(26).backgroundColor(Color.White).borderRadius(20)
            .border({ width: 1, color: '#E7ECF4' })
            .shadow({ radius: 28, color: '#0A273F6A', offsetX: 0, offsetY: 8 })

          Text(this.loginError).fontSize(13).fontColor('#B45353').textAlign(TextAlign.Center)
            .width('100%').constraintSize({ minHeight: 20 }).margin({ top: 20 }).padding({ left: 8, right: 8 })
        }.width('100%').constraintSize({ maxWidth: 440 }).padding({ left: 24, right: 24, top: 40, bottom: 32 })
      }.width('100%').layoutWeight(1).align(Alignment.Center)
    } else {
      this.dashboard()
    }
      Text(this.appVersion).fontSize(11).fontColor('#A1ACBC').height(16).width('100%')
        .textAlign(TextAlign.Center).margin({ top: 4, bottom: 12 })
    }.width('100%').height('100%').backgroundColor('#F4F6FA')
      .onAreaChange((_oldArea: Area, newArea: Area) => {
        const height = Number(newArea.height);
        if (height > 0 && this.pageHeight !== height) { this.pageHeight = height; }
      })
  }
}
