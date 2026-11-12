using PasteAnywhere.Networking;

namespace PasteAnywhere.Views;

public record UserCard(RemoteUser User, bool IsSelf)
{
    public string Name => User.Name;
    public string Initial => Name.Length == 0 ? "?" : Name[..1].ToUpperInvariant();
}

public record ClipboardCard(Entry? Entry, int Position)
{
    public bool HasContent => Entry != null;
    public string Number => Position.ToString("00");
    public string Preview => Entry == null ? "暂无内容" : Entry.Kind == "image" ? "<image>" :
        Entry.Text[..Math.Min(Entry.Text.Length, 240)].Replace('\r', ' ').Replace('\n', ' ').Replace('\t', ' ');
    public string Time => Entry == null ? "" : DateTimeOffset.FromUnixTimeMilliseconds(Entry.CreatedAt).ToLocalTime().ToString("MM/dd HH:mm");
}
