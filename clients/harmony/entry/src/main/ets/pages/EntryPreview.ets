import { common } from '@kit.AbilityKit';
import { RemoteEntry } from '../networking/RemoteTypes';
import { RemoteClipboard } from '../services/RemoteClipboard';
import { ClipboardAccess } from '../platform/ClipboardAccess';
import { ImageFiles } from '../platform/ImageFiles';

@CustomDialog
export struct EntryPreview {
  controller?: CustomDialogController;
  onClose: () => void = () => {};
  entry: RemoteEntry = { id: '', userId: '', senderId: '', sourceDeviceId: '', kind: 'text', text: '', createdAt: 0 };
  previewHeight: number = 280;
  @State uri: string = '';
  @State loading: boolean = false;
  @State received: number = 0;
  @State total: number = 0;
  @State error: string = '';
  @State working: boolean = false;
  private closed: boolean = false;

  aboutToAppear(): void { if (this.entry.kind === 'image') { void this.load(); } }
  aboutToDisappear(): void { this.closed = true; ImageFiles.release(this.uri); this.uri = ''; }

  private close(): void {
    this.onClose();
    this.closed = true;
  }

  private async load(): Promise<void> {
    if (this.closed || this.loading || this.working) { return; }
    this.loading = true;
    this.received = 0;
    this.total = this.entry.attachment?.size || 0;
    const reload = this.error.length > 0;
    this.error = '';
    try {
      const content = await RemoteClipboard.prepare(this.entry, (received: number, total: number) => {
        if (!this.closed) { this.received = received; this.total = total; }
      }, reload);
      if (!this.closed) {
        ImageFiles.release(this.uri);
        this.uri = content.uri;
        ImageFiles.retain(this.uri);
      }
    } catch (error) {
      if (!this.closed) { this.error = error.message || '图片加载失败，请重试'; }
    } finally { if (!this.closed) { this.loading = false; } }
  }

  private async act(share: boolean): Promise<void> {
    if (this.closed || this.working || this.loading) { return; }
    this.working = true;
    const available = RemoteClipboard.entryAvailable(this.entry);
    try {
      const content = await RemoteClipboard.prepare(this.entry);
      if (this.closed || !available()) { return; }
      if (share) {
        const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
        await ClipboardAccess.share(context, content.text, content.uri, content.utd);
      } else { await ClipboardAccess.copy(content.text, content.uri, () => !this.closed && available()); }
      if (this.closed) { return; }
      this.getUIContext().getPromptAction().showToast({ message: share ? '已打开分享' : '已复制', duration: 2000 });
      this.close();
    } catch (_) { if (!this.closed) { this.error = '操作失败，请重试'; } }
    finally { this.working = false; }
  }

  build() {
    Column({ space: 16 }) {
      if (this.entry.kind === 'image') {
        Stack() {
          if (this.loading) {
            Column({ space: 12 }) {
              LoadingProgress().width(28).height(28).color('#245CDB')
              Text('正在加载图片…').fontSize(13).fontColor('#7C899D')
              Text(`${Math.floor(this.received / 1024)} / ${Math.ceil(this.total / 1024)} KiB`)
                .fontSize(12).fontColor('#7C899D')
            }
          } else if (this.uri) {
            Image(this.uri).width('100%').height('100%').objectFit(ImageFit.Contain)
              .draggable(false).onError(() => {
                if (!this.closed) {
                  ImageFiles.release(this.uri); this.uri = ''; this.error = '图片无法显示，请重试';
                }
              })
          } else {
            Button('重新加载').fontSize(14).enabled(!this.working).onClick(() => { void this.load(); })
          }
        }.width('100%').height(this.previewHeight).backgroundColor('#F4F6FA').borderRadius(12).clip(true)
      } else {
        Scroll() {
          Text(this.entry.text).fontSize(15).lineHeight(23).fontColor('#34465F')
            .wordBreak(WordBreak.BREAK_ALL).width('100%').padding(14)
        }.width('100%').constraintSize({ maxHeight: this.previewHeight })
          .scrollBar(BarState.Auto).align(Alignment.TopStart).backgroundColor('#F4F6FA').borderRadius(12)
      }
      if (this.error) {
        Text(this.error).fontSize(12).fontColor('#B45353').width('100%')
      }
      Column({ space: 8 }) {
        Button('复制').width('100%').height(40)
          .backgroundColor('#F4F6FA').fontColor('#34465F').enabled(!this.working && !this.loading)
          .onClick(() => { void this.act(false); })
        Button('分享').width('100%').height(40).backgroundColor('#F4F6FA').fontColor('#34465F')
          .enabled(!this.working && !this.loading).onClick(() => { void this.act(true); })
        Button('关闭').width('100%').height(40).backgroundColor(Color.Transparent).fontColor('#64738B')
          .onClick(() => { this.close(); })
      }
    }.padding(24).width('100%')
  }
}
