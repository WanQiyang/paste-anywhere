using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Media.Effects;
using System.Windows.Threading;
using PasteAnywhere.Networking;
using PasteAnywhere.Platform;
using Point = System.Windows.Point;
using Color = System.Windows.Media.Color;
using ColorConverter = System.Windows.Media.ColorConverter;

namespace PasteAnywhere.Views;

public partial class MainWindow : Window
{
    private RemoteClient? client;
    private Settings? settings;
    private UserPreferences preferences = new();
    private int clipboardRevision;
    private CancellationTokenSource session = new();
    private UploadShortcut? shortcut;
    private string shortcutText = "Ctrl+Win+Alt+C";
    private System.Windows.Forms.NotifyIcon? tray;
    private System.Drawing.Icon? trayIcon;
    private readonly DispatcherTimer dashboardTimer = new() { Interval = TimeSpan.FromSeconds(5) };
    private readonly DispatcherTimer holdTimer = new() { Interval = TimeSpan.FromMilliseconds(500) };
    private readonly DispatcherTimer dragTimer = new() { Interval = TimeSpan.FromMilliseconds(16) };
    private readonly DispatcherTimer toastTimer = new() { Interval = TimeSpan.FromSeconds(2.4) };
    private List<Entry> entries = [];
    private List<RemoteUser> users = [];
    private long newest;
    private long clearedThrough;
    private bool clearing;
    private bool confirming;
    private int dashboardRevision;
    private bool refreshing;
    private bool refreshAgain;
    private bool connected;
    private bool capturing;
    private bool exiting;
    private bool loggingIn;
    private bool loggingOut;
    private bool dragging;
    private bool didDrag;
    private Point dragOrigin;
    private ClipboardCard? dragCard;
    private FrameworkElement? dragSource;
    private CardDragAdorner? dragPreview;
    private Border? highlightedUser;

    public MainWindow()
    {
        InitializeComponent();
        RenderCards();
        SourceInitialized += Initialize;
        Closing += OnClosing;
        IsVisibleChanged += (_, _) => { if (IsVisible) _ = RefreshDashboard(); };
        dashboardTimer.Tick += (_, _) => { if (connected) _ = RefreshDashboard(); };
        dashboardTimer.Start();
        holdTimer.Tick += (_, _) =>
        {
            holdTimer.Stop();
            if (System.Windows.Input.Mouse.LeftButton == MouseButtonState.Pressed && dragSource != null) BeginCardDrag();
        };
        dragTimer.Tick += (_, _) => UpdateDragFeedback();
        toastTimer.Tick += (_, _) => { toastTimer.Stop(); ToastPanel.Visibility = Visibility.Collapsed; };
    }

    private async void Initialize(object? sender, EventArgs args)
    {
        shortcut = new UploadShortcut(new WindowInteropHelper(this).Handle, () => _ = UploadClipboard());
        var menu = new System.Windows.Forms.ContextMenuStrip();
        menu.Items.Add("打开", null, (_, _) => Dispatcher.Invoke(ShowWindow));
        menu.Items.Add("设置", null, (_, _) => Dispatcher.Invoke(ConfigureSettings));
        menu.Items.Add("退出", null, (_, _) => Dispatcher.Invoke(() => { exiting = true; Close(); }));
        using (var stream = System.Windows.Application.GetResourceStream(new Uri("pack://application:,,,/Assets/app.ico")).Stream)
            trayIcon = new System.Drawing.Icon(stream, 32, 32);
        tray = new System.Windows.Forms.NotifyIcon { Icon = trayIcon, Text = "Paste Anywhere", Visible = true, ContextMenuStrip = menu };
        tray.DoubleClick += (_, _) => Dispatcher.Invoke(ShowWindow);
        try { preferences = UserPreferences.Load(); }
        catch (Exception ex) { System.Diagnostics.Trace.TraceError("Loading preferences failed: {0}", ex); Status("无法读取本机设置"); }
        try
        {
            settings = CredentialStore.Load();
            if (settings != null) shortcutText = settings.Shortcut;
            SetShortcut();
            if (settings != null) await StartSession(settings);
        }
        catch (Exception ex) { Status(ex.Message); }
    }

