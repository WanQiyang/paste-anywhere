param(
    [string]$DevEcoHome = $env:DEVECO_HOME,
    [ValidateSet('debug', 'release')][string]$BuildMode = 'debug'
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
Push-Location (Join-Path $PSScriptRoot '../clients/harmony')
try {
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
} finally {
    Pop-Location
    $env:Path = $oldPath
    $env:DEVECO_SDK_HOME = $oldSdk
    $env:JAVA_HOME = $oldJava
    $env:NODE_HOME = $oldNode
}
