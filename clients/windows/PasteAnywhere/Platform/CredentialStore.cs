using System.IO;
using System.Security.Cryptography;
using System.Text.Json;
using PasteAnywhere.Networking;

namespace PasteAnywhere.Platform;

public static class CredentialStore
{
    private static readonly string DirectoryPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "PasteAnywhere");
    private static readonly string FilePath = Path.Combine(DirectoryPath, "connection.bin");
    public static Settings? Load() => !File.Exists(FilePath) ? null : JsonSerializer.Deserialize<Settings>(
        ProtectedData.Unprotect(File.ReadAllBytes(FilePath), null, DataProtectionScope.CurrentUser));
    public static void Save(Settings settings)
    {
        Directory.CreateDirectory(DirectoryPath);
        var bytes = ProtectedData.Protect(JsonSerializer.SerializeToUtf8Bytes(settings), null, DataProtectionScope.CurrentUser);
        File.WriteAllBytes(FilePath + ".tmp", bytes);
        File.Move(FilePath + ".tmp", FilePath, true);
    }
}
