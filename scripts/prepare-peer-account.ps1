$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$exe = Join-Path $root 'build/server/windows-amd64/paste-anywhere.exe'
$db = Join-Path $root 'data/local/dev.db'
$path = Join-Path $root 'data/local/peer-account.json'
if (!(Test-Path $exe) -or !(Test-Path $db)) { throw 'Start the local development server first' }
if (!(Test-Path $path)) {
    $account = & $exe user create peer --db $db
    if ($LASTEXITCODE -ne 0) { throw 'Could not create peer account. Check user list before retrying.' }
    $account | Set-Content $path -Encoding utf8
}
$peer = Get-Content $path -Raw | ConvertFrom-Json
if ($peer.invite.Length -ne 8 -or $peer.user.name -notmatch '^[A-Za-z0-9]+$') {
    throw 'Invalid peer account configuration: an alphanumeric username and an 8-character pairing code are required.'
}
Write-Host "Peer account: $($peer.user.name)"
Write-Host "Invite saved in: $path"
Write-Host 'Keep the phone on ribo; connect Windows with this invitation to test cross-user dragging.'
