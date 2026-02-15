$ErrorActionPreference = "Stop"

function Ensure-Daemon {
    param(
        [string]$DaemonUrl
    )

    $daemonToken = if ($env:SMARTSH_DAEMON_TOKEN) { $env:SMARTSH_DAEMON_TOKEN } else { "" }
    $healthHeaders = @{}
    if ($daemonToken) {
        $healthHeaders["X-Smartsh-Token"] = $daemonToken
    }
    try {
        Invoke-RestMethod -Uri "$DaemonUrl/health" -Method Get -Headers $healthHeaders -TimeoutSec 1 | Out-Null
        return
    } catch {
        # start daemon
    }

    if (Get-Command smartshd -ErrorAction SilentlyContinue) {
        Start-Process -FilePath "smartshd" -WindowStyle Hidden | Out-Null
    } else {
        $Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
        Start-Process -FilePath "go" -ArgumentList @("run", (Join-Path $Root "cmd/smartshd")) -WindowStyle Hidden | Out-Null
    }

    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Milliseconds 200
        try {
            Invoke-RestMethod -Uri "$DaemonUrl/health" -Method Get -Headers $healthHeaders -TimeoutSec 1 | Out-Null
            return
        } catch {
            continue
        }
    }

    throw "smartshd is not responding at $DaemonUrl"
}

$asyncEnabled = if ($env:SMARTSH_ASYNC) { $env:SMARTSH_ASYNC -eq "1" } else { $true }
$timeoutSec = 0
if ($env:SMARTSH_TIMEOUT_SEC -and $env:SMARTSH_TIMEOUT_SEC -match '^\d+$') {
    $timeoutSec = [int]$env:SMARTSH_TIMEOUT_SEC
} else {
    $timeoutSec = 180
}
$pollIntervalMs = 400
if ($env:SMARTSH_POLL_INTERVAL_MS -and $env:SMARTSH_POLL_INTERVAL_MS -match '^\d+$') {
    $pollIntervalMs = [int]$env:SMARTSH_POLL_INTERVAL_MS
}
$pollMaxAttempts = 300
if ($env:SMARTSH_POLL_MAX_ATTEMPTS -and $env:SMARTSH_POLL_MAX_ATTEMPTS -match '^\d+$') {
    $pollMaxAttempts = [int]$env:SMARTSH_POLL_MAX_ATTEMPTS
}
$daemonToken = if ($env:SMARTSH_DAEMON_TOKEN) { $env:SMARTSH_DAEMON_TOKEN } else { "" }
$daemonHeaders = @{}
if ($daemonToken) {
    $daemonHeaders["X-Smartsh-Token"] = $daemonToken
}

function Invoke-RunAndPoll {
    param(
        [string]$DaemonUrl,
        [string]$Payload
    )

    $response = Invoke-RestMethod -Uri "$DaemonUrl/run" -Method Post -Headers $daemonHeaders -ContentType "application/json" -Body $Payload
    if (-not $asyncEnabled) {
        return $response
    }

    if (-not $response.job_id) {
        return $response
    }

    for ($attempt = 0; $attempt -lt $pollMaxAttempts; $attempt++) {
        if ($response.status -in @("completed", "failed", "blocked")) {
            return $response
        }
        Start-Sleep -Milliseconds $pollIntervalMs
        $response = Invoke-RestMethod -Uri "$DaemonUrl/jobs/$($response.job_id)" -Method Get -Headers $daemonHeaders
    }
    return $response
}

$daemonUrl = if ($env:SMARTSH_DAEMON_URL) { $env:SMARTSH_DAEMON_URL } else { "http://127.0.0.1:8787" }
Ensure-Daemon -DaemonUrl $daemonUrl

if ($args.Count -gt 0) {
    $payload = @{
        command = ($args -join " ")
        cwd = (Get-Location).Path
        async = $asyncEnabled
    } | ConvertTo-Json -Compress
    if ($timeoutSec -gt 0) {
        $payloadObject = $payload | ConvertFrom-Json
        $payloadObject | Add-Member -NotePropertyName timeout_sec -NotePropertyValue $timeoutSec -Force
        $payload = $payloadObject | ConvertTo-Json -Compress
    }
    $response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $payload
    $response | ConvertTo-Json -Depth 10 -Compress
    exit 0
}

if ($Host.Name -match "ConsoleHost" -and [Console]::IsInputRedirected -eq $false) {
    Write-Error "Usage: claude-smartsh.ps1 <command> OR pipe JSON/plain command to stdin"
    exit 2
}

$stdinPayload = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($stdinPayload)) {
    Write-Output '{"executed":false,"exit_code":1,"error":"empty input"}'
    exit 1
}

$trimmedPayload = $stdinPayload.Trim()
if ($trimmedPayload.StartsWith("{")) {
    $response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $stdinPayload
    $response | ConvertTo-Json -Depth 10 -Compress
    exit 0
}

$payload = @{
    command = $trimmedPayload
    cwd = (Get-Location).Path
    async = $asyncEnabled
} | ConvertTo-Json -Compress
if ($timeoutSec -gt 0) {
    $payloadObject = $payload | ConvertFrom-Json
    $payloadObject | Add-Member -NotePropertyName timeout_sec -NotePropertyValue $timeoutSec -Force
    $payload = $payloadObject | ConvertTo-Json -Compress
}
$response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $payload
$response | ConvertTo-Json -Depth 10 -Compress
exit 0
