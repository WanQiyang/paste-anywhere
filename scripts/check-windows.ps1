$ErrorActionPreference = 'Stop'
$project = Join-Path $PSScriptRoot '../clients/windows/PasteAnywhere.Checks/PasteAnywhere.Checks.csproj'
dotnet run --project $project -c Release
if ($LASTEXITCODE -ne 0) { throw 'Windows checks failed' }
