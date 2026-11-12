param(
    [string]$DevEcoHome = $env:DEVECO_HOME,
    [ValidateSet('debug', 'release')][string]$BuildMode = 'debug'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
& (Join-Path $PSScriptRoot 'build-harmony.ps1') -DevEcoHome $DevEcoHome -BuildMode $BuildMode
$output = Join-Path $root 'clients/harmony/entry/build/default/outputs/default'
$signed = Join-Path $output 'entry-default-signed.hap'
$unsigned = Join-Path $output 'entry-default-unsigned.hap'
if (!(Test-Path $signed) -or !(Test-Path $unsigned)) { throw 'A signed HAP is required. Configure signing in DevEco Studio first.' }
if ((Get-Item $signed).LastWriteTimeUtc -lt (Get-Item $unsigned).LastWriteTimeUtc) {
    throw 'The signed HAP is older than the unsigned output. Check the signing configuration.'
}
$dist = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$destination = Join-Path $dist "PasteAnywhere-harmony-$BuildMode-signed.hap"
Copy-Item $signed $destination -Force
$hash = (Get-FileHash $destination -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  $([IO.Path]::GetFileName($destination))" | Set-Content "$destination.sha256" -Encoding utf8NoBOM
Write-Host "HarmonyOS package: $destination"
Write-Host 'Install eligibility depends on the certificate/profile and authorized devices used to sign this HAP.'
