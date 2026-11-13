using System.IO;
using System.Windows;
using System.Windows.Media.Imaging;

namespace PasteAnywhere.Platform;

public sealed record ClipboardContent(string? Text, byte[]? Image)
{
    public static ClipboardContent Read() => Read(System.Windows.Clipboard.GetDataObject());

    internal static ClipboardContent Read(System.Windows.IDataObject? data)
    {
        if (data == null) throw new IOException("当前剪贴板为空");
        // Screenshot tools may expose a DIB with an unused (all-zero) alpha channel.
        // Prefer their encoded PNG rather than converting that DIB into a transparent PNG.
        foreach (var format in new[] { "PNG", "image/png" })
        {
            if (!data.GetDataPresent(format, false)) continue;
            byte[]? bytes = null;
            var value = data.GetData(format, false);
            if (value is Stream source)
            {
                var position = source.CanSeek ? source.Position : 0;
                try
                {
                    if (source.CanSeek) source.Position = 0;
                    using var output = new MemoryStream();
                    var buffer = new byte[81920];
                    int count;
                    while ((count = source.Read(buffer, 0, buffer.Length)) > 0)
                    {
                        if (output.Length + count > 16 * 1024 * 1024) throw new IOException("图片超过 16 MiB");
                        output.Write(buffer, 0, count);
                    }
                    bytes = output.ToArray();
                }
                finally { if (source.CanSeek) source.Position = position; }
            }
            else if (value is byte[] encoded) bytes = encoded;
            if (bytes == null) continue;
            if (bytes.Length > 16 * 1024 * 1024) throw new IOException("图片超过 16 MiB");
            Decode(bytes);
            return new ClipboardContent(null, bytes);
        }
        if (data.GetDataPresent(System.Windows.DataFormats.Bitmap))
        {
            var bitmap = data.GetData(System.Windows.DataFormats.Bitmap) as BitmapSource ?? throw new IOException("无法读取剪贴板图片");
            CheckDimensions(bitmap);
            using var stream = new MemoryStream();
            var encoder = new PngBitmapEncoder();
            encoder.Frames.Add(BitmapFrame.Create(bitmap));
            encoder.Save(stream);
            if (stream.Length > 16 * 1024 * 1024) throw new IOException("编码后的图片超过 16 MiB");
            return new ClipboardContent(null, stream.ToArray());
        }
        if (data.GetDataPresent(System.Windows.DataFormats.FileDrop))
        {
            var files = data.GetData(System.Windows.DataFormats.FileDrop) as string[];
            if (files?.Length != 1) throw new IOException("仅支持单张图片");
            var path = files[0]!;
            var extension = Path.GetExtension(path).ToLowerInvariant();
            if (extension != ".png" && extension != ".jpg" && extension != ".jpeg")
                throw new IOException("当前只支持文本和 PNG/JPEG 图片文件");
            if (new FileInfo(path).Length > 16 * 1024 * 1024) throw new IOException("图片超过 16 MiB");
            var bytes = File.ReadAllBytes(path);
            Decode(bytes);
            return new ClipboardContent(null, bytes);
        }
        if (data.GetDataPresent(System.Windows.DataFormats.UnicodeText))
        {
            var text = data.GetData(System.Windows.DataFormats.UnicodeText) as string ?? "";
            if (text.Length > 0) return new ClipboardContent(text, null);
        }
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
        decoded ??= Decode(Image);
        data.SetImage(decoded);
        // Preserve PNG representation alongside the standard bitmap format where possible.
        using var png = new MemoryStream();
        var encoder = new PngBitmapEncoder(); encoder.Frames.Add(BitmapFrame.Create(decoded)); encoder.Save(png);
        png.Position = 0;
        data.SetData("PNG", png);
        System.Windows.Clipboard.SetDataObject(data, true);
    }
}
