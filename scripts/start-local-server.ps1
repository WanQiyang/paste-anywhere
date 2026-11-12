param([ValidateRange(1, 65535)][int]$Port = 18080)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$exe = Join-Path $root 'build/server/windows-amd64/paste-anywhere.exe'
$db = Join-Path $root 'data/local/dev.db'
$accountFile = Join-Path $root 'data/local/dev-account.json'
if (!(Test-Path $exe)) { throw 'Run scripts/check-server.ps1 first' }
$listeners = @(Get-NetTCPConnection -State Listen -ErrorAction Stop | Where-Object {
    $_.LocalPort -eq $Port -and $_.LocalAddress -in @('127.0.0.1', '0.0.0.0', '::')
})
if ($listeners.Count -gt 0) {
    $ownerId = $listeners[0].OwningProcess
    $process = Get-CimInstance Win32_Process -Filter "ProcessId = $ownerId"
    $dbArgument = '(?:^|\s)-db\s+"?' + [regex]::Escape($db) + '"?(?:\s|$)'
    if ($process.ExecutablePath -eq $exe -and $process.CommandLine -match '\sserve\s' -and
        $process.CommandLine -match $dbArgument) {
        try {
            $health = Invoke-RestMethod "http://127.0.0.1:$Port/healthz" -TimeoutSec 3
        } catch {
            throw "Local server PID $ownerId is listening, but its health check failed: $($_.Exception.Message)"
        }
        if ($health.status -ne 'ok') { throw "Local server PID $ownerId returned an unexpected health response" }
        Write-Host "Local server already running: http://127.0.0.1:$Port (PID $ownerId)"
        Write-Host "Development invite: $accountFile (same invite for both devices)"
        Write-Host 'Ready to connect. To load a new build, stop the existing server and start again.'
        return
    }
    throw "Port $Port is occupied by another process (PID $ownerId). Choose -Port <free-port> or stop that process first."
}
if (!(Test-Path $accountFile)) {
    $account = & $exe user create ribo --db $db
    if ($LASTEXITCODE -ne 0) { throw 'Failed to create local development account' }
    $account | Set-Content -Path $accountFile -Encoding utf8
}
$savedAccount = Get-Content $accountFile -Raw | ConvertFrom-Json
if ($savedAccount.invite.Length -ne 8) {
    throw 'Invalid development account configuration: an 8-character pairing code is required.'
}
Write-Host "Local server: http://127.0.0.1:$Port"
Write-Host "Development invite: $accountFile (same invite for both devices)"
& $exe serve -db $db -listen "127.0.0.1:$Port"
if ($LASTEXITCODE -ne 0) { throw 'Server stopped with an error' }
