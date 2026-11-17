$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $files = @(git -c core.quotePath=false ls-files --cached --others --exclude-standard | Sort-Object -Unique)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate repository files' }
    $issues = [System.Collections.Generic.List[string]]::new()
    $sensitivePath = '(^|/)(secrets|data|dist|backups|\.hvigor|\.idea)/|build-profile\.local\.json5$|\.(p12|p7b|cer|pem|key|csr|pfx|jks|keystore|hap|db|db-wal|db-shm)$|(^|/)\.env($|\.(?!example$))'
    $sensitiveContent = '-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|"(?:storePassword|keyPassword|private_key|client_secret)"\s*:\s*"[^"\s]+|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|[A-Za-z]:[\\/]+Users[\\/]+'
    foreach ($file in $files) {
        if ($file -match $sensitivePath) { $issues.Add("Sensitive repository path: $file") }
        if (!(Test-Path -LiteralPath $file -PathType Leaf)) { continue }
        if ([IO.Path]::GetExtension($file) -in @('.png', '.ico')) { continue }
        $text = [IO.File]::ReadAllText((Join-Path $root $file))
        if ($text -match $sensitiveContent) { $issues.Add("Potential secret/personal path in $file (value redacted)") }
    }
    # Inspect the staged snapshot too: cleaning only the working tree is not enough.
    $stagedHits = @(git grep --cached -I -l -E '"(storePassword|keyPassword|private_key|client_secret)"[[:space:]]*:[[:space:]]*"[^"[:space:]]+|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----' -- 2>$null)
    if ($LASTEXITCODE -gt 1) { throw 'Cannot inspect Git index' }
    foreach ($file in $stagedHits) { $issues.Add("Potential secret in Git index: $file (value redacted)") }
    $profile = Get-Content clients/harmony/build-profile.json5 -Raw | ConvertFrom-Json
    if ($profile.app.signingConfigs.Count -ne 0 -or @($profile.app.products | Where-Object signingConfig).Count -ne 0) {
        $issues.Add('Public HarmonyOS profile must not contain signing configurations or references')
    }
    if ($issues.Count) { throw ($issues -join "`n") }
    Write-Host "Public repository checks passed ($($files.Count) files and Git index; pattern-based scan)."
} finally { Pop-Location }
