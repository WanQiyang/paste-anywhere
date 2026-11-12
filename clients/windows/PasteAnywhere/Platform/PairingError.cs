using System.IO;
using System.Net;
using System.Net.Http;
using System.Security.Cryptography;
using System.Text.Json;

namespace PasteAnywhere.Platform;

internal static class PairingError
{
    public static string Describe(Exception error)
    {
        System.Diagnostics.Trace.TraceError("Pairing failed: {0}", error);
        if (error is HttpRequestException { StatusCode: { } status })
        {
            var message = status switch
            {
                HttpStatusCode.TooManyRequests => "请求过于频繁，请稍后重试",
                _ => "配对失败"
            };
            return $"{message}（{(int)status}）。";
        }
        return error switch
        {
            UriFormatException or ArgumentException => "服务器地址格式不正确。",
            HttpRequestException { HttpRequestError: HttpRequestError.SecureConnectionError } => "无法建立安全连接。",
            HttpRequestException => "无法连接服务器。",
            OperationCanceledException => "无法连接服务器。",
            JsonException => "服务器返回数据不正确。",
            IOException or UnauthorizedAccessException or CryptographicException => "无法保存设备凭证。",
            _ => "配对未完成。"
        };
    }
}
