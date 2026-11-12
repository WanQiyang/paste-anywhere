param([switch]$IncludeOutputs)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
# Only repository-owned generated directories; do not remove runtime data,
# local signing configuration, IDE settings, or machine-wide SDK caches.
$paths = @(
    'build/intermediate',
    'build/.staging',
    'clients/windows/PasteAnywhere/bin',
    'clients/windows/PasteAnywhere/obj',
    'clients/harmony/entry/build',
    'clients/harmony/build',
    'clients/harmony/.hvigor'
)
if ($IncludeOutputs) { $paths += @('build', 'dist') }
foreach ($relative in $paths) {
    $path = Join-Path $root $relative
    if (Test-Path $path) {
        Remove-Item $path -Recurse -Force
        Write-Host "Removed: $relative"
    }
}
