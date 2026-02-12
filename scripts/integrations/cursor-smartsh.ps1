$ErrorActionPreference = "Stop"

if (Get-Command smartsh -ErrorAction SilentlyContinue) {
    smartsh --agent @args
    exit $LASTEXITCODE
}

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
go run (Join-Path $Root "cmd/smartsh") --agent @args
exit $LASTEXITCODE
