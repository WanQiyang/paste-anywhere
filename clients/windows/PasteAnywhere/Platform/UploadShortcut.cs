using System.Runtime.InteropServices;
using System.Windows.Input;
using System.Windows.Interop;

namespace PasteAnywhere.Platform;

public sealed class UploadShortcut : IDisposable
{
    private readonly HwndSource source;
    private readonly Action action;
    private bool registered;
    private const int HotkeyId = 0x5041;

    public UploadShortcut(nint handle, Action action)
    {
        source = HwndSource.FromHwnd(handle);
        this.action = action;
        source.AddHook(Hook);
    }

    public bool Register(string gesture)
    {
        var key = (KeyGesture?)new KeyGestureConverter().ConvertFromInvariantString(gesture) ?? throw new ArgumentException("无效快捷键");
        uint modifiers = 0x4000; // MOD_NOREPEAT
        if (key.Modifiers.HasFlag(ModifierKeys.Control)) modifiers |= 2;
        if (key.Modifiers.HasFlag(ModifierKeys.Alt)) modifiers |= 1;
        if (key.Modifiers.HasFlag(ModifierKeys.Shift)) modifiers |= 4;
        if (key.Modifiers.HasFlag(ModifierKeys.Windows)) modifiers |= 8;
        if (modifiers == 0x4000) throw new ArgumentException("快捷键需要至少一个修饰键");
        if (registered) UnregisterHotKey(source.Handle, HotkeyId);
        registered = RegisterHotKey(source.Handle, HotkeyId, modifiers, (uint)KeyInterop.VirtualKeyFromKey(key.Key));
        return registered;
    }

    private nint Hook(nint hwnd, int message, nint wParam, nint lParam, ref bool handled)
    {
        if (message == 0x0312 && wParam == HotkeyId) { handled = true; action(); }
        return 0;
    }

    [DllImport("user32.dll")] private static extern bool RegisterHotKey(nint hwnd, int id, uint modifiers, uint key);
    [DllImport("user32.dll")] private static extern bool UnregisterHotKey(nint hwnd, int id);
    public void Dispose() { if (registered) UnregisterHotKey(source.Handle, HotkeyId); source.RemoveHook(Hook); }
}
