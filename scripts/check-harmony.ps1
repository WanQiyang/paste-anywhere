param([string]$DevEcoHome = $env:DEVECO_HOME)
$ErrorActionPreference = 'Stop'
if (!$DevEcoHome) { $DevEcoHome = Join-Path $env:ProgramFiles 'Huawei/DevEco Studio' }
foreach ($test in @('download', 'session')) {
    & (Join-Path $DevEcoHome 'tools/node/node.exe') (Join-Path $PSScriptRoot "../tests/harmony/$test.cjs") $DevEcoHome
    if ($LASTEXITCODE -ne 0) { throw "HarmonyOS $test checks failed" }
}
