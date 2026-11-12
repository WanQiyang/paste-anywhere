# Render the shared HarmonyOS SVG with WPF; no external image tools required.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName PresentationCore, WindowsBase
[xml]$svg = Get-Content (Join-Path $PSScriptRoot '../clients/harmony/AppScope/resources/base/media/app_icon.svg') -Raw
$output = Join-Path $PSScriptRoot '../clients/windows/PasteAnywhere/Assets'
[void][IO.Directory]::CreateDirectory($output)
$frames = @()
foreach ($size in @(16, 24, 32, 48, 64, 128, 256)) {
    $visual = [Windows.Media.DrawingVisual]::new()
    $drawing = $visual.RenderOpen()
    $drawing.PushTransform([Windows.Media.ScaleTransform]::new($size / 128.0, $size / 128.0))
    foreach ($node in $svg.DocumentElement.ChildNodes) {
        if ($node.LocalName -eq 'rect') {
            $brush = [Windows.Media.BrushConverter]::new().ConvertFromString($node.fill)
            $rect = [Windows.Rect]::new([double]$node.GetAttribute('x'), [double]$node.GetAttribute('y'), [double]$node.width, [double]$node.height)
            $drawing.DrawRoundedRectangle($brush, $null, $rect, [double]$node.rx, [double]$node.rx)
        } elseif ($node.LocalName -eq 'path') {
            $brush = [Windows.Media.BrushConverter]::new().ConvertFromString($node.stroke)
            $pen = [Windows.Media.Pen]::new($brush, [double]$node.GetAttribute('stroke-width'))
            $drawing.DrawGeometry($null, $pen, [Windows.Media.Geometry]::Parse($node.d))
        }
    }
    $drawing.Pop()
    $drawing.Close()
    $bitmap = [Windows.Media.Imaging.RenderTargetBitmap]::new($size, $size, 96, 96, [Windows.Media.PixelFormats]::Pbgra32)
    $bitmap.Render($visual)
    $encoder = [Windows.Media.Imaging.PngBitmapEncoder]::new()
    $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))
    $stream = [IO.MemoryStream]::new()
    $encoder.Save($stream)
    $frames += ,$stream.ToArray()
    $stream.Dispose()
    if ($size -eq 256) { [IO.File]::WriteAllBytes((Join-Path $output 'app-icon.png'), $frames[-1]) }
}
$file = [IO.File]::Create((Join-Path $output 'app.ico'))
$writer = [IO.BinaryWriter]::new($file)
try {
    $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]$frames.Count)
    $offset = 6 + 16 * $frames.Count
    $sizes = @(16, 24, 32, 48, 64, 128, 0)
    for ($i = 0; $i -lt $frames.Count; $i++) {
        $writer.Write([byte]$sizes[$i]); $writer.Write([byte]$sizes[$i])
        $writer.Write([byte]0); $writer.Write([byte]0)
        $writer.Write([uint16]1); $writer.Write([uint16]32)
        $writer.Write([uint32]$frames[$i].Length); $writer.Write([uint32]$offset)
        $offset += $frames[$i].Length
    }
    foreach ($frame in $frames) { $writer.Write([byte[]]$frame) }
} finally { $writer.Dispose() }
