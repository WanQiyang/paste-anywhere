param(
    [ValidateSet('amd64', 'arm64')][string]$Architecture = 'amd64',
    [ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = (Get-Content "$PSScriptRoot/../VERSION" -Raw).Trim()
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$stage = Join-Path $root "build/.staging/server-$([guid]::NewGuid().ToString('N'))"
$binary = Join-Path $root "build/server/linux-$Architecture/paste-anywhere"
$dist = Join-Path $root 'dist'
$name = "paste-anywhere-$Version-linux-$Architecture"
$folder = Join-Path $stage $name
New-Item -ItemType Directory -Force -Path $folder, $dist | Out-Null
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
$oldToolchain = $env:GOTOOLCHAIN
Push-Location (Join-Path $root 'server')
try {
    $env:GOTOOLCHAIN = 'local'
    if ((go env GOVERSION) -ne 'go1.27.4') { throw 'Go 1.27.4 is required' }
    $env:GOOS = 'linux'; $env:GOARCH = $Architecture; $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags '-s -w' -o $binary ./cmd/paste-anywhere
    if ($LASTEXITCODE -ne 0) { throw 'Linux server build failed' }
    Copy-Item $binary (Join-Path $folder 'paste-anywhere')
    $deploy = Join-Path $folder 'deploy'
    New-Item -ItemType Directory -Path $deploy, (Join-Path $folder '.container-data') | Out-Null
    New-Item -ItemType File -Path (Join-Path $folder '.container-data/.keep') | Out-Null
    Copy-Item (Join-Path $root 'deploy/Dockerfile.binary') (Join-Path $deploy 'Dockerfile')
    foreach ($file in @('compose.yaml', 'compose.push.yaml', 'Caddyfile', '.env.example')) {
        Copy-Item (Join-Path $root "deploy/$file") $deploy
    }
    Copy-Item (Join-Path $root 'docs/deployment.md') (Join-Path $folder 'README.md')
    $archive = Join-Path $dist "$name.tar.gz"
    tar -czf $archive -C $stage $name
    if ($LASTEXITCODE -ne 0) { throw 'Server archive failed' }
    $hash = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $([IO.Path]::GetFileName($archive))" | Set-Content "$archive.sha256" -Encoding utf8NoBOM
    Write-Host "Linux server package: $archive"
} finally {
    Pop-Location
    $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO; $env:GOTOOLCHAIN = $oldToolchain
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
}
