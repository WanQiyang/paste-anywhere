using System.IO;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Net.WebSockets;
using System.Text.Json;

namespace PasteAnywhere.Networking;

public record RemoteUser(string Id, string Name);
public record RemoteDevice(string Id, string UserId, string Name);
public record Connection(RemoteDevice Device, RemoteUser User, string Token);
public record Attachment(string Id, string Name, string Mime, long Size, int Width, int Height);
public record Entry(string Id, string UserId, string SenderId, string SourceDeviceId, string Kind, string Text, long CreatedAt, Attachment? Attachment = null)
{
    public string Summary => Kind == "image" ? $"图片 · {Attachment?.Width} × {Attachment?.Height}" : Text;
}
public record RemoteEvent(string Type, string? ItemId, string? SourceDeviceId);
public record Settings(string Server, string DeviceName, Connection Connection, string Shortcut);

public sealed class RemoteClient : IDisposable
{
    private static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);
    private readonly HttpClient http;
    private readonly Uri address;
    private readonly string token;

    public static Uri ValidateAddress(string server)
    {
        var uri = new Uri(server.TrimEnd('/') + "/", UriKind.Absolute);
        if (uri.Scheme != "https" && uri.Scheme != "http") throw new ArgumentException("服务器地址需要 http:// 或 https://");
        if (!string.IsNullOrEmpty(uri.UserInfo) || !string.IsNullOrEmpty(uri.Query) || !string.IsNullOrEmpty(uri.Fragment))
            throw new ArgumentException("服务器地址不能包含账号、查询参数或片段");
        return uri;
    }
    public RemoteClient(string server, string token)
    {
        address = ValidateAddress(server);
        this.token = token;
        http = new HttpClient { BaseAddress = address, Timeout = TimeSpan.FromSeconds(60) };
        if (token.Length > 0) http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
    }
    private static async Task<T> Response<T>(HttpResponseMessage response, CancellationToken ct)
    {
        using (response)
        {
            if (!response.IsSuccessStatusCode)
                throw new HttpRequestException($"服务端返回 {(int)response.StatusCode}：{await response.Content.ReadAsStringAsync(ct)}");
            return (await response.Content.ReadFromJsonAsync<T>(Json, ct))!;
        }
    }
    public Task<Connection> Connect(string invite, string name, CancellationToken ct = default) =>
        Post<Connection>("v1/connect", new { invite, deviceName = name }, ct);
    public Task<Entry> Upload(string text, CancellationToken ct = default) => Post<Entry>("v1/entries", new { text }, ct);
    public async Task<Entry> UploadImage(byte[] image, CancellationToken ct = default)
    {
        if (image.Length > 16 * 1024 * 1024) throw new IOException("图片超过客户端 16 MiB 上限");
        using var body = new ByteArrayContent(image);
        body.Headers.ContentType = new MediaTypeHeaderValue("application/octet-stream");
        return await Response<Entry>(await http.PostAsync("v1/images", body, ct), ct);
    }
    public async Task<byte[]> DownloadImage(Entry entry, CancellationToken ct)
    {
        if (entry.Attachment == null || entry.Attachment.Size > 16 * 1024 * 1024) throw new IOException("图片元数据无效或超过 16 MiB");
        using var response = await http.GetAsync($"v1/entries/{entry.Id}/image", HttpCompletionOption.ResponseHeadersRead, ct);
        response.EnsureSuccessStatusCode();
        using var source = await response.Content.ReadAsStreamAsync(ct);
        using var output = new MemoryStream();
        var buffer = new byte[81920];
        int count;
        while ((count = await source.ReadAsync(buffer, ct)) > 0)
        {
            if (output.Length + count > 16 * 1024 * 1024) throw new IOException("图片超过 16 MiB");
            output.Write(buffer, 0, count);
        }
        if (output.Length != entry.Attachment.Size) throw new IOException("图片下载不完整");
        return output.ToArray();
    }
    public Task<Entry> Send(string id, string userId, CancellationToken ct = default) => Post<Entry>($"v1/entries/{id}/send", new { userId }, ct);
    private async Task<T> Post<T>(string path, object body, CancellationToken ct) => await Response<T>(await http.PostAsJsonAsync(path, body, ct), ct);
    public async Task<Entry?> Latest(CancellationToken ct = default) => await Response<Entry?>(await http.GetAsync("v1/clipboard", ct), ct);
    public async Task<Entry> Get(string id, CancellationToken ct) => await Response<Entry>(await http.GetAsync($"v1/entries/{id}", ct), ct);
    public async Task<List<RemoteUser>> Users(CancellationToken ct = default) => await Response<List<RemoteUser>>(await http.GetAsync("v1/users", ct), ct);
    public async Task<List<Entry>> History(CancellationToken ct = default) => await Response<List<Entry>>(await http.GetAsync("v1/history", ct), ct);

    public async Task Listen(Action<RemoteEvent> receive, Action<string> status, CancellationToken ct)
    {
        while (!ct.IsCancellationRequested)
        {
            try
            {
                using var ws = new ClientWebSocket();
                ws.Options.SetRequestHeader("Authorization", "Bearer " + token);
                var uri = new UriBuilder(new Uri(address, "v1/events")) { Scheme = address.Scheme == "https" ? "wss" : "ws" };
                await ws.ConnectAsync(uri.Uri, ct);
                var buffer = new byte[4096];
                while (ws.State == WebSocketState.Open)
                {
                    using var message = new MemoryStream();
                    WebSocketReceiveResult part;
                    do
                    {
                        part = await ws.ReceiveAsync(buffer, ct);
                        if (part.MessageType == WebSocketMessageType.Close) throw new IOException("连接关闭");
                        message.Write(buffer, 0, part.Count);
                        if (message.Length > 65536) throw new IOException("通知过大");
                    } while (!part.EndOfMessage);
                    var item = JsonSerializer.Deserialize<RemoteEvent>(message.ToArray(), Json);
                    if (item?.Type == "ready") status("在线：等待新内容，重连不会补取历史");
                    else if (item != null) receive(item);
                }
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested) { break; }
            catch (Exception ex) { status("连接中断，3 秒后重试：" + ex.Message); }
            try { await Task.Delay(3000, ct); } catch (OperationCanceledException) { break; }
        }
    }
    public void Dispose() => http.Dispose();
}
