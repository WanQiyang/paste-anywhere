param(
    [string]$DevEcoHome = $env:DEVECO_HOME,
    [ValidateSet('debug', 'release')][string]$BuildMode = 'debug',
    [switch]$UseLocalSigning
)
$ErrorActionPreference = 'Stop'
if (!$DevEcoHome) { $DevEcoHome = Join-Path $env:ProgramFiles 'Huawei\DevEco Studio' }
$expected = @{
    'product-info.json' = '26.0.0.865'
    'tools/hvigor/hvigor/package.json' = '6.26.9'
    'tools/hvigor/hvigor-ohos-plugin/package.json' = '6.26.9'
    'tools/ohpm/package.json' = '26.0.0.630'
}
foreach ($entry in $expected.GetEnumerator()) {
    $actual = (Get-Content (Join-Path $DevEcoHome $entry.Key) -Raw | ConvertFrom-Json).version
    if ($actual -ne $entry.Value) { throw "Toolchain mismatch: $($entry.Key): expected $($entry.Value), got $actual" }
}
$sdk = Get-Content (Join-Path $DevEcoHome 'sdk/default/sdk-pkg.json') -Raw | ConvertFrom-Json
if ($sdk.data.version -ne '26.0.0.105') { throw 'Expected HarmonyOS SDK 26.0.0.105' }
$oldPath = $env:Path
$oldSdk = $env:DEVECO_SDK_HOME
$oldJava = $env:JAVA_HOME
$oldNode = $env:NODE_HOME
$root = Split-Path $PSScriptRoot -Parent
$profilePath = Join-Path $root 'clients/harmony/build-profile.json5'
$originalProfile = $null
$output = Join-Path $root "build/harmony/$BuildMode"
Push-Location (Join-Path $PSScriptRoot '../clients/harmony')
try {
    if ($UseLocalSigning) {
        $localProfile = Get-Content 'build-profile.local.json5' -Raw | ConvertFrom-Json
        if (!$localProfile.app.signingConfigs.Count) { throw 'Local HarmonyOS signing configuration is missing' }
        $originalProfile = [IO.File]::ReadAllBytes($profilePath)
        $profile = Get-Content $profilePath -Raw | ConvertFrom-Json
        $profile.app.signingConfigs = $localProfile.app.signingConfigs
        foreach ($product in $profile.app.products) {
            $localProduct = $localProfile.app.products | Where-Object { $_.name -eq $product.name }
            if (!$localProduct.signingConfig) { throw "No local signing configuration for product $($product.name)" }
            $product | Add-Member -NotePropertyName signingConfig -NotePropertyValue $localProduct.signingConfig -Force
        }
        $profile | ConvertTo-Json -Depth 30 | Set-Content $profilePath -Encoding utf8NoBOM
    }
    # Remove stale HAPs so an earlier signed build cannot be mistaken for this one.
    $nativeOutput = 'entry/build/default/outputs/default'
    if (Test-Path $nativeOutput) { Remove-Item $nativeOutput -Recurse -Force }
    if (Test-Path $output) { Remove-Item $output -Recurse -Force }
    $env:NODE_HOME = Join-Path $DevEcoHome 'tools/node'
    $env:Path = "$env:NODE_HOME;$(Join-Path $DevEcoHome 'tools/ohpm/bin');$(Join-Path $DevEcoHome 'jbr/bin');$oldPath"
    $env:DEVECO_SDK_HOME = Join-Path $DevEcoHome 'sdk'
    $env:JAVA_HOME = Join-Path $DevEcoHome 'jbr'
    $node = Join-Path $env:NODE_HOME 'node.exe'
    if ((& $node --version) -ne 'v24.14.1') { throw 'Expected bundled Node.js 24.14.1' }
    & $node (Join-Path $DevEcoHome 'tools/ohpm/bin/pm-cli.js') install
    if ($LASTEXITCODE -ne 0) { throw 'OHPM install failed' }
    & $node (Join-Path $DevEcoHome 'tools/hvigor/bin/hvigorw.js') --mode module -p product=default -p module=entry@default -p buildMode=$BuildMode assembleHap --no-daemon
    if ($LASTEXITCODE -ne 0) { throw 'HarmonyOS build failed' }
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    Copy-Item "$nativeOutput/*" $output -Recurse -Force
    if ($UseLocalSigning -and !(Test-Path (Join-Path $output 'entry-default-signed.hap'))) {
        throw 'Signed HAP was not generated'
    }
    Write-Host "HarmonyOS build output: $output"
} finally {
    if ($null -ne $originalProfile) { [IO.File]::WriteAllBytes($profilePath, $originalProfile) }
    Pop-Location
    $env:Path = $oldPath
    $env:DEVECO_SDK_HOME = $oldSdk
    $env:JAVA_HOME = $oldJava
    $env:NODE_HOME = $oldNode
}
