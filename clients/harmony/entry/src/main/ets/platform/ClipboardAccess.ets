import { abilityAccessCtrl, common, Want } from '@kit.AbilityKit';
import { systemShare } from '@kit.ShareKit';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { fileIo as fs, fileUri } from '@kit.CoreFileKit';
import { image } from '@kit.ImageKit';
import { pasteboard } from '@kit.BasicServicesKit';
import { RemoteClipboard } from '../services/RemoteClipboard';

// Explicit clipboard reads/writes and platform sharing.
export class ClipboardAccess {
  private static generation: number = 0;

  static async captureAndUpload(context: common.UIAbilityContext): Promise<void> {
    const generation = ++ClipboardAccess.generation;
    RemoteClipboard.incoming();
    const permission = await abilityAccessCtrl.createAtManager().requestPermissionsFromUser(
      context, ['ohos.permission.READ_PASTEBOARD']);
    if (generation !== ClipboardAccess.generation) { return; }
    if (permission.authResults.length !== 1 || permission.authResults[0] !== 0) {
      throw new Error('请允许读取系统剪贴板后重试');
    }
    // Read one snapshot; later clipboard changes do not replace this upload.
    const data = await pasteboard.getSystemPasteboard().getData();
    if (generation !== ClipboardAccess.generation) { return; }
    if (data.getRecordCount() !== 1) {
      throw new Error('剪贴板为空，或不包含单条文本、单张图片');
    }
    const record = data.getRecord(0);
    let text = '';
    let uri = '';
    if (record.pixelMap) {
      const pixels = record.pixelMap;
      try {
        const info = await pixels.getImageInfo();
        if (info.size.width * info.size.height > 40000000) { throw new Error('图片超过 4000 万像素'); }
        const packer = image.createImagePacker();
        try {
          const bytes = await packer.packToData(pixels, { format: 'image/png', quality: 100 });
          if (bytes.byteLength > 16 * 1024 * 1024) { throw new Error('图片超过 16 MiB'); }
          const path = `${context.cacheDir}/incoming-${generation}-${Date.now()}.png`;
          const file = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.TRUNC);
          try {
            let offset = 0;
            while (offset < bytes.byteLength) {
              const count = await fs.write(file.fd, bytes.slice(offset));
              if (count <= 0) { throw new Error('图片缓存写入失败'); }
              offset += count;
            }
          } finally { await fs.close(file); }
          uri = fileUri.getUriFromPath(path);
        } finally { await packer.release(); }
      } finally { await pixels.release(); }
    } else if (record.uri) {
      uri = await ClipboardAccess.cacheImage(context.cacheDir, record.uri, generation);
    } else {
      text = data.getPrimaryText();
      if (!text) { throw new Error('剪贴板为空，或不包含支持的文本或单张图片'); }
    }
    if (generation !== ClipboardAccess.generation) { return; }
    AppStorage.setOrCreate('localText', text);
    AppStorage.setOrCreate('localImage', uri);
    await RemoteClipboard.upload(text, uri);
  }

  static async receive(context: common.UIAbilityContext, want: Want): Promise<void> {
    if (want.action !== 'ohos.want.action.sendData') {
      return;
    }
    const generation = ++ClipboardAccess.generation;
    RemoteClipboard.incoming();
    AppStorage.setOrCreate('operationStatus', '正在读取系统分享…');
    try {
      const shared = await systemShare.getSharedData(want);
      const records = shared.getRecords();
      if (records.length !== 1) {
        throw new Error('仅支持一条文本或单张图片，请重新分享。');
      }
      const record = records[0];
      const descriptor = uniformTypeDescriptor.getTypeDescriptor(record.utd);
      let text = '';
      let uri = '';
      if (descriptor.belongsTo(uniformTypeDescriptor.UniformDataType.IMAGE)) {
        if (!record.uri) {
          throw new Error('图片分享未携带可读取的 URI。');
        }
        // Copy while the incoming temporary URI permission is available.
        uri = await ClipboardAccess.cacheImage(context.cacheDir, record.uri, generation);
      } else if (record.content !== undefined &&
        (descriptor.belongsTo(uniformTypeDescriptor.UniformDataType.TEXT) ||
          record.utd === uniformTypeDescriptor.UniformDataType.HYPERLINK)) {
        text = record.content;
      } else {
        throw new Error(`暂不支持此分享类型：${record.utd}`);
      }
      if (generation !== ClipboardAccess.generation) {
        return;
      }
      AppStorage.setOrCreate('localText', text);
      AppStorage.setOrCreate('localImage', uri);
      try {
        AppStorage.setOrCreate('operationStatus', uri ? '正在上传图片…' : '正在上传文本…');
        await RemoteClipboard.upload(text, uri);
      } catch (error) {
        if (generation === ClipboardAccess.generation) {
          AppStorage.setOrCreate('operationStatus', `已接收，上传失败：${error.message}`);
        }
      }
    } catch (error) {
      if (generation === ClipboardAccess.generation) {
        AppStorage.setOrCreate('operationStatus', `接收失败：${error.message}`);
      }
    }
  }

  private static async cacheImage(cacheDir: string, uri: string, generation: number): Promise<string> {
    const source = await fs.open(uri, fs.OpenMode.READ_ONLY);
    const path = `${cacheDir}/incoming-${generation}-${Date.now()}.image`;
    try {
      const stat = await fs.stat(source.fd);
      if (stat.size <= 0 || stat.size > 16 * 1024 * 1024) { throw new Error('图片为空或超过 16 MiB'); }
      await fs.copyFile(source.fd, path);
    } finally { await fs.close(source); }
    return fileUri.getUriFromPath(path);
  }

  static async copy(text: string, uri: string): Promise<void> {
    try {
      if (!uri) {
        await pasteboard.getSystemPasteboard().setData(
          pasteboard.createData(pasteboard.MIMETYPE_TEXT_PLAIN, text));
        return;
      }
      const file = await fs.open(uri, fs.OpenMode.READ_ONLY);
      try {
        const source = image.createImageSource(file.fd);
        try {
          const pixels = await source.createPixelMap();
          try {
            await pasteboard.getSystemPasteboard().setData(
              pasteboard.createData(pasteboard.MIMETYPE_PIXELMAP, pixels));
          } finally {
            await pixels.release();
          }
        } finally {
          await source.release();
        }
      } finally {
        await fs.close(file);
      }
    } catch (error) {
      throw new Error(`系统剪贴板操作失败 (${error.code}): ${error.message}`);
    }
  }

  static async share(context: common.UIAbilityContext, text: string, uri: string, utd: string): Promise<void> {
    try {
      const record: systemShare.SharedRecord = uri ? { utd: utd, uri: uri } : {
        utd: uniformTypeDescriptor.UniformDataType.PLAIN_TEXT, content: text
      };
      const controller = new systemShare.ShareController(new systemShare.SharedData(record));
      await controller.show(context, {});
    } catch (error) {
      throw new Error(`系统分享操作失败 (${error.code}): ${error.message}`);
    }
  }
}
