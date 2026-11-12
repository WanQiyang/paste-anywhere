using System.Windows;
using System.Windows.Interop;
using PasteAnywhere.Platform;
using PasteAnywhere.Views;

namespace PasteAnywhere;

public partial class App : System.Windows.Application
{
    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        var window = new MainWindow();
        MainWindow = window;
        if (e.Args.Contains(StartupRegistration.TrayArgument, StringComparer.OrdinalIgnoreCase))
        {
            // Create the HWND and initialize the tray/hotkey/session without
            // ever showing the window (Show followed by Hide would flash).
            new WindowInteropHelper(window).EnsureHandle();
        }
        else window.Show();
    }
}
