$ErrorActionPreference = 'Stop'
if ((go env GOVERSION) -ne 'go1.27.4') { throw 'Go 1.27.4 is required' }
$oldToolchain = $env:GOTOOLCHAIN
Push-Location (Join-Path $PSScriptRoot '../server')
try {
    $env:GOTOOLCHAIN = 'local'
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    go build -o ../.local/paste-anywhere.exe ./cmd/paste-anywhere
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
    $env:GOTOOLCHAIN = $oldToolchain
}
