param(
    [string]$DevEcoHome = $env:DEVECO_HOME,
    [ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = (Get-Content "$PSScriptRoot/../VERSION" -Raw).Trim()
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $versions = @(
        (Get-Content VERSION -Raw).Trim()
        (Get-Content clients/harmony/AppScope/app.json5 -Raw | ConvertFrom-Json).app.versionName
        (Get-Content clients/harmony/oh-package.json5 -Raw | ConvertFrom-Json).version
        (Get-Content clients/harmony/entry/oh-package.json5 -Raw | ConvertFrom-Json).version
    )
    if (@($versions | Where-Object { $_ -ne $Version }).Count -gt 0) {
        throw 'VERSION and all HarmonyOS version names must match before release'
    }
    & "$PSScriptRoot/clean-build.ps1" -IncludeOutputs
    & "$PSScriptRoot/check-server.ps1"
    dotnet build clients/windows/PasteAnywhere/PasteAnywhere.csproj -c Release -p:Version=$Version -p:CopyRetryCount=0
    if ($LASTEXITCODE -ne 0) { throw 'Windows client build failed' }
    & "$PSScriptRoot/package-server.ps1" -Version $Version
    & "$PSScriptRoot/package-windows.ps1" -Version $Version -FrameworkDependent
    & "$PSScriptRoot/package-harmony.ps1" -DevEcoHome $DevEcoHome -BuildMode release
    foreach ($checksum in Get-ChildItem dist -Filter '*.sha256') {
        $artifact = $checksum.FullName -replace '\.sha256$', ''
        $expected = (Get-Content $checksum.FullName -Raw).Split(' ')[0]
        if ((Get-FileHash $artifact -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
            throw "Checksum mismatch: $artifact"
        }
    }
    Write-Host 'All builds and release packages completed. Checksums verified.'
} finally {
    & "$PSScriptRoot/clean-build.ps1"
    Pop-Location
}
