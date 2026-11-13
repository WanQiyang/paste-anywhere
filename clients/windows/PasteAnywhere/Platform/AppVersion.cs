namespace PasteAnywhere.Platform;

public static class AppVersion
{
    public static string Number
    {
        get
        {
            var version = typeof(App).Assembly.GetName().Version!;
            return $"{version.Major}.{version.Minor}.{version.Build}";
        }
    }
    public static string Display => $"v {Number}";
}
