$ErrorActionPreference = 'Stop'
if ((go env GOVERSION) -ne 'go1.27.4') { throw 'Go 1.27.4 is required' }
$oldToolchain = $env:GOTOOLCHAIN
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
Push-Location (Join-Path $PSScriptRoot '../server')
try {
    $env:GOTOOLCHAIN = 'local'
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    go build -o ../build/server/windows-amd64/paste-anywhere.exe ./cmd/paste-anywhere
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
    $env:GOTOOLCHAIN = $oldToolchain
    $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO
}
