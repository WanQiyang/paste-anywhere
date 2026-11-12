using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Media.Effects;
using System.Windows.Media.Imaging;
using Point = System.Windows.Point;
using Size = System.Windows.Size;

namespace PasteAnywhere.Views;

// A non-interactive preview: the real user cards underneath remain drop targets.
internal sealed class CardDragAdorner : Adorner
{
    private readonly Border preview;
    private readonly Point anchor;
    private Point position;

    public CardDragAdorner(UIElement surface, FrameworkElement source, Point anchor) : base(surface)
    {
        IsHitTestVisible = false;
        this.anchor = anchor;
        var dpi = VisualTreeHelper.GetDpi(source);
        var bitmap = new RenderTargetBitmap(Math.Max(1, (int)Math.Ceiling(source.ActualWidth * dpi.DpiScaleX)),
            Math.Max(1, (int)Math.Ceiling(source.ActualHeight * dpi.DpiScaleY)), dpi.PixelsPerInchX, dpi.PixelsPerInchY, PixelFormats.Pbgra32);
        var drawing = new DrawingVisual();
        using (var context = drawing.RenderOpen())
            context.DrawRectangle(new VisualBrush(source), null, new Rect(0, 0, source.ActualWidth, source.ActualHeight));
        bitmap.Render(drawing);
        bitmap.Freeze();
        preview = new Border
        {
            Width = source.ActualWidth, Height = source.ActualHeight, CornerRadius = new CornerRadius(12),
            Child = new System.Windows.Controls.Image { Source = bitmap, Stretch = Stretch.Fill },
            RenderTransformOrigin = new Point(0.5, 0.5), RenderTransform = new ScaleTransform(1, 1),
            Effect = new DropShadowEffect { Color = System.Windows.Media.Color.FromRgb(39, 63, 106), BlurRadius = 26, ShadowDepth = 10, Opacity = 0.22 }
        };
        AddVisualChild(preview);
        var ease = new CubicEase { EasingMode = EasingMode.EaseOut };
        var lift = new DoubleAnimation(1, 1.025, TimeSpan.FromMilliseconds(160)) { EasingFunction = ease };
        ((ScaleTransform)preview.RenderTransform).BeginAnimation(ScaleTransform.ScaleXProperty, lift);
        ((ScaleTransform)preview.RenderTransform).BeginAnimation(ScaleTransform.ScaleYProperty, lift);
        preview.BeginAnimation(OpacityProperty, new DoubleAnimation(0.65, 0.96, TimeSpan.FromMilliseconds(160)));
    }

    public void Move(Point cursor) { position = cursor; InvalidateArrange(); }
    protected override int VisualChildrenCount => 1;
    protected override Visual GetVisualChild(int index) => preview;
    protected override Size MeasureOverride(Size constraint) { preview.Measure(new Size(preview.Width, preview.Height)); return AdornedElement.RenderSize; }
    protected override Size ArrangeOverride(Size finalSize)
    {
        preview.Arrange(new Rect(position.X - anchor.X, position.Y - anchor.Y - 6, preview.Width, preview.Height));
        return finalSize;
    }
}
