using System.IO;
using System.Windows;
using System.Windows.Media.Imaging;

namespace PasteAnywhere.Platform;

public sealed record ClipboardContent(string? Text, byte[]? Image)
{
    public static ClipboardContent Read()
    {
        if (System.Windows.Clipboard.ContainsImage())
        {
            var bitmap = System.Windows.Clipboard.GetImage() ?? throw new IOException("无法读取剪贴板图片");
            CheckDimensions(bitmap);
            using var stream = new MemoryStream();
            var encoder = new PngBitmapEncoder();
            encoder.Frames.Add(BitmapFrame.Create(bitmap));
            encoder.Save(stream);
            if (stream.Length > 16 * 1024 * 1024) throw new IOException("编码后的图片超过 16 MiB");
            return new ClipboardContent(null, stream.ToArray());
        }
        if (System.Windows.Clipboard.ContainsFileDropList())
        {
            var files = System.Windows.Clipboard.GetFileDropList();
            if (files.Count != 1) throw new IOException("仅支持单张图片");
            var path = files[0]!;
            var extension = Path.GetExtension(path).ToLowerInvariant();
            if (extension != ".png" && extension != ".jpg" && extension != ".jpeg")
                throw new IOException("当前只支持文本和 PNG/JPEG 图片文件");
            if (new FileInfo(path).Length > 16 * 1024 * 1024) throw new IOException("图片超过 16 MiB");
            var bytes = File.ReadAllBytes(path);
            Decode(bytes);
            return new ClipboardContent(null, bytes);
        }
        if (System.Windows.Clipboard.ContainsText()) return new ClipboardContent(System.Windows.Clipboard.GetText(), null);
        throw new IOException("当前剪贴板为空，或不包含支持的文本或单张图片");
    }

    public static BitmapSource Decode(byte[] bytes)
    {
        using var stream = new MemoryStream(bytes);
        var decoder = BitmapDecoder.Create(stream, BitmapCreateOptions.PreservePixelFormat, BitmapCacheOption.OnLoad);
        var bitmap = decoder.Frames[0];
        CheckDimensions(bitmap);
        bitmap.Freeze();
        return bitmap;
    }
    private static void CheckDimensions(BitmapSource bitmap)
    {
        if ((long)bitmap.PixelWidth * bitmap.PixelHeight > 40_000_000) throw new IOException("图片超过 4000 万像素");
    }
    public void Write(BitmapSource? decoded = null)
    {
        if (Image == null) { System.Windows.Clipboard.SetText(Text ?? ""); return; }
        var data = new System.Windows.DataObject();
        data.SetImage(decoded ?? Decode(Image));
        // Preserve PNG representation alongside the standard bitmap format where possible.
        using var png = new MemoryStream();
        var encoder = new PngBitmapEncoder(); encoder.Frames.Add(BitmapFrame.Create(decoded ?? Decode(Image))); encoder.Save(png);
        png.Position = 0;
        data.SetData("PNG", png);
        System.Windows.Clipboard.SetDataObject(data, true);
    }
}
