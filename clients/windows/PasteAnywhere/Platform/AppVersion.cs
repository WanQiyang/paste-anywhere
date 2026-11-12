namespace PasteAnywhere.Platform;

public static class AppVersion
{
    public static string Display
    {
        get
        {
            var version = typeof(App).Assembly.GetName().Version!;
            return $"v {version.Major}.{version.Minor}.{version.Build}";
        }
    }
}
