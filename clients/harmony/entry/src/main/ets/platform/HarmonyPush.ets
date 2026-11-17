import { common } from '@kit.AbilityKit';
import { notificationManager } from '@kit.NotificationKit';
import { pushService } from '@kit.PushKit';

export class HarmonyPush {
  static async token(context: common.UIAbilityContext, requestPermission: boolean): Promise<string> {
    try {
      if (!await notificationManager.isNotificationEnabled()) {
        if (requestPermission) {
          try { await notificationManager.requestEnableNotification(context); }
          catch (_) { return ''; }
        }
        if (!await notificationManager.isNotificationEnabled()) { return ''; }
      }
      return await pushService.getToken();
    } catch (_) {
      throw new Error('无法获取推送令牌');
    }
  }
}
