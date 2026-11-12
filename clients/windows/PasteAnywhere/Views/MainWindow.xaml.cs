using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media.Imaging;
using PasteAnywhere.Networking;
using PasteAnywhere.Platform;

namespace PasteAnywhere.Views;

public partial class MainWindow : Window
{
    private RemoteClient? client;
    private Settings? settings;
    private Entry? current;
    private ClipboardContent? content;
    private BitmapSource? bitmap;
    private int viewVersion;
    private CancellationTokenSource session = new();
    private UploadShortcut? shortcut;
    private System.Windows.Forms.NotifyIcon? tray;
    private long newest;
    private bool capturing;
    private bool exiting;
    public MainWindow()
    {
        InitializeComponent();
        DeviceName.Text = Environment.MachineName;
        SourceInitialized += Initialize;
        Closing += OnClosing;
    }
    private async void Initialize(object? sender, EventArgs args)
    {
        shortcut = new UploadShortcut(new WindowInteropHelper(this).Handle, () => _ = UploadClipboard());
        var menu = new System.Windows.Forms.ContextMenuStrip();
        menu.Items.Add("打开", null, (_, _) => Dispatcher.Invoke(ShowWindow));
        menu.Items.Add("退出", null, (_, _) => Dispatcher.Invoke(() => { exiting = true; Close(); }));
        tray = new System.Windows.Forms.NotifyIcon { Icon = System.Drawing.SystemIcons.Application, Text = "Paste Anywhere", Visible = true, ContextMenuStrip = menu };
        tray.DoubleClick += (_, _) => Dispatcher.Invoke(ShowWindow);
        try
        {
            settings = CredentialStore.Load();
            if (settings != null)
            {
                ServerAddress.Text = settings.Server; DeviceName.Text = settings.DeviceName; ShortcutText.Text = settings.Shortcut;
                SetShortcut();
                await StartSession(settings);
            }
            else SetShortcut();
        }
        catch (Exception ex) { Status(ex.Message); }
    }
    private void ShowWindow() { Show(); WindowState = WindowState.Normal; Activate(); }
    private void Status(string text) => Dispatcher.Invoke(() => StatusText.Text = text);
    private void SetShortcut()
    {
        if (shortcut?.Register(ShortcutText.Text) != true) { Status("快捷键注册失败，可能已被占用，请换一组键"); return; }
        if (settings != null) { settings = settings with { Shortcut = ShortcutText.Text }; CredentialStore.Save(settings); }
    }
    private void ShortcutClick(object sender, RoutedEventArgs e) { try { SetShortcut(); } catch (Exception ex) { Status(ex.Message); } }
    private async void ConnectClick(object sender, RoutedEventArgs e)
    {
        ConnectButton.IsEnabled = false;
        try
        {
            Settings value;
            if (Invite.Password.Length == 0 && settings != null && settings.Server == ServerAddress.Text)
                value = settings;
            else
            {
                using var login = new RemoteClient(ServerAddress.Text, "");
                var connection = await login.Connect(Invite.Password.Trim(), DeviceName.Text.Trim());
                value = new Settings(ServerAddress.Text, DeviceName.Text, connection, ShortcutText.Text);
            }
            CredentialStore.Save(value); settings = value; Invite.Clear();
            await StartSession(value); SetShortcut();
        }
        catch (Exception ex) { Status(ex.Message); }
        finally { ConnectButton.IsEnabled = true; }
    }
    private async Task StartSession(Settings value)
    {
        session.Cancel(); client?.Dispose();
        session = new CancellationTokenSource(); newest = 0; current = null;
        content = null; bitmap = null; ++viewVersion; ContentImage.Source = null; ContentImage.Visibility = Visibility.Collapsed;
        client = new RemoteClient(value.Server, value.Connection.Token);
        AccountText.Text = $"当前用户：{value.Connection.User.Name} · 设备：{value.Connection.Device.Name}";
        var api = client; var ct = session.Token;
        ContentText.Text = "等待新内容；启动和重连不会自动替换剪贴板。";
        _ = api.Listen(ev => Dispatcher.Invoke(() => Receive(ev, api, value, ct)), Status, ct);
        UsersList.ItemsSource = (await api.Users(ct)).Where(u => u.Id != value.Connection.User.Id).ToList();
    }
    private void Receive(RemoteEvent ev, RemoteClient api, Settings value, CancellationToken ct)
    {
        if (ct.IsCancellationRequested || ev.Type != "clipboard.updated" || ev.SourceDeviceId == value.Connection.Device.Id ||
            !long.TryParse(ev.ItemId, out var id) || id <= newest) return;
        newest = id;
        ++viewVersion;
        _ = Download(id, api, ct);
    }
    private async Task Download(long id, RemoteClient api, CancellationToken ct)
    {
        try
        {
            var entry = await api.Get(id.ToString(), ct);
            var prepared = entry.Kind == "image" ? new ClipboardContent(null, await api.DownloadImage(entry, ct)) : new ClipboardContent(entry.Text, null);
            var decoded = prepared.Image == null ? null : ClipboardContent.Decode(prepared.Image);
            for (int attempt = 0; attempt < 10; attempt++)
            {
                if (ct.IsCancellationRequested || id != newest) return;
                try
                {
                    prepared.Write(decoded);
                    ShowEntry(entry, prepared, decoded); Status("已更新系统剪贴板，可直接 Ctrl+V"); return;
                }
                catch (COMException) when (attempt < 9) { await Task.Delay(100, ct); }
            }
        }
        catch (OperationCanceledException) { }
        catch (Exception ex) { if (!ct.IsCancellationRequested && id == newest) Status("接收失败，保留原剪贴板：" + ex.Message); }
    }
    private async Task UploadClipboard()
    {
        if (capturing || client == null) return;
        capturing = true; var api = client; var ct = session.Token;
        try
        {
            // Snapshot on the UI thread before awaiting network I/O. No simulated copy
            // or clipboard write: subsequent local changes cannot change this upload.
            var captured = ClipboardContent.Read();
            if (ct.IsCancellationRequested) return;
            var entry = captured.Image != null ? await api.UploadImage(captured.Image, ct) : await api.Upload(captured.Text ?? "", ct);
            if (ct.IsCancellationRequested) return;
            if (long.Parse(entry.Id) >= newest) { newest = long.Parse(entry.Id); ++viewVersion; ShowEntry(entry, captured, captured.Image == null ? null : ClipboardContent.Decode(captured.Image)); }
            Status("当前剪贴板内容已上传");
        }
        catch (Exception ex) { if (!ct.IsCancellationRequested) Status("上传失败：" + ex.Message); }
        finally { capturing = false; }
    }
    private void ShowEntry(Entry entry, ClipboardContent prepared, BitmapSource? decoded)
    {
        current = entry; content = prepared; bitmap = decoded;
        ContentText.Text = entry.Summary;
        ContentImage.Source = decoded; ContentImage.Visibility = decoded == null ? Visibility.Collapsed : Visibility.Visible;
    }
    private async Task Preview(Entry entry, RemoteClient api, CancellationToken ct, int version)
    {
        var prepared = entry.Kind == "image" ? new ClipboardContent(null, await api.DownloadImage(entry, ct)) : new ClipboardContent(entry.Text, null);
        if (ct.IsCancellationRequested || version != viewVersion) return;
        ShowEntry(entry, prepared, prepared.Image == null ? null : ClipboardContent.Decode(prepared.Image));
    }
    private async void RefreshClick(object sender, RoutedEventArgs e)
    {
        if (client == null) return;
        var api = client; var ct = session.Token; var watermark = newest; var version = ++viewVersion;
        try
        {
            var item = await api.Latest(ct);
            if (ct.IsCancellationRequested) return;
            if (item != null && long.Parse(item.Id) >= newest) await Preview(item, api, ct, version);
            else if (item == null && watermark == newest && version == viewVersion) { current = null; content = null; bitmap = null; ContentImage.Source = null; ContentImage.Visibility = Visibility.Collapsed; ContentText.Text = ""; Status("远程剪贴板为空"); }
            var users = await api.Users(ct);
            if (!ct.IsCancellationRequested) UsersList.ItemsSource = users.Where(u => u.Id != settings?.Connection.User.Id).ToList();
        }
        catch (Exception ex) { Status(ex.Message); }
    }
    private void CopyClick(object sender, RoutedEventArgs e)
    {
        if (content == null) return;
        try { content.Write(bitmap); Status("已复制当前内容"); } catch (Exception ex) { Status(ex.Message); }
    }
    private void DragContent(object sender, System.Windows.Input.MouseEventArgs e)
    {
        if (e.LeftButton == MouseButtonState.Pressed && current != null)
            System.Windows.DragDrop.DoDragDrop((DependencyObject)sender, new System.Windows.DataObject("PasteAnywhere.Entry", current.Id), System.Windows.DragDropEffects.Copy);
    }
    private async void SendDrop(object sender, System.Windows.DragEventArgs e)
    {
        if (client == null || e.Data.GetData("PasteAnywhere.Entry") is not string id) return;
        var element = ItemsControl.ContainerFromElement(UsersList, e.OriginalSource as DependencyObject) as ListBoxItem;
        if (element?.DataContext is not RemoteUser user) return;
        try { await client.Send(id, user.Id, session.Token); Status("已发送给 " + user.Name); }
        catch (Exception ex) { Status(ex.Message); }
    }
    private async void HistoryClick(object sender, RoutedEventArgs e)
    {
        if (client == null) return;
        try { HistoryList.ItemsSource = await client.History(session.Token); HistoryList.Visibility = Visibility.Visible; }
        catch (Exception ex) { Status(ex.Message); }
    }
    private async void SelectHistory(object sender, SelectionChangedEventArgs e)
    {
        if (HistoryList.SelectedItem is not Entry entry || client == null) return;
        try { await Preview(entry, client, session.Token, ++viewVersion); } catch (Exception ex) { Status(ex.Message); }
    }
    private void OnClosing(object? sender, CancelEventArgs e)
    {
        if (!exiting) { e.Cancel = true; Hide(); return; }
        session.Cancel(); client?.Dispose(); shortcut?.Dispose(); tray?.Dispose();
    }
}
