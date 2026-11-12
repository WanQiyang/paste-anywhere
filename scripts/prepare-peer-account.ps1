$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$exe = Join-Path $root '.local/paste-anywhere.exe'
$db = Join-Path $root '.local/dev.db'
$path = Join-Path $root '.local/peer-account.json'
if (!(Test-Path $exe) -or !(Test-Path $db)) { throw 'Start the local development server first' }
if (!(Test-Path $path)) {
    $account = & $exe user-create -db $db -name '联调用户'
    if ($LASTEXITCODE -ne 0) { throw 'Could not create peer account. Check user-list before retrying.' }
    $account | Set-Content $path -Encoding utf8
}
$peer = Get-Content $path -Raw | ConvertFrom-Json
Write-Host "Peer account: $($peer.user.name) (ID $($peer.user.id))"
Write-Host "Invite saved in: $path"
Write-Host 'Keep the phone on ribo; connect Windows with this invitation to test cross-user dragging.'
