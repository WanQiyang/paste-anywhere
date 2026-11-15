using System.IO;
using System.Text.Json;

namespace PasteAnywhere.Platform;

public sealed record UserPreferences
{
    public bool AutoOverwriteClipboard { get; init; } = true;
    public bool NotifyUploadSuccess { get; init; } = true;
    public bool NotifyClipboardOverwrite { get; init; } = true;

    private static readonly string FilePath = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "PasteAnywhere", "preferences.json");

    public static UserPreferences Load() => File.Exists(FilePath)
        ? JsonSerializer.Deserialize<UserPreferences>(File.ReadAllText(FilePath)) ?? new()
        : new();

    public void Save()
    {
        Directory.CreateDirectory(Path.GetDirectoryName(FilePath)!);
        File.WriteAllText(FilePath + ".tmp", JsonSerializer.Serialize(this));
        File.Move(FilePath + ".tmp", FilePath, true);
    }
}
