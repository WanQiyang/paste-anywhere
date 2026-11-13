using System.Net;
using System.Net.Sockets;
using System.Text;
using System.IO;
using System.Windows;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using PasteAnywhere.Networking;
using PasteAnywhere.Platform;

internal static class Program
{
    private sealed class Reporter : IProgress<(long Received, long Total)>
    {
        public readonly List<(long Received, long Total)> Values = [];
        public void Report((long Received, long Total) value) => Values.Add(value);
    }
    private static void Check(bool condition, string message) { if (!condition) throw new Exception(message); }
    private static async Task Fails(Func<Task> action)
    {
        try { await action(); } catch (IOException) { return; }
        throw new Exception("Expected failure");
    }
    [STAThread]
    public static void Main()
    {
        CheckClipboard();
        CheckDownloads().GetAwaiter().GetResult();
    }

    private static void CheckClipboard()
    {
        var pixels = BitmapSource.Create(2, 1, 96, 96, PixelFormats.Bgra32, null,
            new byte[] { 20, 40, 60, 128, 0, 0, 0, 0 }, 8);
        var encoder = new PngBitmapEncoder();
        encoder.Frames.Add(BitmapFrame.Create(pixels));
        using var encoded = new MemoryStream();
        encoder.Save(encoded);
        var expected = encoded.ToArray();
        foreach (var format in new[] { "PNG", "image/png" })
        {
            var data = new DataObject();
            data.SetImage(BitmapSource.Create(2, 1, 96, 96, PixelFormats.Bgra32, null, new byte[8], 8));
            using var stream = new MemoryStream(expected);
            stream.Position = 3;
            data.SetData(format, stream);
            var captured = ClipboardContent.Read(data).Image!;
            Check(captured.SequenceEqual(expected), "Original PNG must take precedence over transparent DIB");
            Check(stream.Position == 3, "Clipboard stream position must be preserved");
            var decoded = ClipboardContent.Decode(captured);
            Check(decoded.IsFrozen, "Decoded bitmap must support cross-thread preview");
            var rgba = new FormatConvertedBitmap(decoded, PixelFormats.Bgra32, null, 0);
            var actual = new byte[8]; rgba.CopyPixels(actual, 8, 0);
            Check(actual[3] == 128 && actual[7] == 0, "Genuine alpha must be preserved");
        }
        var bitmap = new DataObject(); bitmap.SetImage(pixels);
        Check(ClipboardContent.Decode(ClipboardContent.Read(bitmap).Image!).PixelWidth == 2, "Bitmap fallback");
        var text = new DataObject(); text.SetText("中文\n hello ");
        Check(ClipboardContent.Read(text).Text == "中文\n hello ", "Text snapshot");
        var file = Path.Combine(Path.GetTempPath(), $"paste-image-check-{Guid.NewGuid():N}.png");
        try
        {
            File.WriteAllBytes(file, expected);
            var data = new DataObject(); data.SetData(DataFormats.FileDrop, new[] { file });
            Check(ClipboardContent.Read(data).Image!.SequenceEqual(expected), "PNG file byte preservation");
        }
        finally { File.Delete(file); }
        Console.WriteLine("PASS: PNG priority/alpha/stream lifetime, bitmap/file/text clipboard snapshots (system clipboard untouched).");
    }

    private static async Task CheckDownloads()
    {
        var listener = new TcpListener(IPAddress.Loopback, 0);
        listener.Start();
        var port = ((IPEndPoint)listener.LocalEndpoint).Port;
        using var shutdown = new CancellationTokenSource();
        string mode = "ok";
        int requests = 0;
        var server = Task.Run(async () =>
        {
            try
            {
                while (!shutdown.IsCancellationRequested)
                {
                    using var socket = await listener.AcceptTcpClientAsync(shutdown.Token);
                    using var stream = socket.GetStream();
                    using var reader = new StreamReader(stream, leaveOpen: true);
                    while (await reader.ReadLineAsync(shutdown.Token) is { Length: > 0 }) { }
                    Interlocked.Increment(ref requests);
                    var body = mode == "short" ? new byte[] { 1, 2 } : mode == "large" ? new byte[] { 1, 2, 3, 4, 5 } : new byte[] { 1, 2, 3, 4 };
                    await stream.WriteAsync(Encoding.ASCII.GetBytes($"HTTP/1.1 200 OK\r\nContent-Length: {(mode == "large" ? 5 : 4)}\r\nConnection: close\r\n\r\n"), shutdown.Token);
                    if (mode == "stall")
                    {
                        _ = await stream.ReadAsync(new byte[1], shutdown.Token);
                        continue;
                    }
                    await stream.WriteAsync(body, shutdown.Token);
                }
            }
            catch (OperationCanceledException) { }
        });
        Entry MakeEntry(string id) => new(id, "u", "u", "d", "image", "", 0, new Attachment("a" + id, "image.png", "image/png", 4, 1, 1));
        try
        {
            using var api = new RemoteClient($"http://127.0.0.1:{port}", "session-one");
            var progress = new Reporter();
            var bytes = await api.DownloadImage(MakeEntry("1"), default, progress);
            Check(bytes.SequenceEqual(new byte[] { 1, 2, 3, 4 }), "Download integrity");
            Check(progress.Values.First() == (0, 4) && progress.Values.Last() == (4, 4), "Actual byte progress");
            await api.DownloadImage(MakeEntry("1"), default);
            Check(requests == 1, "Cache hit must not download");
            await api.DownloadImage(MakeEntry("1"), default, reload: true);
            Check(requests == 2, "Retry bypasses cache");
            using (var other = new RemoteClient($"http://127.0.0.1:{port}", "session-two"))
                await other.DownloadImage(MakeEntry("1"), default);
            Check(requests == 3, "Session isolation");
            mode = "short";
            await Fails(async () => await api.DownloadImage(MakeEntry("2"), default));
            mode = "ok";
            await api.DownloadImage(MakeEntry("2"), default);
            Check(requests == 5, "Partial data must not be cached");
            mode = "large";
            await Fails(async () => await api.DownloadImage(MakeEntry("3"), default));
            mode = "stall";
            using (var cancel = new CancellationTokenSource(TimeSpan.FromMilliseconds(250)))
            {
                try { await api.DownloadImage(MakeEntry("4"), cancel.Token); throw new Exception("Cancellation ignored"); }
                catch (OperationCanceledException) when (cancel.IsCancellationRequested) { }
            }
            mode = "ok";
            await api.DownloadImage(MakeEntry("4"), default);
            Check(requests == 8, "Canceled response must not be cached");
            var cache = new ImageDownloadCache();
            for (var i = 0; i < 21; i++) cache.Put(i.ToString(), [1]);
            Check(cache.Get("0", 1) == null && cache.Get("20", 1) != null, "20-item eviction");
            cache.Clear();
            var big = new byte[16 * 1024 * 1024];
            for (var i = 0; i < 5; i++) cache.Put(i.ToString(), big);
            Check(cache.Get("0", big.Length) == null && cache.Get("4", big.Length) != null, "64 MiB eviction");
            Check(cache.Get("4", 1) == null, "Size mismatch invalidates cache");
            cache.Clear();
            Check(cache.Get("3", big.Length) == null, "Cache cleanup");
            Console.WriteLine("PASS: loopback download/progress, cache reuse/reload/session isolation, truncated/oversized responses, stalled-body cancellation, cache eviction/validation/cleanup.");
        }
        finally { shutdown.Cancel(); listener.Stop(); await server; }
    }
}