    private void ShowWindow() { Show(); WindowState = WindowState.Normal; Activate(); }
    private void Notify(string text)
    {
        try { tray?.ShowBalloonTip(5000, "Paste Anywhere", text, System.Windows.Forms.ToolTipIcon.Info); }
        catch (Exception ex) { System.Diagnostics.Trace.TraceError("Showing notification failed: {0}", ex); }
    }
    private void Status(string text) => Dispatcher.Invoke(() =>
    {
        LoginStatus.Text = text;
        toastTimer.Stop();
        if (string.IsNullOrEmpty(text) || LoginView.Visibility == Visibility.Visible || !IsVisible)
        {
            ToastPanel.Visibility = Visibility.Collapsed;
            return;
        }
        ToastText.Text = text;
        ToastPanel.Visibility = Visibility.Visible;
        ToastPanel.BeginAnimation(OpacityProperty, new DoubleAnimation(0, 1, TimeSpan.FromMilliseconds(120)));
        toastTimer.Start();
    });
    private void SessionStatus(string text, CancellationToken ct)
    {
        if (!ct.IsCancellationRequested) System.Diagnostics.Debug.WriteLine(text);
    }
    private void LoginInputChanged(object sender, RoutedEventArgs e) => UpdateLoginButton();
    private void UpdateLoginButton()
    {
        if (ConnectButton == null) return;
        ConnectButton.IsEnabled = !loggingIn && !string.IsNullOrWhiteSpace(ServerAddress.Text) &&
            !string.IsNullOrWhiteSpace(DeviceName.Text) && !string.IsNullOrWhiteSpace(Invite.Password);
    }
    private void SetShortcut()
    {
        if (shortcut?.Register(shortcutText) != true) Status("快捷键注册失败，可能已被占用");
    }
    private void ConfigureSettings()
    {
        ShowWindow();
        bool startupEnabled;
        try { startupEnabled = StartupRegistration.IsEnabled(); }
        catch (Exception ex) { System.Diagnostics.Trace.TraceError("Startup settings: {0}", ex); Status("无法读取开机自启设置"); return; }
        var input = new System.Windows.Controls.TextBox
        {
            Text = shortcutText, Height = 42, Margin = new Thickness(0, 8, 0, 22),
            Style = (Style)FindResource(typeof(System.Windows.Controls.TextBox))
        };
        System.Windows.Controls.CheckBox Option(string label, bool enabled) => new()
        {
            Content = label, IsChecked = enabled, FontSize = 14, Margin = new Thickness(0, 0, 0, 18)
        };
        var startup = Option("开机时自动启动", startupEnabled);
        var autoOverwrite = Option("自动覆盖系统剪贴板", preferences.AutoOverwriteClipboard);
        var notifyUpload = Option("上传在线剪贴板成功时通知", preferences.NotifyUploadSuccess);
        var notifyOverwrite = Option("系统剪贴板被覆盖时通知", preferences.NotifyClipboardOverwrite);
        var errorText = new TextBlock { Foreground = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#B45353")),
            TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12), Visibility = Visibility.Collapsed };
        var save = new System.Windows.Controls.Button { Content = "保存", Height = 40, IsDefault = true, Style = (Style)FindResource("LoginButton") };
        var panel = new StackPanel { Margin = new Thickness(24) };
        panel.Children.Add(new TextBlock { Text = "设置", FontSize = 22, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 24) });
        panel.Children.Add(new TextBlock { Text = "快捷键：将系统剪贴板上传", FontSize = 14 });
        panel.Children.Add(input); panel.Children.Add(startup);
        panel.Children.Add(autoOverwrite); panel.Children.Add(notifyUpload); panel.Children.Add(notifyOverwrite);
        panel.Children.Add(errorText); panel.Children.Add(save);
        var dialog = new Window { Title = "Paste Anywhere", Owner = this, Width = 380, SizeToContent = SizeToContent.Height,
            Background = Background, Foreground = Foreground, FontFamily = FontFamily, FontSize = 13,
            ResizeMode = ResizeMode.NoResize, WindowStartupLocation = WindowStartupLocation.CenterOwner, Content = panel };
        save.Click += (_, _) =>
        {
            var previous = shortcutText;
            var previousSettings = settings;
            var credentialWritten = false;
            var startupWritten = false;
            try
            {
                var candidate = input.Text.Trim();
                if (shortcut?.Register(candidate) != true)
                {
                    shortcut?.Register(previous); errorText.Text = "快捷键不可用，请换一组键"; errorText.Visibility = Visibility.Visible; return;
                }
                var updated = settings == null ? null : settings with { Shortcut = candidate };
                if (updated != null) { CredentialStore.Save(updated); credentialWritten = true; }
                StartupRegistration.SetEnabled(startup.IsChecked == true);
                startupWritten = true;
                var updatedPreferences = new UserPreferences
                {
                    AutoOverwriteClipboard = autoOverwrite.IsChecked == true,
                    NotifyUploadSuccess = notifyUpload.IsChecked == true,
                    NotifyClipboardOverwrite = notifyOverwrite.IsChecked == true
                };
                updatedPreferences.Save();
                if (preferences.AutoOverwriteClipboard != updatedPreferences.AutoOverwriteClipboard) ++clipboardRevision;
                preferences = updatedPreferences;
                settings = updated; shortcutText = candidate;
            }
            catch (Exception ex)
            {
                System.Diagnostics.Trace.TraceError("Saving settings failed: {0}", ex);
                shortcutText = previous; settings = previousSettings;
                try
                {
                    shortcut?.Register(previous);
                    if (startupWritten) StartupRegistration.SetEnabled(startupEnabled);
                    if (credentialWritten && previousSettings != null) CredentialStore.Save(previousSettings);
                }
                catch (Exception rollback) { System.Diagnostics.Trace.TraceError("Restoring settings failed: {0}", rollback); }
                errorText.Text = "保存设置失败，请重试"; errorText.Visibility = Visibility.Visible;
                return;
            }
            dialog.DialogResult = true;
        };
        dialog.ShowDialog();
    }
    private async void ConnectClick(object sender, RoutedEventArgs e)
    {
        if (!ConnectButton.IsEnabled || loggingIn) return;
        loggingIn = true; UpdateLoginButton(); LoginFields.IsEnabled = false;
        ConnectButton.Content = "配对中…"; LoginStatus.Text = "";
        try
        {
            var server = ServerAddress.Text.Trim();
            var device = DeviceName.Text.Trim();
            using var login = new RemoteClient(server, "");
            var connection = await login.Connect(Invite.Password.Trim(), device);
            var value = new Settings(server, connection, shortcutText);
            CredentialStore.Save(value); settings = value; Invite.Clear();
            await StartSession(value); SetShortcut();
        }
        catch (Exception ex)
        {
            var message = PairingError.Describe(ex);
            Status(message.StartsWith("配对失败") ? message : "配对失败：" + message);
        }
        finally { loggingIn = false; LoginFields.IsEnabled = true; ConnectButton.Content = "配对"; UpdateLoginButton(); }
    }
    private async Task StartSession(Settings value)
    {
        session.Cancel(); session.Dispose(); client?.Dispose();
        session = new CancellationTokenSource(); newest = 0; clearedThrough = 0; entries = []; users = []; ++dashboardRevision;
        client = new RemoteClient(value.Server, value.Connection.Token);
        AccountText.Text = $"{value.Connection.User.Name}-{value.Connection.Device.Name}";
        LoginView.Visibility = Visibility.Collapsed; MainView.Visibility = Visibility.Visible;
        Status(""); SetConnection(false); RenderCards();
        var api = client; var ct = session.Token;
        _ = api.Listen(ev => Dispatcher.Invoke(() => Receive(ev, api, value, ct)),
            online => Dispatcher.Invoke(() =>
            {
                if (ct.IsCancellationRequested) return;
                SetConnection(online);
                if (online) { Status(""); _ = RefreshDashboard(); }
            }), text => SessionStatus(text, ct), ct);
        await RefreshDashboard();
    }
    private void SetConnection(bool online)
    {
        connected = online;
        ConnectionLight.Fill = new SolidColorBrush((Color)ColorConverter.ConvertFromString(online ? "#22A675" : "#DF6363"));
        ConnectionLight.ToolTip = online ? "已连接" : "未连接";
        System.Windows.Automation.AutomationProperties.SetName(ConnectionLight, online ? "已连接" : "未连接");
        if (!online) users = [];
        RenderUsers();
        UpdateClearButton();
    }
    private void RenderUsers()
    {
        if (settings == null) { UsersList.ItemsSource = null; return; }
        var self = settings.Connection.User;
        var count = connected ? users.FirstOrDefault(u => u.Id == self.Id)?.OnlineDevices : null;
        AccountText.Text = $"{self.Name}-{settings.Connection.Device.Name}" +
            (connected && count.HasValue ? $" ({count.Value} 台在线)" : "");
        var cards = new List<UserCard> { new(self, true) };
        if (connected)
            cards.AddRange(users.Where(u => u.OnlineDevices > 0 && u.Id != self.Id)
                .OrderBy(u => u.Name, StringComparer.OrdinalIgnoreCase).ThenBy(u => u.Name, StringComparer.Ordinal)
                .Select(u => new UserCard(u, false)));
        UsersList.ItemsSource = cards;
    }
    private void RenderCards()
    {
        ClipboardList.ItemsSource = Enumerable.Range(0, 5)
            .Select(index => new ClipboardCard(index < entries.Count ? entries[index] : null, index + 1)).ToList();
        UpdateClearButton();
    }
    private void UpdateClearButton()
    {
        ClearButton.IsEnabled = connected && entries.Count > 0 && !clearing && !loggingOut && !capturing && !confirming;
        PasteButton.IsEnabled = connected && !clearing && !loggingOut && !capturing && !confirming;
        LogoutButton.IsEnabled = !clearing && !loggingOut && !capturing && !confirming;
    }
    private bool ConfirmOperation(string message)
    {
        confirming = true; UpdateClearButton();
        try
        {
            return new ConfirmationWindow(this, message).ShowDialog() == true;
        }
        finally { confirming = false; UpdateClearButton(); }
    }
    private async void PasteClick(object sender, RoutedEventArgs e)
    {
        if (!connected) return;
        await UploadClipboard();
    }
    private void ApplyClear(string? through)
    {
        if (!long.TryParse(through, out var id) || id <= clearedThrough) return;
        clearedThrough = id; newest = Math.Max(newest, id); ++dashboardRevision;
        entries = entries.Where(entry => long.Parse(entry.Id) > id).ToList();
        if (dragCard == null) RenderCards();
        else UpdateClearButton();
    }
    private async void ClearClick(object sender, RoutedEventArgs e)
    {
        if (client == null || clearing || loggingOut || capturing || confirming || !connected || entries.Count == 0) return;
        if (!ConfirmOperation("确定清空在线剪贴板？")) return;
        if (client == null || !connected || entries.Count == 0) return;
        clearing = true; ClearButton.Content = "清空中…"; UpdateClearButton();
        var api = client; var ct = session.Token;
        try
        {
            var result = await api.ClearClipboard(ct);
            if (ct.IsCancellationRequested) return;
            ApplyClear(result.ItemId); Status("已清空"); _ = RefreshDashboard();
        }
        catch (Exception) { if (!ct.IsCancellationRequested) Status("清空失败，请重试"); }
        finally { clearing = false; ClearButton.Content = "清空"; UpdateClearButton(); }
    }
    private void MergeEntry(Entry entry)
    {
        if (long.Parse(entry.Id) <= clearedThrough) return;
        ++dashboardRevision;
        entries = entries.Where(e => e.Id != entry.Id).Append(entry).OrderByDescending(e => long.Parse(e.Id)).Take(5).ToList();
        if (dragCard == null) RenderCards();
    }
    private async Task RefreshDashboard()
    {
        if (client == null || !IsVisible || loggingOut || dragCard != null) return;
        if (refreshing) { refreshAgain = true; return; }
        refreshing = true;
        try
        {
            do
            {
                refreshAgain = false;
                var api = client; var value = settings; var ct = session.Token; var revision = dashboardRevision;
                if (api == null || value == null) return;
                var usersTask = api.Users(ct); var historyTask = api.History(ct);
                try
                {
                    await Task.WhenAll(usersTask, historyTask);
                    if (ct.IsCancellationRequested || api != client) return;
                    if (dragCard != null) return;
                    users = await usersTask; RenderUsers();
                    if (revision == dashboardRevision) { entries = (await historyTask).Where(entry => long.Parse(entry.Id) > clearedThrough).Take(5).ToList(); RenderCards(); }
                    else refreshAgain = true;
                }
                catch (OperationCanceledException) { return; }
                catch (Exception ex) { System.Diagnostics.Debug.WriteLine(ex); return; }
            } while (refreshAgain && IsVisible && !loggingOut);
        }
        finally { refreshing = false; }
    }
    private void Receive(RemoteEvent ev, RemoteClient api, Settings value, CancellationToken ct)
    {
        if (!ct.IsCancellationRequested && ev.Type == "clipboard.cleared") { ApplyClear(ev.ItemId); _ = RefreshDashboard(); return; }
        if (ct.IsCancellationRequested || ev.Type != "clipboard.updated" || ev.SourceDeviceId == value.Connection.Device.Id ||
            !long.TryParse(ev.ItemId, out var id) || id <= newest) return;
        newest = id; ++dashboardRevision;
        _ = Download(id, api, preferences.AutoOverwriteClipboard, clipboardRevision, ct); _ = RefreshDashboard();
    }
    private async Task Download(long id, RemoteClient api, bool overwrite, int revision, CancellationToken ct)
    {
        try
        {
            var entry = await api.Get(id.ToString(), ct);
            if (ct.IsCancellationRequested || id <= clearedThrough) return;
            MergeEntry(entry);
            if (!overwrite || revision != clipboardRevision || id != newest) return;
            var prepared = entry.Kind == "image" ? new ClipboardContent(null, await api.DownloadImage(entry, ct)) : new ClipboardContent(entry.Text, null);
            var bitmap = prepared.Image == null ? null : ClipboardContent.Decode(prepared.Image);
            for (int attempt = 0; attempt < 10; attempt++)
            {
                if (ct.IsCancellationRequested || id != newest || id <= clearedThrough || revision != clipboardRevision) return;
                try
                {
                    prepared.Write(bitmap); Status("已更新系统剪贴板");
                    if (preferences.NotifyClipboardOverwrite) Notify("系统剪贴板已更新");
                    return;
                }
                catch (COMException) when (attempt < 9) { await Task.Delay(100, ct); }
            }
        }
        catch (OperationCanceledException) { }
        catch (Exception) { if (!ct.IsCancellationRequested && id == newest && id > clearedThrough && revision == clipboardRevision) Status("接收失败，已保留本地剪贴板"); }
    }
    private async Task UploadClipboard()
    {
        if (capturing || client == null || !connected || loggingOut || clearing || confirming) return;
        capturing = true; var api = client; var ct = session.Token;
        UpdateClearButton();
        try
        {
            var captured = ClipboardContent.Read();
            if (ct.IsCancellationRequested) return;
            var entry = captured.Image != null ? await api.UploadImage(captured.Image, ct) : await api.Upload(captured.Text ?? "", ct);
            if (ct.IsCancellationRequested || long.Parse(entry.Id) <= clearedThrough) return;
            newest = Math.Max(newest, long.Parse(entry.Id)); MergeEntry(entry);
            Status("已发送当前剪贴板"); _ = RefreshDashboard();
            if (preferences.NotifyUploadSuccess) Notify("已成功上传在线剪贴板");
        }
        catch (System.IO.IOException ex) { if (!ct.IsCancellationRequested) Status(ex.Message); }
        catch (Exception) { if (!ct.IsCancellationRequested) Status("上传失败，请检查连接后重试"); }
        finally { capturing = false; UpdateClearButton(); }
    }
    private void CardMouseDown(object sender, MouseButtonEventArgs e)
    {
        holdTimer.Stop();
        dragOrigin = e.GetPosition(this); dragSource = sender as FrameworkElement;
        dragCard = dragSource?.DataContext as ClipboardCard; didDrag = false;
        if (dragCard?.Entry == null) { dragSource = null; dragCard = null; return; }
        dragSource?.CaptureMouse();
        if (connected) holdTimer.Start();
    }
    private void CardCaptureLost(object sender, System.Windows.Input.MouseEventArgs e)
    {
        if (dragging) return;
        holdTimer.Stop(); dragSource = null; dragCard = null;
    }
    private void DragContent(object sender, System.Windows.Input.MouseEventArgs e)
    {
        if (e.LeftButton != MouseButtonState.Pressed || dragCard?.Entry == null || dragging || !connected) return;
        var delta = e.GetPosition(this) - dragOrigin;
        // Small pointer movements during a click should not consume the preview gesture.
        if (Math.Abs(delta.X) < Math.Max(8, SystemParameters.MinimumHorizontalDragDistance) &&
            Math.Abs(delta.Y) < Math.Max(8, SystemParameters.MinimumVerticalDragDistance)) return;
        BeginCardDrag();
    }
    private void BeginCardDrag()
    {
        holdTimer.Stop();
        if (dragging || !connected || dragSource == null || !dragSource.IsMouseCaptured ||
            dragCard?.Entry == null || System.Windows.Input.Mouse.LeftButton != MouseButtonState.Pressed) return;
        var source = dragSource;
        var layer = AdornerLayer.GetAdornerLayer(SurfaceRoot);
        didDrag = true; dragging = true;
        source.ReleaseMouseCapture();
        try
        {
            dragPreview = new CardDragAdorner(SurfaceRoot, source, TranslatePoint(dragOrigin, source));
            layer?.Add(dragPreview);
            source.Opacity = 0.35;
            UpdateDragFeedback();
            dragTimer.Start();
            System.Windows.DragDrop.DoDragDrop(source, new System.Windows.DataObject("PasteAnywhere.Entry", dragCard.Entry.Id), System.Windows.DragDropEffects.Copy);
        }
        finally
        {
            dragTimer.Stop();
            if (dragPreview != null) layer?.Remove(dragPreview);
            dragPreview = null; source.ClearValue(OpacityProperty); HighlightUser(null);
            dragging = false; dragSource = null; dragCard = null; RenderCards(); _ = RefreshDashboard();
        }
    }
    [StructLayout(LayoutKind.Sequential)]
    private struct CursorPoint { public int X; public int Y; }
    [DllImport("user32.dll")]
    private static extern bool GetCursorPos(out CursorPoint point);
    private void UpdateDragFeedback()
    {
        if (!dragging || !GetCursorPos(out var cursor)) return;
        var position = SurfaceRoot.PointFromScreen(new Point(cursor.X, cursor.Y));
        dragPreview?.Move(position);
        var hit = SurfaceRoot.InputHitTest(position) as DependencyObject;
        Border? target = null;
        while (hit is Visual)
        {
            if (hit is Border border && border.DataContext is UserCard) { target = border; break; }
            hit = VisualTreeHelper.GetParent(hit);
        }
        HighlightUser(connected ? target : null);
        var relative = SurfaceRoot.TranslatePoint(position, UsersScroll);
        if (relative.Y >= 0 && relative.Y <= UsersScroll.ActualHeight)
        {
            if (relative.X >= 0 && relative.X < 22) UsersScroll.ScrollToHorizontalOffset(UsersScroll.HorizontalOffset - 4);
            else if (relative.X > UsersScroll.ActualWidth - 22 && relative.X <= UsersScroll.ActualWidth)
                UsersScroll.ScrollToHorizontalOffset(UsersScroll.HorizontalOffset + 4);
        }
    }
    private void HighlightUser(Border? target)
    {
        if (highlightedUser == target) return;
        if (highlightedUser != null)
        {
            highlightedUser.ClearValue(Border.BackgroundProperty); highlightedUser.ClearValue(Border.BorderBrushProperty);
            highlightedUser.ClearValue(Border.BorderThicknessProperty); highlightedUser.ClearValue(EffectProperty);
        }
        highlightedUser = target;
        if (target == null) return;
        target.Background = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#DFEAFF"));
        target.BorderBrush = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#245CDB"));
        target.BorderThickness = new Thickness(2);
        target.Effect = new DropShadowEffect { Color = (Color)ColorConverter.ConvertFromString("#245CDB"), BlurRadius = 12, ShadowDepth = 0, Opacity = 0.18 };
    }
    private void CardMouseUp(object sender, MouseButtonEventArgs e)
    {
        var selected = dragCard;
        holdTimer.Stop();
        dragSource?.ReleaseMouseCapture();
        dragSource = null; dragCard = null;
        RenderCards();
        if (didDrag || selected?.Entry is not { } entry || client == null) return;
        var api = client; var ct = session.Token;
        var dialog = new EntryPreviewWindow(this, entry, api, ct,
            () => !ct.IsCancellationRequested && long.Parse(entry.Id) > clearedThrough);
        if (dialog.ShowDialog() == true)
        {
            Status("已复制");
            if (preferences.NotifyClipboardOverwrite) Notify("系统剪贴板已更新");
        }
    }
    private void UserDragOver(object sender, System.Windows.DragEventArgs e)
    {
        e.Effects = connected && e.Data.GetDataPresent("PasteAnywhere.Entry") ? System.Windows.DragDropEffects.Copy : System.Windows.DragDropEffects.None;
        if (sender is Border border && e.Effects == System.Windows.DragDropEffects.Copy) HighlightUser(border);
        e.Handled = true;
    }
    private void UserDragLeave(object sender, System.Windows.DragEventArgs e)
    {
        if (sender is Border border && !new Rect(border.RenderSize).Contains(e.GetPosition(border))) HighlightUser(null);
    }
    private async void SendDrop(object sender, System.Windows.DragEventArgs e)
    {
        HighlightUser(null);
        if (!connected || client == null || e.Data.GetData("PasteAnywhere.Entry") is not string id ||
            (sender as FrameworkElement)?.DataContext is not UserCard card) return;
        e.Handled = true;
        var api = client; var ct = session.Token;
        try
        {
            var sent = await api.Send(id, card.User.Id, ct);
            if (ct.IsCancellationRequested) return;
            if (card.IsSelf) { newest = Math.Max(newest, long.Parse(sent.Id)); MergeEntry(sent); }
            Status("已发送给 " + card.Name); _ = RefreshDashboard();
        }
        catch (Exception) { if (!ct.IsCancellationRequested) Status("发送失败，请重试"); }
    }
    private void ScrollUsers(object sender, MouseWheelEventArgs e)
    {
        if (sender is ScrollViewer scroll) { scroll.ScrollToHorizontalOffset(scroll.HorizontalOffset - e.Delta); e.Handled = true; }
    }
    private async void LogoutClick(object sender, RoutedEventArgs e)
    {
        if (loggingOut || clearing || capturing || confirming || client == null || settings == null) return;
        if (!ConfirmOperation("确定登出当前设备？")) return;
        if (client == null || settings == null) return;
        loggingOut = true; LogoutButton.IsEnabled = false; UpdateClearButton();
        var api = client;
        try
        {
            await api.Logout($"{settings.Connection.User.Name}-{settings.Connection.Device.Name}", session.Token);
            CredentialStore.Clear();
            session.Cancel(); api.Dispose(); client = null; settings = null;
            ++dashboardRevision; entries = []; users = []; newest = 0; SetConnection(false); RenderCards();
            AccountText.Text = ""; ServerAddress.Clear(); DeviceName.Clear(); Invite.Clear(); Status("");
            MainView.Visibility = Visibility.Collapsed; LoginView.Visibility = Visibility.Visible;
        }
        catch (Exception) { Status("登出失败，请检查连接后重试"); }
        finally { loggingOut = false; LogoutButton.IsEnabled = true; UpdateClearButton(); }
    }
    private void OnClosing(object? sender, CancelEventArgs e)
    {
        if (!exiting) { e.Cancel = true; Hide(); return; }
        dashboardTimer.Stop(); holdTimer.Stop(); dragTimer.Stop(); toastTimer.Stop();
        session.Cancel(); session.Dispose(); client?.Dispose(); shortcut?.Dispose(); tray?.ContextMenuStrip?.Dispose(); tray?.Dispose(); trayIcon?.Dispose();
    }
}
