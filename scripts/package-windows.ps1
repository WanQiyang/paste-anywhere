param(
    [ValidateSet('win-x64', 'win-arm64')][string]$Runtime = 'win-x64',
    [ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = (Get-Content "$PSScriptRoot/../VERSION" -Raw).Trim(),
    [switch]$FrameworkDependent
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'
$flavor = if ($FrameworkDependent) { 'framework-dependent' } else { 'standalone' }
$name = "PasteAnywhere-$Version-$Runtime-$flavor"
$folder = Join-Path $root "build/windows/publish/$name"
if (Test-Path $folder) { Remove-Item $folder -Recurse -Force }
New-Item -ItemType Directory -Force -Path $folder, $dist | Out-Null
Push-Location $root
try {
    $selfContained = if ($FrameworkDependent) { 'false' } else { 'true' }
    dotnet publish clients/windows/PasteAnywhere/PasteAnywhere.csproj -c Release -r $Runtime --self-contained $selfContained -p:Version=$Version -p:PublishTrimmed=false -p:DebugType=None -p:DebugSymbols=false -o $folder
    if ($LASTEXITCODE -ne 0) { throw 'Windows publish failed' }
    Copy-Item (Join-Path $root 'docs/windows-release.md') (Join-Path $folder 'README.md')
    $archive = Join-Path $dist "$name.zip"
    Compress-Archive -Path $folder -DestinationPath $archive -Force
    $hash = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $([IO.Path]::GetFileName($archive))" | Set-Content "$archive.sha256" -Encoding utf8NoBOM
    Write-Host "Windows package: $archive"
} finally {
    Pop-Location
}
