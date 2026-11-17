import { fileIo as fs, fileUri } from '@kit.CoreFileKit';
import { image } from '@kit.ImageKit';
import { http } from '@kit.NetworkKit';
import { RemoteEntry, Settings } from '../networking/RemoteTypes';
import { ClientMetadata } from '../networking/ClientMetadata';

const MAX_BYTES: number = 16 * 1024 * 1024;

export class ImageFiles {
  private static sequence: number = 0;
  private static pruning: boolean = false;
  private static downloads: Map<string, string> = new Map();
  private static references: Map<string, number> = new Map();

  static retain(uri: string): void {
    if (uri) { ImageFiles.references.set(uri, (ImageFiles.references.get(uri) || 0) + 1); }
  }
  static release(uri: string): void {
    const count = ImageFiles.references.get(uri) || 0;
    if (count <= 1) { ImageFiles.references.delete(uri); }
    else { ImageFiles.references.set(uri, count - 1); }
  }

  static async uploadBytes(uri: string): Promise<ArrayBuffer> {
    try {
      const file = await fs.open(uri, fs.OpenMode.READ_ONLY);
      try {
        const stat = await fs.stat(file.fd);
        if (stat.size <= 0 || stat.size > MAX_BYTES) { throw new Error('图片超过 16 MiB 或为空'); }
        const source = image.createImageSource(file.fd);
        try {
          const info = await source.getImageInfo();
          if (info.size.width * info.size.height > 40000000) { throw new Error('图片超过 4000 万像素'); }
          // Preserve standard PNG/JPEG bytes. Other decodable formats use a JPEG derivative.
          if (info.mimeType === 'image/png' || info.mimeType === 'image/jpeg') {
            const bytes = new ArrayBuffer(stat.size);
            let offset = 0;
            while (offset < bytes.byteLength) {
              const chunk = new ArrayBuffer(bytes.byteLength - offset);
              const count = await fs.read(file.fd, chunk, { offset: offset });
              if (count <= 0) { throw new Error('图片读取不完整'); }
              new Uint8Array(bytes).set(new Uint8Array(chunk, 0, count), offset);
              offset += count;
            }
            return bytes;
          }
          const packer = image.createImagePacker();
          try {
            const bytes = await packer.packToData(source, { format: 'image/jpeg', quality: 95 });
            if (bytes.byteLength > MAX_BYTES) { throw new Error('转换后的图片超过 16 MiB'); }
            return bytes;
          } finally { await packer.release(); }
        } finally { await source.release(); }
      } finally { await fs.close(file); }
    } catch (error) { throw new Error(`图片处理失败：${error.message}`); }
  }

  static async download(settings: Settings, entry: RemoteEntry, cacheDir: string,
    progress: (received: number, total: number) => void = () => {}, reload: boolean = false): Promise<string> {
    const attachment = entry.attachment;
    if (!attachment || attachment.size <= 0 || attachment.size > MAX_BYTES ||
      attachment.width * attachment.height > 40000000 || !/^\d+$/.test(entry.id)) {
      throw new Error('图片信息无效或超过限制');
    }
    if (attachment.mime !== 'image/png' && attachment.mime !== 'image/jpeg') { throw new Error('不支持的远程图片格式'); }
    const key = `${settings.server}\n${settings.connection.token}\n${entry.id}`;
    const cached = ImageFiles.downloads.get(key);
    if (cached && !reload) {
      try {
        const stat = await fs.stat(cached);
        if (stat.size === attachment.size && Date.now() / 1000 - stat.mtime <= 7 * 86400) {
          ImageFiles.downloads.delete(key);
          ImageFiles.downloads.set(key, cached);
          progress(attachment.size, attachment.size);
          return fileUri.getUriFromPath(cached);
        }
      } catch (_) { }
    }
    ImageFiles.downloads.delete(key);
    const request = http.createHttp();
    let timer: number = -1;
    let incomplete: string = '';
    try {
      const bytes = new ArrayBuffer(attachment.size);
      let received = 0;
      let overflow = false;
      request.on('dataReceive', (chunk: ArrayBuffer) => {
        if (overflow) { return; }
        if (received + chunk.byteLength > bytes.byteLength) { overflow = true; return; }
        new Uint8Array(bytes).set(new Uint8Array(chunk), received);
        received += chunk.byteLength;
        progress(received, attachment.size);
      });
      const ended = new Promise<void>((resolve) => { request.on('dataEnd', () => { resolve(); }); });
      const timeout = new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => { reject(new Error('图片下载超时，请重试')); }, 75000);
      });
      const response = request.requestInStream(`${settings.server}/v1/entries/${entry.id}/content`, {
        method: http.RequestMethod.GET,
        header: { 'Authorization': `Bearer ${settings.connection.token}`,
          'X-Paste-Platform': 'HarmonyOS', 'X-Paste-Version': ClientMetadata.version() },
        maxLimit: MAX_BYTES,
        connectTimeout: 10000,
        readTimeout: 60000
      });
      const status = await Promise.race([response, timeout]);
      if (status !== 200) { throw new Error(`图片下载 HTTP ${status}`); }
      await Promise.race([ended, timeout]);
      if (overflow || received !== attachment.size) { throw new Error('图片下载大小不符'); }
      const extension = attachment.mime === 'image/png' ? '.png' : '.jpg';
      const path = `${cacheDir}/remote-${Date.now()}-${++ImageFiles.sequence}${extension}`;
      const file = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.TRUNC);
      incomplete = path;
      try {
        let offset = 0;
        while (offset < bytes.byteLength) {
          const count = await fs.write(file.fd, bytes.slice(offset));
          if (count <= 0) { throw new Error('图片缓存写入失败'); }
          offset += count;
        }
      } finally { await fs.close(file); }
      incomplete = '';
      ImageFiles.downloads.set(key, path);
      if (ImageFiles.downloads.size > 20) {
        for (const oldest of ImageFiles.downloads.keys()) {
          ImageFiles.downloads.delete(oldest);
          break;
        }
      }
      return fileUri.getUriFromPath(path);
    } catch (error) { throw new Error(`图片下载失败：${error.message}`); }
    finally {
      if (timer !== -1) { clearTimeout(timer); }
      request.destroy();
      if (incomplete) { try { await fs.unlink(incomplete); } catch (_) { } }
    }
  }

  static async prune(cacheDir: string): Promise<void> {
    if (!cacheDir || ImageFiles.pruning) { return; }
    ImageFiles.pruning = true;
    try {
      const names = await fs.listFile(cacheDir);
      interface CachedFile { path: string; modified: number; }
      const files: CachedFile[] = [];
      for (const name of names) {
        if (!/^(remote|incoming)-[0-9-]+\.[a-zA-Z0-9]+$/.test(name)) { continue; }
        const path = `${cacheDir}/${name}`;
        const stat = await fs.stat(path);
        files.push({ path: path, modified: stat.mtime });
      }
      files.sort((a: CachedFile, b: CachedFile) => b.modified - a.modified);
      for (let i = 0; i < files.length; i++) {
        const file = files[i];
        // A short grace period protects incoming shares and concurrent downloads.
        if (ImageFiles.references.has(fileUri.getUriFromPath(file.path)) || Date.now() / 1000 - file.modified < 300) { continue; }
        if (i >= 20 || Date.now() / 1000 - file.modified > 7 * 86400) { await fs.unlink(file.path); }
      }
    } catch (error) { console.warn(`Image cache cleanup: ${error.message}`); }
    finally { ImageFiles.pruning = false; }
  }
}
