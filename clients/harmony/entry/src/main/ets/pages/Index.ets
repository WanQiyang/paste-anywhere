import { common } from '@kit.AbilityKit';
import { ClipboardProbe } from '../platform/ClipboardProbe';
import { RemoteClipboard } from '../services/RemoteClipboard';
import { RemoteUser, RemoteEntry } from '../networking/RemoteTypes';

@Entry
@Component
struct Index {
  @StorageLink('probeText') text: string = '';
  @StorageLink('probeImage') imageUri: string = '';
  @StorageLink('probeType') contentType: string = '';
  @StorageLink('probeStatus') status: string = '';
  @State busy: boolean = false;
  @StorageLink('serverAddress') server: string = '';
  @StorageLink('deviceName') deviceName: string = 'HarmonyOS';
  @StorageLink('accountName') account: string = '';
  @StorageLink('networkStatus') network: string = '';
  @StorageLink('remoteId') remoteId: string = '';
  @StorageLink('remoteUsers') users: RemoteUser[] = [];
  @State invite: string = '';
  @State editing: boolean = false;
  @State history: RemoteEntry[] = [];
  @State historyVisible: boolean = false;

  private async loadHistory(): Promise<void> {
    try { this.history = await RemoteClipboard.history(); this.historyVisible = true; }
    catch (error) { this.status = `读取历史失败：${error.message}`; }
  }

  private async connect(): Promise<void> {
    this.busy = true;
    try {
      await RemoteClipboard.connect(this.server, this.deviceName, this.invite);
      this.invite = ''; this.editing = false;
    } catch (error) { this.status = `连接失败：${error.message}`; }
    finally { this.busy = false; }
  }

  private async upload(): Promise<void> {
    this.busy = true;
    try { await RemoteClipboard.upload(this.text, this.imageUri); }
    catch (error) { this.status = `上传失败：${error.message}`; }
    finally { this.busy = false; }
  }

  private async copy(): Promise<void> {
    this.busy = true;
    try {
      await ClipboardProbe.copy(this.text, this.imageUri);
      this.status = this.imageUri ? '图片写入接口成功；微信/QQ 图片粘贴兼容性未通过，建议使用分享' : '已写入系统剪贴板';
    } catch (error) {
      this.status = `复制失败：${error.message}`;
    } finally {
      this.busy = false;
    }
  }

  private async share(): Promise<void> {
    this.busy = true;
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      await ClipboardProbe.share(context, this.text, this.imageUri, this.contentType);
      this.status = '已调用系统分享面板；请在目标 App 验证内容。';
    } catch (error) {
      this.status = `分享失败：${error.message}`;
    } finally {
      this.busy = false;
    }
  }

  build() {
    Scroll() {
      Column({ space: 20 }) {
        Text('Paste Anywhere').fontSize(28).fontWeight(FontWeight.Bold)
        Text(this.account ? `${this.account} · ${this.network}` : '剪贴板同步 · 请先连接服务器').fontSize(14).fontColor('#596579')
        if (!this.account || this.editing) {
          TextInput({ text: this.server, placeholder: '服务器地址，例如 https://clip.example.com' })
            .onChange((value: string) => { this.server = value; })
          TextInput({ text: this.deviceName, placeholder: '设备名' })
            .onChange((value: string) => { this.deviceName = value; })
          TextInput({ text: this.invite, placeholder: '专属邀请码' }).type(InputType.Password)
            .onChange((value: string) => { this.invite = value; })
          Button('连接并保存').enabled(!this.busy).onClick(() => { void this.connect(); })
        } else {
          Button('连接设置').onClick(() => { this.editing = true; })
        }
        if (this.imageUri) {
          Image(this.imageUri).width('100%').height(280).objectFit(ImageFit.Contain)
            .draggable(this.remoteId.length > 0)
            .onDragStart(() => { RemoteClipboard.dragId = this.remoteId; return { extraInfo: this.remoteId }; })
            .onDragEnd(() => { RemoteClipboard.dragId = ''; })
        } else {
          Text(this.text || '当前没有内容').fontSize(20).width('100%').padding(20).backgroundColor(Color.White).borderRadius(16)
            .draggable(this.remoteId.length > 0)
            .onDragStart(() => { RemoteClipboard.dragId = this.remoteId; return { extraInfo: this.remoteId }; })
            .onDragEnd(() => { RemoteClipboard.dragId = ''; })
        }
        Text(this.contentType).fontSize(12).fontColor('#596579')
        Button(this.imageUri ? '尝试复制图片（兼容性待验证）' : '复制到系统剪贴板').width('100%').enabled(!this.busy && (this.text.length > 0 || this.imageUri.length > 0))
          .onClick(() => { void this.copy(); })
        Button('分享到其他应用').width('100%').enabled(!this.busy)
          .onClick(() => { void this.share(); })
        if (this.account) {
          Button('刷新远程内容').enabled(!this.busy).onClick(() => { void RemoteClipboard.refresh(); })
          Button('历史记录').enabled(!this.busy).onClick(() => { void this.loadHistory(); })
          if (this.historyVisible) {
            ForEach(this.history, (entry: RemoteEntry) => {
              Text(entry.kind === 'image' ? `图片 · ${entry.attachment?.width} × ${entry.attachment?.height}` : entry.text).maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
                .width('100%').padding(8).backgroundColor(Color.White)
                .onClick(() => { void RemoteClipboard.preview(entry); this.historyVisible = false; })
            }, (entry: RemoteEntry) => entry.id)
            Button('收起历史').onClick(() => { this.historyVisible = false; })
          }
          if (!this.remoteId && (this.text || this.imageUri)) {
            Button('重试上传当前内容').enabled(!this.busy).onClick(() => { void this.upload(); })
          }
          Text('长按当前内容，拖到用户即可发送').fontSize(13)
          ForEach(this.users, (user: RemoteUser) => {
            Text(user.name).width('100%').padding(16).backgroundColor('#DDE8FF').borderRadius(12)
              .onDrop(() => { void RemoteClipboard.send(user.id); })
          }, (user: RemoteUser) => user.id)
        }
        Text(this.status).fontSize(15).width('100%')
      }.padding(24).width('100%')
    }.width('100%').height('100%').backgroundColor('#F4F6FA')
  }
}
