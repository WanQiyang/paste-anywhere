import { common, Want } from '@kit.AbilityKit';
import { systemShare } from '@kit.ShareKit';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { fileIo as fs, fileUri } from '@kit.CoreFileKit';
import { image } from '@kit.ImageKit';
import { pasteboard } from '@kit.BasicServicesKit';
import { RemoteClipboard } from '../services/RemoteClipboard';

// Platform sharing and explicit clipboard writes. Incoming content uploads after parsing.
export class ClipboardProbe {
  private static generation: number = 0;

  static async receive(context: common.UIAbilityContext, want: Want): Promise<void> {
    if (want.action !== 'ohos.want.action.sendData') {
      return;
    }
    const generation = ++ClipboardProbe.generation;
    RemoteClipboard.incoming();
    AppStorage.setOrCreate('probeStatus', '正在读取系统分享…');
    try {
      const shared = await systemShare.getSharedData(want);
      const records = shared.getRecords();
      if (records.length !== 1) {
        throw new Error('第一版仅支持一条文本或单张图片，请重新分享。');
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
        const source = await fs.open(record.uri, fs.OpenMode.READ_ONLY);
        const extension = descriptor.filenameExtensions.find((value: string) => /^\.[a-zA-Z0-9]+$/.test(value)) || '.image';
        const path = `${context.cacheDir}/probe-${generation}-${Date.now()}${extension}`;
        try {
          const stat = await fs.stat(source.fd);
          if (stat.size > 16 * 1024 * 1024) { throw new Error('图片超过 16 MiB'); }
          await fs.copyFile(source.fd, path);
        } finally {
          await fs.close(source);
        }
        uri = fileUri.getUriFromPath(path);
      } else if (record.content !== undefined &&
        (descriptor.belongsTo(uniformTypeDescriptor.UniformDataType.TEXT) ||
          record.utd === uniformTypeDescriptor.UniformDataType.HYPERLINK)) {
        text = record.content;
      } else {
        throw new Error(`暂不支持此分享类型：${record.utd}`);
      }
      if (generation !== ClipboardProbe.generation) {
        return;
      }
      AppStorage.setOrCreate('probeText', text);
      AppStorage.setOrCreate('probeImage', uri);
      try {
        AppStorage.setOrCreate('probeStatus', uri ? '正在上传图片…' : '正在上传文本…');
        await RemoteClipboard.upload(text, uri);
      } catch (error) {
        if (generation === ClipboardProbe.generation) {
          AppStorage.setOrCreate('probeStatus', `已接收，上传失败：${error.message}`);
        }
      }
    } catch (error) {
      if (generation === ClipboardProbe.generation) {
        AppStorage.setOrCreate('probeStatus', `接收失败：${error.message}`);
      }
    }
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
