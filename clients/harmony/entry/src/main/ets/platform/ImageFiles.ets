import { fileIo as fs, fileUri } from '@kit.CoreFileKit';
import { image } from '@kit.ImageKit';
import { http } from '@kit.NetworkKit';
import { RemoteEntry, Settings } from '../networking/RemoteTypes';

const MAX_BYTES: number = 16 * 1024 * 1024;

export class ImageFiles {
  private static sequence: number = 0;
  private static pruning: boolean = false;

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
            // Keep the API 12-compatible entry point until the minimum OS version is finalized.
            const bytes = await packer.packing(source, { format: 'image/jpeg', quality: 95 });
            if (bytes.byteLength > MAX_BYTES) { throw new Error('转换后的图片超过 16 MiB'); }
            return bytes;
          } finally { await packer.release(); }
        } finally { await source.release(); }
      } finally { await fs.close(file); }
    } catch (error) { throw new Error(`图片处理失败：${error.message}`); }
  }

  static async download(settings: Settings, entry: RemoteEntry, cacheDir: string): Promise<string> {
    const attachment = entry.attachment;
    if (!attachment || attachment.size <= 0 || attachment.size > MAX_BYTES ||
      attachment.width * attachment.height > 40000000 || !/^\d+$/.test(entry.id)) {
      throw new Error('图片信息无效或超过限制');
    }
    if (attachment.mime !== 'image/png' && attachment.mime !== 'image/jpeg') { throw new Error('不支持的远程图片格式'); }
    const request = http.createHttp();
    try {
      const result = await request.request(`${settings.server}/v1/entries/${entry.id}/content`, {
        method: http.RequestMethod.GET,
        header: { 'Authorization': `Bearer ${settings.connection.token}` },
        expectDataType: http.HttpDataType.ARRAY_BUFFER,
        maxLimit: MAX_BYTES,
        connectTimeout: 10000,
        readTimeout: 60000
      });
      if (result.responseCode !== 200) { throw new Error(`图片下载 HTTP ${result.responseCode}`); }
      const bytes = result.result as ArrayBuffer;
      if (bytes.byteLength !== attachment.size) { throw new Error('图片下载不完整'); }
      const extension = attachment.mime === 'image/png' ? '.png' : '.jpg';
      const path = `${cacheDir}/remote-${Date.now()}-${++ImageFiles.sequence}${extension}`;
      const file = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.TRUNC);
      try {
        let offset = 0;
        while (offset < bytes.byteLength) {
          const count = await fs.write(file.fd, bytes.slice(offset));
          if (count <= 0) { throw new Error('图片缓存写入失败'); }
          offset += count;
        }
      } finally { await fs.close(file); }
      return fileUri.getUriFromPath(path);
    } catch (error) { throw new Error(`图片下载失败：${error.message}`); }
    finally { request.destroy(); }
  }

  static async prune(cacheDir: string): Promise<void> {
    if (!cacheDir || ImageFiles.pruning) { return; }
    ImageFiles.pruning = true;
    try {
      const names = await fs.listFile(cacheDir);
      interface CachedFile { path: string; modified: number; }
      const files: CachedFile[] = [];
      for (const name of names) {
        if (!/^(remote|probe)-[0-9-]+\.[a-zA-Z0-9]+$/.test(name)) { continue; }
        const path = `${cacheDir}/${name}`;
        const stat = await fs.stat(path);
        files.push({ path: path, modified: stat.mtime });
      }
      files.sort((a: CachedFile, b: CachedFile) => b.modified - a.modified);
      const current = AppStorage.get<string>('probeImage') || '';
      for (let i = 0; i < files.length; i++) {
        const file = files[i];
        // A short grace period protects incoming shares and concurrent downloads.
        if (fileUri.getUriFromPath(file.path) === current || Date.now() / 1000 - file.modified < 300) { continue; }
        if (i >= 20 || Date.now() / 1000 - file.modified > 7 * 86400) { await fs.unlink(file.path); }
      }
    } catch (error) { console.warn(`Image cache cleanup: ${error.message}`); }
    finally { ImageFiles.pruning = false; }
  }
}
