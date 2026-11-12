import { UIAbility, Want, AbilityConstant } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { ClipboardProbe } from '../platform/ClipboardProbe';
import { RemoteClipboard } from '../services/RemoteClipboard';
import { RemoteUser } from '../networking/RemoteTypes';

export default class EntryAbility extends UIAbility {
  private foregroundActive: boolean = false;
  onCreate(want: Want, _launchParam: AbilityConstant.LaunchParam): void {
    AppStorage.setOrCreate('probeText', '');
    AppStorage.setOrCreate('probeImage', '');
    AppStorage.setOrCreate('probeType', 'general.plain-text');
    AppStorage.setOrCreate('probeStatus', '请从其他 App 分享文本或单张图片到本应用。');
    AppStorage.setOrCreate('remoteId', '');
    AppStorage.setOrCreate('serverAddress', '');
    AppStorage.setOrCreate('deviceName', 'HarmonyOS');
    AppStorage.setOrCreate('accountName', '');
    AppStorage.setOrCreate('networkStatus', '未连接');
    AppStorage.setOrCreate<RemoteUser[]>('remoteUsers', []);
    if (want.action === 'ohos.want.action.sendData') { RemoteClipboard.incoming(); }
    void this.receive(want);
  }

  onNewWant(want: Want, _launchParam: AbilityConstant.LaunchParam): void {
    if (want.action === 'ohos.want.action.sendData') { RemoteClipboard.incoming(); }
    void this.receive(want);
  }

  private async receive(want: Want): Promise<void> {
    await RemoteClipboard.init(this.context);
    await ClipboardProbe.receive(this.context, want);
  }

  onForeground(): void {
    this.foregroundActive = true;
    void RemoteClipboard.init(this.context).then(() => {
      if (this.foregroundActive) { void RemoteClipboard.foreground(); }
    });
  }

  onBackground(): void { this.foregroundActive = false; RemoteClipboard.background(); }
  onDestroy(): void { this.foregroundActive = false; RemoteClipboard.background(); }

  onWindowStageCreate(windowStage: window.WindowStage): void {
    try {
      windowStage.loadContent('pages/Index', (error) => {
        if (error.code) {
          console.error(`Page load failed: ${error.code}, ${error.message}`);
        }
      });
    } catch (error) {
      console.error(`Page load failed: ${error.message}`);
    }
  }
}
