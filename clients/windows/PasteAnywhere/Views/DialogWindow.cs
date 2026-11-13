using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using Button = System.Windows.Controls.Button;
using Brushes = System.Windows.Media.Brushes;
using HorizontalAlignment = System.Windows.HorizontalAlignment;
using Orientation = System.Windows.Controls.Orientation;

namespace PasteAnywhere.Views;

public class DialogWindow : Window
{
    protected Border Body { get; } = new() { Padding = new Thickness(24) };

    public DialogWindow(MainWindow owner)
    {
        Owner = owner;
        Title = "Paste Anywhere";
        Width = Math.Min(460, SystemParameters.WorkArea.Width);
        SizeToContent = SizeToContent.Height;
        ResizeMode = ResizeMode.NoResize;
        WindowStyle = WindowStyle.SingleBorderWindow;
        Background = Brushes.White;
        Foreground = owner.Foreground;
        FontFamily = owner.FontFamily;
        FontSize = 14;
        ShowInTaskbar = false;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Content = Body;
        PreviewKeyDown += (_, e) => { if (e.Key == Key.Escape) { Close(); e.Handled = true; } };
    }
}

public sealed class ConfirmationWindow : DialogWindow
{
    public ConfirmationWindow(MainWindow owner, string message) : base(owner)
    {
        Width = Math.Min(390, SystemParameters.WorkArea.Width);
        var panel = new StackPanel();
        panel.Children.Add(new TextBlock
        {
            Text = message, FontSize = 15,
            TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 24)
        });
        var buttons = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
        var cancel = new Button
        {
            Content = "取消", IsCancel = true, Width = 88, Height = 34, Padding = new Thickness(12, 4, 12, 4),
            Margin = new Thickness(0, 0, 8, 0),
            Style = (Style)owner.FindResource(typeof(Button))
        };
        var confirm = new Button
        {
            Content = "确定", Width = 88, Height = 34, Padding = new Thickness(12, 4, 12, 4),
            Style = (Style)owner.FindResource("DangerButton")
        };
        confirm.Click += (_, _) => DialogResult = true;
        buttons.Children.Add(cancel); buttons.Children.Add(confirm);
        panel.Children.Add(buttons); Body.Child = panel;
        Loaded += (_, _) => cancel.Focus();
    }
}
