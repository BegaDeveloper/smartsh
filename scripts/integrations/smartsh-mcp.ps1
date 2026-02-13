$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$localBin = Join-Path $root "bin/smartsh"
$useInstalled = if ($env:SMARTSH_USE_INSTALLED_BIN) { $env:SMARTSH_USE_INSTALLED_BIN -eq "1" } else { $false }
$forceGoRun = if ($env:SMARTSH_FORCE_GO_RUN) { $env:SMARTSH_FORCE_GO_RUN -eq "1" } else { $false }

if (-not $forceGoRun -and (Test-Path $localBin)) {
    & $localBin mcp
    exit $LASTEXITCODE
}

if ($useInstalled -and (Get-Command smartsh -ErrorAction SilentlyContinue)) {
    & smartsh mcp
    exit $LASTEXITCODE
}

if ($forceGoRun -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Push-Location $root
    try {
        & go run ./cmd/smartsh mcp
        exit $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

if (Get-Command smartsh -ErrorAction SilentlyContinue) {
    & smartsh mcp
    exit $LASTEXITCODE
}

Write-Error "smartsh-mcp launcher failed: expected executable at $localBin (or set SMARTSH_USE_INSTALLED_BIN=1, or SMARTSH_FORCE_GO_RUN=1)"
exit 1
