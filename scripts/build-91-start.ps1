$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$backendDir = Join-Path $repoRoot "backend"
$output = Join-Path $repoRoot "61-start.exe"

Push-Location $backendDir
try {
    go build -o $output .\cmd\launcher
}
finally {
    Pop-Location
}

Write-Host "Built: $output"
