import { UIAbility, Want, AbilityConstant } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { ClipboardAccess } from '../platform/ClipboardAccess';
import { RemoteClipboard } from '../services/RemoteClipboard';
import { RemoteUser, RemoteEntry } from '../networking/RemoteTypes';

export default class EntryAbility extends UIAbility {
  private foregroundActive: boolean = false;
  onCreate(want: Want, _launchParam: AbilityConstant.LaunchParam): void {
    AppStorage.setOrCreate('operationStatus', '');
    AppStorage.setOrCreate('serverAddress', '');
    AppStorage.setOrCreate('deviceName', '');
    AppStorage.setOrCreate('accountName', '');
    AppStorage.setOrCreate('accountId', '');
    AppStorage.setOrCreate('networkConnected', false);
    AppStorage.setOrCreate('onlineDeviceCount', -1);
    AppStorage.setOrCreate('uploading', false);
    AppStorage.setOrCreate<RemoteEntry[]>('remoteEntries', []);
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
    await ClipboardAccess.receive(this.context, want);
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
