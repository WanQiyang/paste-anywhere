import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { http, webSocket } from '@kit.NetworkKit';
import { Connection, RemoteEntry, RemoteEvent, RemoteUser, Settings, newer } from '../networking/RemoteTypes';
import { ImageFiles } from '../platform/ImageFiles';

export class RemoteClipboard {
  private static initializing: Promise<void> | undefined;
  private static prefs: preferences.Preferences | undefined;
  private static settings: Settings | undefined;
  private static active: boolean = false;
  private static epoch: number = 0;
  private static socket: webSocket.WebSocket | undefined;
  private static retry: number = -1;
  private static newest: string = '0';
  private static holdLocal: boolean = false;
  private static incomingRevision: number = 0;
  private static cacheDir: string = '';
  private static displayRevision: number = 0;
  static dragId: string = '';

  static init(context: common.UIAbilityContext): Promise<void> {
    if (!RemoteClipboard.initializing) {
      RemoteClipboard.initializing = RemoteClipboard.load(context);
    }
    return RemoteClipboard.initializing;
  }
  private static async load(context: common.UIAbilityContext): Promise<void> {
    RemoteClipboard.cacheDir = context.cacheDir;
    void ImageFiles.prune(context.cacheDir);
    try {
      RemoteClipboard.prefs = await preferences.getPreferences(context, 'connection');
      const saved = await RemoteClipboard.prefs.get('settings', '') as string;
      if (saved) {
        RemoteClipboard.settings = JSON.parse(saved) as Settings;
        AppStorage.setOrCreate('serverAddress', RemoteClipboard.settings.server);
        AppStorage.setOrCreate('deviceName', RemoteClipboard.settings.deviceName);
        AppStorage.setOrCreate('accountName', RemoteClipboard.settings.connection.user.name);
      }
    } catch (error) {
      AppStorage.setOrCreate('probeStatus', `读取连接配置失败：${error.message}`);
    }
  }
  private static async request(server: string, token: string, path: string, method: http.RequestMethod, body?: string | ArrayBuffer): Promise<string> {
    const request = http.createHttp();
    try {
      const result = await request.request(`${server}${path}`, {
        method: method,
        header: { 'Content-Type': typeof body === 'string' || body === undefined ? 'application/json' : 'application/octet-stream', 'Authorization': `Bearer ${token}` },
        extraData: body,
        expectDataType: http.HttpDataType.STRING,
        connectTimeout: 10000,
        readTimeout: 60000
      });
      if (result.responseCode < 200 || result.responseCode >= 300) {
        throw new Error(`HTTP ${result.responseCode}: ${result.result}`);
      }
      return result.result as string;
    } catch (error) {
      throw new Error(`网络请求失败：${error.message}`);
    } finally {
      request.destroy();
    }
  }
  static async connect(server: string, deviceName: string, invite: string): Promise<void> {
    server = server.trim().replace(/\/+$/, '');
    if (!/^https?:\/\/[^?#@\s]+$/.test(server)) {
      throw new Error('请输入 http:// 或 https:// 开头的服务器地址');
    }
    interface ConnectBody { invite: string; deviceName: string; }
    const body: ConnectBody = { invite: invite.trim(), deviceName: deviceName.trim() };
    const connection = JSON.parse(await RemoteClipboard.request(server, '', '/v1/connect', http.RequestMethod.POST, JSON.stringify(body))) as Connection;
    const settings: Settings = { server: server, deviceName: deviceName, connection: connection };
    if (!RemoteClipboard.prefs) { throw new Error('配置存储尚未初始化，请稍后重试'); }
    try {
      await RemoteClipboard.prefs.put('settings', JSON.stringify(settings));
      await RemoteClipboard.prefs.flush();
    } catch (error) { throw new Error(`保存设备凭证失败：${error.message}`); }
    RemoteClipboard.stop();
    ++RemoteClipboard.displayRevision;
    RemoteClipboard.settings = settings;
    RemoteClipboard.newest = '0';
    RemoteClipboard.holdLocal = false;
    AppStorage.setOrCreate('remoteId', '');
    AppStorage.setOrCreate('probeText', '');
    AppStorage.setOrCreate('probeImage', '');
    AppStorage.setOrCreate('accountName', connection.user.name);
    if (RemoteClipboard.active) { await RemoteClipboard.foreground(); }
  }
  static incoming(): void {
    ++RemoteClipboard.incomingRevision;
    ++RemoteClipboard.displayRevision;
    RemoteClipboard.holdLocal = true;
    AppStorage.setOrCreate('remoteId', '');
  }
  static async upload(text: string, uri: string = ''): Promise<void> {
    const revision = RemoteClipboard.incomingRevision;
    const settings = RemoteClipboard.settings;
    if (!settings) { throw new Error('尚未连接服务器，当前内容仅保存在页面中'); }
    interface UploadBody { text: string; }
    const body: UploadBody = { text: text };
    const bytes = uri ? await ImageFiles.uploadBytes(uri) : undefined;
    if (settings !== RemoteClipboard.settings || revision !== RemoteClipboard.incomingRevision) { return; }
    const entry = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token,
      uri ? '/v1/images' : '/v1/entries', http.RequestMethod.POST, bytes || JSON.stringify(body))) as RemoteEntry;
    if (settings !== RemoteClipboard.settings || revision !== RemoteClipboard.incomingRevision) { return; }
    RemoteClipboard.holdLocal = false;
    if (newer(entry.id, RemoteClipboard.newest) || entry.id === RemoteClipboard.newest) {
      RemoteClipboard.newest = entry.id;
      await RemoteClipboard.show(entry);
    } else if (RemoteClipboard.active) {
      await RemoteClipboard.refresh(false);
    }
    AppStorage.setOrCreate('probeStatus', '已上传到自己的远程剪贴板');
  }
  static async foreground(): Promise<void> {
    RemoteClipboard.active = true;
    const settings = RemoteClipboard.settings;
    if (!settings || RemoteClipboard.socket) { return; }
    const epoch = ++RemoteClipboard.epoch;
    try {
      const socket = webSocket.createWebSocket();
      RemoteClipboard.socket = socket;
      socket.on('message', (_error, data: string | ArrayBuffer) => {
        if (epoch !== RemoteClipboard.epoch || !RemoteClipboard.active || typeof data !== 'string') { return; }
        try {
          const event = JSON.parse(data) as RemoteEvent;
          if (event.type === 'ready') {
            console.info('PasteAnywhere WebSocket ready');
            AppStorage.setOrCreate('networkStatus', '前台实时连接已建立');
            void RemoteClipboard.refresh(false);
            void RemoteClipboard.users();
          } else if (event.type === 'clipboard.updated' && event.itemId &&
            event.sourceDeviceId !== settings.connection.device.id && newer(event.itemId, RemoteClipboard.newest)) {
            RemoteClipboard.newest = event.itemId;
            ++RemoteClipboard.displayRevision;
            void RemoteClipboard.download(event.itemId, epoch);
          }
        } catch (error) { AppStorage.setOrCreate('networkStatus', `通知解析失败：${error.message}`); }
      });
      socket.on('close', (_error, result) => {
        RemoteClipboard.disconnected(epoch, `关闭码 ${result.code}：${result.reason}`);
      });
      socket.on('error', (error) => {
        RemoteClipboard.disconnected(epoch, `错误 ${error.code}：${error.message || '握手或网络错误'}`);
      });
      await socket.connect(settings.server.replace(/^http/, 'ws') + '/v1/events', {
        // API 26: retain the URL's port in Origin (otherwise libwebsockets drops it).
        supportOriginPort: true,
        header: { 'Authorization': `Bearer ${settings.connection.token}` }
      });
    } catch (error) {
      RemoteClipboard.disconnected(epoch, `连接失败 ${error.code}：${error.message}`);
    }
  }
  private static disconnected(epoch: number, reason: string): void {
    if (epoch !== RemoteClipboard.epoch) { return; }
    RemoteClipboard.stop();
    if (!RemoteClipboard.active) { return; }
    console.error(`PasteAnywhere WebSocket: ${reason}`);
    AppStorage.setOrCreate('networkStatus', `连接中断，3 秒后重试（${reason}）`);
    RemoteClipboard.retry = setTimeout(() => { RemoteClipboard.retry = -1; void RemoteClipboard.foreground(); }, 3000);
  }
  private static stop(): void {
    ++RemoteClipboard.epoch;
    ++RemoteClipboard.displayRevision;
    if (RemoteClipboard.retry !== -1) { clearTimeout(RemoteClipboard.retry); RemoteClipboard.retry = -1; }
    const socket = RemoteClipboard.socket;
    RemoteClipboard.socket = undefined;
    if (socket) {
      try { socket.close().catch(() => {}); } catch (_) { }
    }
  }
  static background(): void {
    RemoteClipboard.active = false;
    RemoteClipboard.stop();
    AppStorage.setOrCreate('networkStatus', '后台暂停接收');
  }
  private static async show(entry: RemoteEntry): Promise<void> {
    const settings = RemoteClipboard.settings;
    if (!settings || !RemoteClipboard.active) { return; }
    const revision = ++RemoteClipboard.displayRevision;
    let uri = '';
    if (entry.kind === 'image') {
      AppStorage.setOrCreate('probeStatus', '正在下载图片…');
      uri = await ImageFiles.download(settings, entry, RemoteClipboard.cacheDir);
    }
    if (revision !== RemoteClipboard.displayRevision || settings !== RemoteClipboard.settings || !RemoteClipboard.active) { return; }
    AppStorage.setOrCreate('remoteId', entry.id);
    AppStorage.setOrCreate('probeText', entry.text);
    AppStorage.setOrCreate('probeImage', uri);
    AppStorage.setOrCreate('probeType', entry.kind === 'image' ? (entry.attachment?.mime === 'image/png' ? 'general.png' : 'general.jpeg') : 'general.plain-text');
    void ImageFiles.prune(RemoteClipboard.cacheDir);
  }
  static async refresh(manual: boolean = true): Promise<void> {
    const settings = RemoteClipboard.settings;
    if (!settings) { return; }
    if (manual) { RemoteClipboard.holdLocal = false; ++RemoteClipboard.displayRevision; void RemoteClipboard.users(); }
    const epoch = RemoteClipboard.epoch;
    const watermark = RemoteClipboard.newest;
    try {
      const raw = await RemoteClipboard.request(settings.server, settings.connection.token, '/v1/clipboard', http.RequestMethod.GET);
      if (epoch !== RemoteClipboard.epoch || !RemoteClipboard.active || RemoteClipboard.holdLocal) { return; }
      const entry = JSON.parse(raw) as RemoteEntry | null;
      if (!entry) {
        if (RemoteClipboard.newest === watermark) {
          ++RemoteClipboard.displayRevision;
          AppStorage.setOrCreate('remoteId', ''); AppStorage.setOrCreate('probeText', ''); AppStorage.setOrCreate('probeImage', '');
          AppStorage.setOrCreate('probeStatus', '远程剪贴板为空');
        }
      } else if (newer(entry.id, RemoteClipboard.newest) || entry.id === RemoteClipboard.newest) {
        RemoteClipboard.newest = entry.id; await RemoteClipboard.show(entry);
        AppStorage.setOrCreate('probeStatus', '已获取远程内容，点击复制或分享取用');
      }
    } catch (error) { if (epoch === RemoteClipboard.epoch) { AppStorage.setOrCreate('probeStatus', `读取失败：${error.message}`); } }
  }
  private static async download(id: string, epoch: number): Promise<void> {
    const settings = RemoteClipboard.settings;
    if (!settings) { return; }
    try {
      const entry = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token, `/v1/entries/${id}`, http.RequestMethod.GET)) as RemoteEntry;
      if (epoch === RemoteClipboard.epoch && RemoteClipboard.active && !RemoteClipboard.holdLocal && RemoteClipboard.newest === id) {
        await RemoteClipboard.show(entry); AppStorage.setOrCreate('probeStatus', '收到新的远程内容');
      }
    } catch (error) { if (epoch === RemoteClipboard.epoch) { AppStorage.setOrCreate('probeStatus', `下载失败：${error.message}`); } }
  }
  private static async users(): Promise<void> {
    const settings = RemoteClipboard.settings;
    if (!settings) { return; }
    const epoch = RemoteClipboard.epoch;
    try {
      const users = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token, '/v1/users', http.RequestMethod.GET)) as RemoteUser[];
      if (epoch === RemoteClipboard.epoch && RemoteClipboard.active) {
        AppStorage.setOrCreate('remoteUsers', users.filter((user: RemoteUser) => user.id !== settings.connection.user.id));
      }
    } catch (error) { AppStorage.setOrCreate('networkStatus', `用户列表读取失败：${error.message}`); }
  }
  static async history(): Promise<RemoteEntry[]> {
    const settings = RemoteClipboard.settings;
    if (!settings) { return []; }
    return JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token, '/v1/history', http.RequestMethod.GET)) as RemoteEntry[];
  }
  static async preview(entry: RemoteEntry): Promise<void> {
    try {
      await RemoteClipboard.show(entry);
      AppStorage.setOrCreate('probeStatus', '正在查看历史条目；刷新可返回最新内容');
    } catch (error) { AppStorage.setOrCreate('probeStatus', `读取历史图片失败：${error.message}`); }
  }
  static async send(userId: string): Promise<void> {
    const settings = RemoteClipboard.settings;
    const id = RemoteClipboard.dragId;
    if (!settings || !id) { return; }
    interface SendBody { userId: string; }
    const body: SendBody = { userId: userId };
    try {
      await RemoteClipboard.request(settings.server, settings.connection.token, `/v1/entries/${id}/send`, http.RequestMethod.POST, JSON.stringify(body));
      AppStorage.setOrCreate('probeStatus', '已发送，对方远程剪贴板立即更新');
    } catch (error) { AppStorage.setOrCreate('probeStatus', `发送失败：${error.message}`); }
  }
}
