using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using PasteAnywhere.Networking;
using PasteAnywhere.Platform;
using Button = System.Windows.Controls.Button;
using Image = System.Windows.Controls.Image;
using Brushes = System.Windows.Media.Brushes;
using HorizontalAlignment = System.Windows.HorizontalAlignment;
using Orientation = System.Windows.Controls.Orientation;

namespace PasteAnywhere.Views;

public sealed class EntryPreviewWindow : DialogWindow
{
    public EntryPreviewWindow(MainWindow owner, Entry entry, RemoteClient api, CancellationToken session,
        Func<bool> available) : base(owner)
    {
        MaxHeight = Math.Min(640, SystemParameters.WorkArea.Height);
        var lifetime = CancellationTokenSource.CreateLinkedTokenSource(session);
        Closed += (_, _) => { lifetime.Cancel(); lifetime.Dispose(); };
        var ct = lifetime.Token;
        var panel = new StackPanel();
        var height = Math.Max(60, MaxHeight - 260);
        var content = new Border { Background = Brushes.White, CornerRadius = new CornerRadius(12), Padding = new Thickness(14) };
        var message = new TextBlock { Text = "正在加载图片…", TextWrapping = TextWrapping.Wrap, FontSize = 13 };
        var error = new TextBlock { Foreground = Brushes.IndianRed, TextWrapping = TextWrapping.Wrap,
            Margin = new Thickness(0, 12, 0, 0) };
        var buttons = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right,
            Margin = new Thickness(0, 20, 0, 0) };
        var retry = new Button { Content = "重试", Style = (Style)owner.FindResource(typeof(Button)), Visibility = Visibility.Collapsed,
            Margin = new Thickness(0, 0, 8, 0) };
        var close = new Button { Content = "关闭", IsCancel = true, Style = (Style)owner.FindResource(typeof(Button)),
            Margin = new Thickness(0, 0, 8, 0) };
        var copy = new Button { Content = "复制", IsEnabled = false, Style = (Style)owner.FindResource("LoginButton") };
        buttons.Children.Add(retry); buttons.Children.Add(close); buttons.Children.Add(copy);
        panel.Children.Add(content); panel.Children.Add(error); panel.Children.Add(buttons); Body.Child = panel;
        ClipboardContent? prepared = null;
        System.Windows.Media.Imaging.BitmapSource? decoded = null;
        var attempt = 0;
        async Task Load(bool reload = false)
        {
            var currentAttempt = ++attempt;
            retry.Visibility = Visibility.Collapsed; error.Text = ""; copy.IsEnabled = false;
            prepared = null; decoded = null;
            message.Text = "正在加载图片…";
            content.Child = message;
            var progress = new Progress<(long Received, long Total)>(value =>
            {
                if (!ct.IsCancellationRequested && currentAttempt == attempt && content.Child == message)
                    message.Text = $"正在加载图片…\n{value.Received / 1024} / {(value.Total + 1023) / 1024} KiB";
            });
            try
            {
                prepared = entry.Kind == "image"
                    ? new ClipboardContent(null, await api.DownloadImage(entry, ct, progress, reload)) : new ClipboardContent(entry.Text, null);
                if (ct.IsCancellationRequested) return;
                if (!available()) throw new InvalidOperationException();
                if (prepared.Image is { } bytes)
                {
                    var bitmap = await Task.Run(() => ClipboardContent.Decode(bytes), ct);
                    if (ct.IsCancellationRequested) return;
                    if (!available()) throw new InvalidOperationException();
                    decoded = bitmap;
                    content.Child = new Image { Source = bitmap, Height = height, Stretch = Stretch.Uniform,
                        StretchDirection = StretchDirection.DownOnly };
                }
                else
                    content.Child = new ScrollViewer { MaxHeight = height, VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
                        HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
                        Content = new TextBlock { Text = entry.Text, TextWrapping = TextWrapping.Wrap, FontSize = 15, LineHeight = 23 } };
                copy.IsEnabled = true;
            }
            catch (OperationCanceledException) { }
            catch (Exception ex)
            {
                if (ct.IsCancellationRequested) return;
                message.Text = "暂时无法预览";
                error.Text = ex is System.IO.IOException ? ex.Message : "内容加载失败，请重试";
                retry.Visibility = Visibility.Visible;
            }
            finally { if (currentAttempt == attempt) ++attempt; }
        }
        Loaded += async (_, _) => await Load();
        retry.Click += async (_, _) => await Load(reload: true);
        copy.Click += (_, _) =>
        {
            try
            {
                if (ct.IsCancellationRequested || !available() || prepared == null) throw new InvalidOperationException();
                prepared.Write(decoded); DialogResult = true;
            }
            catch (Exception) { error.Text = "复制失败，内容可能已清空，请重试"; }
        };
    }
}
