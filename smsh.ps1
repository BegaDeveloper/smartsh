$ErrorActionPreference = "Stop"

if (Get-Command smartsh -ErrorAction SilentlyContinue) {
    smartsh @args
    exit $LASTEXITCODE
}

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
go run (Join-Path $Root "cmd/smartsh") @args
exit $LASTEXITCODE
