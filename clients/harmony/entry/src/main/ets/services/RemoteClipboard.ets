import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { http, webSocket } from '@kit.NetworkKit';
import { Connection, RemoteEntry, RemoteEvent, RemoteUser, Settings, PreparedContent, newer } from '../networking/RemoteTypes';
import { ImageFiles } from '../platform/ImageFiles';
import { ClientMetadata } from '../networking/ClientMetadata';

export class RemoteClipboard {
  private static initializing: Promise<void> | undefined;
  private static prefs: preferences.Preferences | undefined;
  private static settings: Settings | undefined;
  private static active: boolean = false;
  private static epoch: number = 0;
  private static socket: webSocket.WebSocket | undefined;
  private static retry: number = -1;
  private static poll: number = -1;
  private static newest: string = '0';
  private static clearedThrough: string = '0';
  private static incomingRevision: number = 0;
  private static uploads: number = 0;
  private static dashboardRevision: number = 0;
  private static refreshing: boolean = false;
  private static refreshEpoch: number = -1;
  private static refreshAgain: boolean = false;
  private static loggingOut: boolean = false;
  private static cacheDir: string = '';
  static dragId: string = '';

  static init(context: common.UIAbilityContext): Promise<void> {
    if (!RemoteClipboard.initializing) { RemoteClipboard.initializing = RemoteClipboard.load(context); }
    return RemoteClipboard.initializing;
  }
  private static async load(context: common.UIAbilityContext): Promise<void> {
    RemoteClipboard.cacheDir = context.cacheDir;
    void ImageFiles.prune(context.cacheDir);
    try {
      RemoteClipboard.prefs = await preferences.getPreferences(context, 'connection');
      const saved = await RemoteClipboard.prefs.get('settings', '') as string;
      if (saved) {
        const settings = JSON.parse(saved) as Settings;
        RemoteClipboard.settings = settings;
        RemoteClipboard.publishAccount(settings);
      }
    } catch (error) { AppStorage.setOrCreate('operationStatus', '读取配对配置失败'); }
  }
  private static publishAccount(settings: Settings): void {
    AppStorage.setOrCreate('serverAddress', settings.server);
    AppStorage.setOrCreate('deviceName', settings.connection.device.name);
    AppStorage.setOrCreate('accountName', settings.connection.user.name);
    AppStorage.setOrCreate('accountId', settings.connection.user.id);
    RemoteClipboard.publishUsers([]);
  }
  private static publishUsers(users: RemoteUser[]): void {
    const settings = RemoteClipboard.settings;
    if (!settings) { AppStorage.setOrCreate<RemoteUser[]>('remoteUsers', []); AppStorage.setOrCreate('onlineDeviceCount', -1); return; }
    const self: RemoteUser = { id: settings.connection.user.id, name: settings.connection.user.name };
    const onlineCount = users.find((user: RemoteUser) => user.id === self.id)?.onlineDevices;
    AppStorage.setOrCreate('onlineDeviceCount', onlineCount === undefined ? -1 : onlineCount);
    const others = users.filter((user: RemoteUser) => (user.onlineDevices || 0) > 0 && user.id !== self.id);
    others.sort((a: RemoteUser, b: RemoteUser) => {
      const left = a.name.toLowerCase(); const right = b.name.toLowerCase();
      if (left !== right) { return left < right ? -1 : 1; }
      return a.name === b.name ? 0 : a.name < b.name ? -1 : 1;
    });
    AppStorage.setOrCreate('remoteUsers', [self, ...others]);
  }
  private static async request(server: string, token: string, path: string, method: http.RequestMethod,
    body?: string | ArrayBuffer, contentType: string = 'application/json'): Promise<string> {
    const request = http.createHttp();
    let responseError: string = '';
    try {
      const result = await request.request(`${server}${path}`, {
        method: method,
        header: { 'Content-Type': contentType, 'Authorization': `Bearer ${token}`,
          'X-Paste-Platform': 'HarmonyOS', 'X-Paste-Version': ClientMetadata.version() },
        extraData: body,
        expectDataType: http.HttpDataType.STRING,
        connectTimeout: 10000,
        readTimeout: 60000
      });
      if (method === http.RequestMethod.DELETE && path.startsWith('/v1/devices/') && result.responseCode === 401) { return ''; }
      if (result.responseCode < 200 || result.responseCode >= 300) {
        if (path === '/v1/devices') {
          responseError = result.responseCode === 429 ? '请求过于频繁，请稍后重试' : '配对失败';
          responseError = `${responseError}（${result.responseCode}）。`;
        } else { responseError = `请求失败（${result.responseCode}）`; }
        throw new Error(responseError);
      }
      return result.result as string;
    } catch (error) {
      console.error(`PasteAnywhere request failed: ${error.code}: ${error.message}`);
      if (responseError) { throw new Error(responseError); }
      if (error.code === 2300035 || error.code === 2300058 || error.code === 2300060 || error.code === 2300077) {
        throw new Error('无法建立安全连接。');
      }
      throw new Error('无法连接服务器。');
    } finally { request.destroy(); }
  }
  static async connect(server: string, deviceName: string, invite: string): Promise<void> {
    server = server.trim().replace(/\/+$/, '');
    if (!/^https?:\/\/[^?#@\s]+$/.test(server)) { throw new Error('服务器地址格式不正确。'); }
    interface ConnectBody { invite: string; deviceName: string; }
    const body: ConnectBody = { invite: invite.trim(), deviceName: deviceName.trim() };
    const response = await RemoteClipboard.request(server, '', '/v1/devices', http.RequestMethod.POST, JSON.stringify(body));
    let connection: Connection;
    try {
      connection = JSON.parse(response) as Connection;
      if (!connection?.device?.id || !connection.user?.id || !connection.user.name ||
        connection.device.name !== body.deviceName || connection.device.userId !== connection.user.id ||
        typeof connection.token !== 'string' || !/^[a-fA-F0-9]{64}$/.test(connection.token)) {
        throw new Error('Invalid pairing response');
      }
    }
    catch (error) { throw new Error('服务器返回数据不正确。'); }
    const settings: Settings = { server: server, connection: connection };
    if (!RemoteClipboard.prefs) { throw new Error('配置存储尚未初始化，请稍后重试。'); }
    try {
      await RemoteClipboard.prefs.put('settings', JSON.stringify(settings));
      await RemoteClipboard.prefs.flush();
    } catch (error) { throw new Error('无法保存设备凭证。'); }
    RemoteClipboard.stop();
    RemoteClipboard.settings = settings;
    RemoteClipboard.newest = '0'; RemoteClipboard.clearedThrough = '0';
    AppStorage.setOrCreate<RemoteEntry[]>('remoteEntries', []);
    RemoteClipboard.publishAccount(settings);
    AppStorage.setOrCreate('operationStatus', '');
    if (RemoteClipboard.active) { await RemoteClipboard.foreground(); }
  }
  static async logout(): Promise<void> {
    if (RemoteClipboard.uploads > 0) { throw new Error('上传期间无法登出'); }
    const settings = RemoteClipboard.settings;
    if (!settings || RemoteClipboard.loggingOut) { return; }
    RemoteClipboard.loggingOut = true;
    try {
      const device = `${settings.connection.user.name}-${settings.connection.device.name}`;
      await RemoteClipboard.request(settings.server, settings.connection.token, `/v1/devices/${device}`, http.RequestMethod.DELETE);
      if (!RemoteClipboard.prefs) { throw new Error('无法清除本机配对信息'); }
      try {
        await RemoteClipboard.prefs.delete('settings');
        await RemoteClipboard.prefs.flush();
      } catch (error) { throw new Error('无法清除本机配对信息'); }
      RemoteClipboard.stop();
      RemoteClipboard.settings = undefined;
      ++RemoteClipboard.incomingRevision;
      RemoteClipboard.newest = '0'; RemoteClipboard.dragId = '';
      AppStorage.setOrCreate('accountName', ''); AppStorage.setOrCreate('accountId', '');
      AppStorage.setOrCreate('deviceName', ''); AppStorage.setOrCreate('serverAddress', '');
      AppStorage.setOrCreate('operationStatus', '');
      AppStorage.setOrCreate<RemoteEntry[]>('remoteEntries', []);
      AppStorage.setOrCreate<RemoteUser[]>('remoteUsers', []);
    } finally { RemoteClipboard.loggingOut = false; }
  }
  static incoming(): void {
    ++RemoteClipboard.incomingRevision;
  }
  private static mergeEntry(entry: RemoteEntry): void {
    if (!newer(entry.id, RemoteClipboard.clearedThrough)) { return; }
    ++RemoteClipboard.dashboardRevision;
    const previous = AppStorage.get<RemoteEntry[]>('remoteEntries') || [];
    const items = previous.filter((item: RemoteEntry) => item.id !== entry.id);
    items.push(entry);
    items.sort((a: RemoteEntry, b: RemoteEntry) => a.id === b.id ? 0 : newer(a.id, b.id) ? -1 : 1);
    AppStorage.setOrCreate('remoteEntries', items.slice(0, 5));
  }
  private static applyClear(through: string): void {
    if (!newer(through, RemoteClipboard.clearedThrough)) { return; }
    RemoteClipboard.clearedThrough = through;
    if (newer(through, RemoteClipboard.newest)) { RemoteClipboard.newest = through; }
    ++RemoteClipboard.dashboardRevision;
    const entries = AppStorage.get<RemoteEntry[]>('remoteEntries') || [];
    AppStorage.setOrCreate('remoteEntries', entries.filter((entry: RemoteEntry) => newer(entry.id, through)));
  }
  static async clearClipboard(): Promise<void> {
    if (RemoteClipboard.uploads > 0) { throw new Error('上传期间无法清空'); }
    const settings = RemoteClipboard.settings;
    if (!settings) { throw new Error('请先配对'); }
    const event = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token, '/v1/entries', http.RequestMethod.DELETE)) as RemoteEvent;
    if (settings !== RemoteClipboard.settings) { return; }
    if (event.itemId) { RemoteClipboard.applyClear(event.itemId); }
    AppStorage.setOrCreate('operationStatus', '已清空');
    void RemoteClipboard.refresh();
  }
  static async upload(text: string, uri: string = ''): Promise<void> {
    ++RemoteClipboard.uploads;
    AppStorage.setOrCreate('uploading', true);
    ImageFiles.retain(uri);
    try { await RemoteClipboard.submit(text, uri); }
    finally {
      ImageFiles.release(uri);
      AppStorage.setOrCreate('uploading', --RemoteClipboard.uploads > 0);
    }
  }
  private static async submit(text: string, uri: string): Promise<void> {
    const revision = RemoteClipboard.incomingRevision;
    const settings = RemoteClipboard.settings;
    if (!settings) { throw new Error('请先配对'); }
    interface UploadBody { text: string; }
    const body: UploadBody = { text: text };
    const bytes = uri ? await ImageFiles.uploadBytes(uri) : undefined;
    if (settings !== RemoteClipboard.settings || revision !== RemoteClipboard.incomingRevision) { return; }
    let contentType = 'application/json';
    if (bytes) {
      const header = new Uint8Array(bytes);
      contentType = header[0] === 0xFF && header[1] === 0xD8 ? 'image/jpeg' : 'image/png';
    }
    const entry = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token,
      '/v1/entries', http.RequestMethod.POST, bytes || JSON.stringify(body), contentType)) as RemoteEntry;
    if (settings !== RemoteClipboard.settings || revision !== RemoteClipboard.incomingRevision) { return; }
    if (!newer(entry.id, RemoteClipboard.clearedThrough)) { return; }
    if (newer(entry.id, RemoteClipboard.newest)) { RemoteClipboard.newest = entry.id; }
    RemoteClipboard.mergeEntry(entry);
    AppStorage.setOrCreate('operationStatus', '已上传');
    void RemoteClipboard.refresh();
  }
  static async foreground(): Promise<void> {
    if (!RemoteClipboard.active) { void ImageFiles.prune(RemoteClipboard.cacheDir); }
    RemoteClipboard.active = true;
    const settings = RemoteClipboard.settings;
    if (!settings || RemoteClipboard.socket || RemoteClipboard.loggingOut) { return; }
    const epoch = ++RemoteClipboard.epoch;
    try {
      const socket = webSocket.createWebSocket();
      RemoteClipboard.socket = socket;
      socket.on('message', (_error, data: string | ArrayBuffer) => {
        if (epoch !== RemoteClipboard.epoch || !RemoteClipboard.active || typeof data !== 'string') { return; }
        try {
          const event = JSON.parse(data) as RemoteEvent;
          if (event.type === 'ready') {
            AppStorage.setOrCreate('networkConnected', true);
            void RemoteClipboard.refresh();
            if (RemoteClipboard.poll !== -1) { clearInterval(RemoteClipboard.poll); }
            RemoteClipboard.poll = setInterval(() => { void RemoteClipboard.refresh(); }, 5000);
          } else if (event.type === 'clipboard.cleared' && event.itemId) {
            RemoteClipboard.applyClear(event.itemId);
            void RemoteClipboard.refresh();
          } else if (event.type === 'clipboard.updated' && event.itemId &&
            event.sourceDeviceId !== settings.connection.device.id && newer(event.itemId, RemoteClipboard.newest)) {
            RemoteClipboard.newest = event.itemId;
            ++RemoteClipboard.dashboardRevision;
            void RemoteClipboard.refresh();
          }
        } catch (error) { console.error(`Invalid notification: ${error.message}`); }
      });
      socket.on('close', () => { RemoteClipboard.disconnected(epoch); });
      socket.on('error', () => { RemoteClipboard.disconnected(epoch); });
      await socket.connect(settings.server.replace(/^http/, 'ws') + '/v1/events', {
        supportOriginPort: true,
        header: { 'Authorization': `Bearer ${settings.connection.token}`,
          'X-Paste-Platform': 'HarmonyOS', 'X-Paste-Version': ClientMetadata.version() }
      });
    } catch (error) { RemoteClipboard.disconnected(epoch); }
  }
  private static disconnected(epoch: number): void {
    if (epoch !== RemoteClipboard.epoch) { return; }
    RemoteClipboard.stop();
    if (!RemoteClipboard.active || RemoteClipboard.loggingOut) { return; }
    RemoteClipboard.retry = setTimeout(() => { RemoteClipboard.retry = -1; void RemoteClipboard.foreground(); }, 3000);
  }
  private static stop(): void {
    ++RemoteClipboard.epoch; ++RemoteClipboard.dashboardRevision;
    AppStorage.setOrCreate('networkConnected', false);
    RemoteClipboard.publishUsers([]);
    if (RemoteClipboard.retry !== -1) { clearTimeout(RemoteClipboard.retry); RemoteClipboard.retry = -1; }
    if (RemoteClipboard.poll !== -1) { clearInterval(RemoteClipboard.poll); RemoteClipboard.poll = -1; }
    const socket = RemoteClipboard.socket;
    RemoteClipboard.socket = undefined;
    if (socket) { try { socket.close().catch(() => {}); } catch (_) { } }
  }
  static background(): void {
    RemoteClipboard.active = false;
    RemoteClipboard.stop();
  }
  static async refresh(): Promise<void> {
    const settings = RemoteClipboard.settings;
    if (!settings || !RemoteClipboard.active || RemoteClipboard.loggingOut || RemoteClipboard.dragId) { return; }
    const epoch = RemoteClipboard.epoch;
    if (RemoteClipboard.refreshing && RemoteClipboard.refreshEpoch === epoch) { RemoteClipboard.refreshAgain = true; return; }
    RemoteClipboard.refreshing = true;
    RemoteClipboard.refreshEpoch = epoch;
    RemoteClipboard.refreshAgain = false;
    const revision = RemoteClipboard.dashboardRevision;
    try {
      const result = await Promise.all([
        RemoteClipboard.request(settings.server, settings.connection.token, '/v1/users', http.RequestMethod.GET),
        RemoteClipboard.request(settings.server, settings.connection.token, '/v1/entries?limit=5', http.RequestMethod.GET)
      ]);
      if (epoch !== RemoteClipboard.epoch || !RemoteClipboard.active || settings !== RemoteClipboard.settings || RemoteClipboard.dragId) { return; }
      RemoteClipboard.publishUsers(JSON.parse(result[0]) as RemoteUser[]);
      if (revision === RemoteClipboard.dashboardRevision) {
        AppStorage.setOrCreate('remoteEntries', (JSON.parse(result[1]) as RemoteEntry[])
          .filter((entry: RemoteEntry) => newer(entry.id, RemoteClipboard.clearedThrough)).slice(0, 5));
      } else { RemoteClipboard.refreshAgain = true; }
    } catch (error) {
      if (epoch === RemoteClipboard.epoch) { AppStorage.setOrCreate('operationStatus', '暂时无法刷新，请稍后重试'); }
    } finally {
      if (RemoteClipboard.refreshEpoch === epoch) {
        RemoteClipboard.refreshing = false;
        if (RemoteClipboard.refreshAgain) { RemoteClipboard.refreshAgain = false; void RemoteClipboard.refresh(); }
      }
    }
  }
  static async prepare(entry: RemoteEntry, progress: (received: number, total: number) => void = () => {},
    reload: boolean = false): Promise<PreparedContent> {
    const settings = RemoteClipboard.settings;
    if (!settings) { throw new Error('请先配对'); }
    if (!newer(entry.id, RemoteClipboard.clearedThrough)) { throw new Error('内容已清空'); }
    const uri = entry.kind === 'image' ? await ImageFiles.download(settings, entry, RemoteClipboard.cacheDir, progress, reload) : '';
    if (settings !== RemoteClipboard.settings || RemoteClipboard.loggingOut) { throw new Error('配对状态已改变'); }
    if (!newer(entry.id, RemoteClipboard.clearedThrough)) { throw new Error('内容已清空'); }
    const content: PreparedContent = { text: entry.text, uri: uri,
      utd: entry.kind === 'image' ? (entry.attachment?.mime === 'image/png' ? 'general.png' : 'general.jpeg') : 'general.plain-text' };
    return content;
  }
  static async send(userId: string): Promise<void> {
    const settings = RemoteClipboard.settings; const id = RemoteClipboard.dragId;
    if (!settings || !id || RemoteClipboard.loggingOut) { return; }
    interface SendBody { entryId: string; userId: string; }
    const body: SendBody = { entryId: id, userId: userId };
    try {
      const entry = JSON.parse(await RemoteClipboard.request(settings.server, settings.connection.token,
        '/v1/transfers', http.RequestMethod.POST, JSON.stringify(body))) as RemoteEntry;
      if (settings !== RemoteClipboard.settings) { return; }
      if (userId === settings.connection.user.id) {
        if (newer(entry.id, RemoteClipboard.newest)) { RemoteClipboard.newest = entry.id; }
        RemoteClipboard.mergeEntry(entry);
      }
      AppStorage.setOrCreate('operationStatus', '已发送');
      void RemoteClipboard.refresh();
    } catch (error) { if (settings === RemoteClipboard.settings) { AppStorage.setOrCreate('operationStatus', '发送失败，请重试'); } }
  }
}
