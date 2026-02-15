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
$openExternalTerminal = if ($env:SMARTSH_OPEN_EXTERNAL_TERMINAL) {
    $env:SMARTSH_OPEN_EXTERNAL_TERMINAL -match '^(?i:1|true|yes|on)$'
} else {
    $false
}
$allowlistMode = if ($env:SMARTSH_ALLOWLIST_MODE) { $env:SMARTSH_ALLOWLIST_MODE } else { "warn" }
$requireApproval = if ($env:SMARTSH_REQUIRE_APPROVAL) {
    $env:SMARTSH_REQUIRE_APPROVAL -match '^(?i:1|true|yes|on)$'
} else {
    $true
}
$riskyDryRunFirst = if ($env:SMARTSH_RISKY_DRY_RUN_FIRST) {
    $env:SMARTSH_RISKY_DRY_RUN_FIRST -match '^(?i:1|true|yes|on)$'
} else {
    $true
}
$terminalApp = if ($env:SMARTSH_TERMINAL_APP) { $env:SMARTSH_TERMINAL_APP } else { "terminal" }
$terminalSessionKey = if ($env:SMARTSH_TERMINAL_SESSION_KEY) { $env:SMARTSH_TERMINAL_SESSION_KEY } else { "cursor-main" }
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
        if ($response.status -in @("completed", "failed", "blocked", "needs_approval")) {
            return $response
        }
        Start-Sleep -Milliseconds $pollIntervalMs
        $response = Invoke-RestMethod -Uri "$DaemonUrl/jobs/$($response.job_id)" -Method Get -Headers $daemonHeaders
    }
    return $response
}

function Test-RiskyCommand {
    param(
        [string]$Command
    )
    $normalizedCommand = $Command.ToLowerInvariant()
    foreach ($token in @("delete", "remove", "wipe", "reset", "prune", "drop", "destroy")) {
        if ($normalizedCommand.Contains($token)) {
            return $true
        }
    }
    return $false
}

$daemonUrl = if ($env:SMARTSH_DAEMON_URL) { $env:SMARTSH_DAEMON_URL } else { "http://127.0.0.1:8787" }
Ensure-Daemon -DaemonUrl $daemonUrl

if ($args.Count -gt 0) {
    $payload = @{
        command = ($args -join " ")
        cwd = (Get-Location).Path
        async = $asyncEnabled
        open_external_terminal = $openExternalTerminal
        allowlist_mode = $allowlistMode
        require_approval = $requireApproval
        terminal_session_key = $terminalSessionKey
    } | ConvertTo-Json -Compress
    if ($terminalApp) {
        $payloadObject = $payload | ConvertFrom-Json
        $payloadObject | Add-Member -NotePropertyName terminal_app -NotePropertyValue $terminalApp -Force
        $payload = $payloadObject | ConvertTo-Json -Compress
    }
    if ($timeoutSec -gt 0) {
        $payloadObject = $payload | ConvertFrom-Json
        $payloadObject | Add-Member -NotePropertyName timeout_sec -NotePropertyValue $timeoutSec -Force
        $payload = $payloadObject | ConvertTo-Json -Compress
    }
    if ($riskyDryRunFirst) {
        $commandText = ($args -join " ")
        if (Test-RiskyCommand -Command $commandText) {
            $payloadObject = $payload | ConvertFrom-Json
            $payloadObject | Add-Member -NotePropertyName dry_run -NotePropertyValue $true -Force
            $payload = $payloadObject | ConvertTo-Json -Compress
        }
    }
    $response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $payload
    $response | ConvertTo-Json -Depth 10 -Compress
    exit 0
}

if ($Host.Name -match "ConsoleHost" -and [Console]::IsInputRedirected -eq $false) {
    Write-Error "Usage: cursor-smartsh.ps1 <command> OR pipe JSON/plain command to stdin"
    exit 2
}

$stdinPayload = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($stdinPayload)) {
    Write-Output '{"executed":false,"exit_code":1,"error":"empty input"}'
    exit 1
}

$trimmedPayload = $stdinPayload.Trim()
if ($trimmedPayload.StartsWith("{")) {
    $payloadObject = $stdinPayload | ConvertFrom-Json
    if (-not $payloadObject.cwd) {
        $payloadObject | Add-Member -NotePropertyName cwd -NotePropertyValue (Get-Location).Path -Force
    }
    if ($null -eq $payloadObject.async) {
        $payloadObject | Add-Member -NotePropertyName async -NotePropertyValue $asyncEnabled -Force
    }
    if ($null -eq $payloadObject.open_external_terminal) {
        $payloadObject | Add-Member -NotePropertyName open_external_terminal -NotePropertyValue $openExternalTerminal -Force
    }
    if (-not $payloadObject.allowlist_mode) {
        $payloadObject | Add-Member -NotePropertyName allowlist_mode -NotePropertyValue $allowlistMode -Force
    }
    if ($null -eq $payloadObject.require_approval) {
        $payloadObject | Add-Member -NotePropertyName require_approval -NotePropertyValue $requireApproval -Force
    }
    if (-not $payloadObject.terminal_session_key) {
        $payloadObject | Add-Member -NotePropertyName terminal_session_key -NotePropertyValue $terminalSessionKey -Force
    }
    if ($terminalApp -and -not $payloadObject.terminal_app) {
        $payloadObject | Add-Member -NotePropertyName terminal_app -NotePropertyValue $terminalApp -Force
    }
    if ($timeoutSec -gt 0 -and $null -eq $payloadObject.timeout_sec) {
        $payloadObject | Add-Member -NotePropertyName timeout_sec -NotePropertyValue $timeoutSec -Force
    }
    if ($null -eq $payloadObject.dry_run -and $riskyDryRunFirst -and $payloadObject.command) {
        if (Test-RiskyCommand -Command ([string]$payloadObject.command)) {
            $payloadObject | Add-Member -NotePropertyName dry_run -NotePropertyValue $true -Force
        }
    }
    $normalizedPayload = $payloadObject | ConvertTo-Json -Depth 20 -Compress
    $response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $normalizedPayload
    $response | ConvertTo-Json -Depth 10 -Compress
    exit 0
}

$payload = @{
    command = $trimmedPayload
    cwd = (Get-Location).Path
    async = $asyncEnabled
    open_external_terminal = $openExternalTerminal
    allowlist_mode = $allowlistMode
    require_approval = $requireApproval
    terminal_session_key = $terminalSessionKey
} | ConvertTo-Json -Compress
if ($terminalApp) {
    $payloadObject = $payload | ConvertFrom-Json
    $payloadObject | Add-Member -NotePropertyName terminal_app -NotePropertyValue $terminalApp -Force
    $payload = $payloadObject | ConvertTo-Json -Compress
}
if ($timeoutSec -gt 0) {
    $payloadObject = $payload | ConvertFrom-Json
    $payloadObject | Add-Member -NotePropertyName timeout_sec -NotePropertyValue $timeoutSec -Force
    $payload = $payloadObject | ConvertTo-Json -Compress
}
if ($riskyDryRunFirst -and (Test-RiskyCommand -Command $trimmedPayload)) {
    $payloadObject = $payload | ConvertFrom-Json
    $payloadObject | Add-Member -NotePropertyName dry_run -NotePropertyValue $true -Force
    $payload = $payloadObject | ConvertTo-Json -Compress
}
$response = Invoke-RunAndPoll -DaemonUrl $daemonUrl -Payload $payload
$response | ConvertTo-Json -Depth 10 -Compress
exit 0
