$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$OutDir = if ($env:SMARTSH_SETUP_OUT_DIR) { $env:SMARTSH_SETUP_OUT_DIR } else { Join-Path $env:USERPROFILE ".smartsh" }
$Model = if ($env:SMARTSH_MODEL) { $env:SMARTSH_MODEL } else { "llama3.1:8b" }
$DaemonUrl = if ($env:SMARTSH_DAEMON_URL) { $env:SMARTSH_DAEMON_URL } else { "http://127.0.0.1:8787" }

New-Item -ItemType Directory -Path $OutDir -Force | Out-Null

function Ensure-Ollama {
    if (-not (Get-Command ollama -ErrorAction SilentlyContinue)) {
        throw "ollama command not found. Install Ollama first."
    }
    try {
        Invoke-RestMethod -Uri "http://127.0.0.1:11434/api/tags" -Method Get -TimeoutSec 1 | Out-Null
    } catch {
        Start-Process -FilePath "ollama" -ArgumentList @("serve") -WindowStyle Hidden | Out-Null
        Start-Sleep -Seconds 1
    }
    try {
        & ollama pull $Model | Out-Null
    } catch {
        # best effort
    }
}

function Ensure-Daemon {
    try {
        Invoke-RestMethod -Uri "$DaemonUrl/health" -Method Get -TimeoutSec 1 | Out-Null
        return
    } catch {}

    if (Get-Command smartshd -ErrorAction SilentlyContinue) {
        Start-Process -FilePath "smartshd" -WindowStyle Hidden | Out-Null
    } else {
        Start-Process -FilePath "go" -ArgumentList @("run", (Join-Path $Root "cmd/smartshd")) -WindowStyle Hidden | Out-Null
    }

    for ($i = 0; $i -lt 30; $i++) {
        Start-Sleep -Milliseconds 200
        try {
            Invoke-RestMethod -Uri "$DaemonUrl/health" -Method Get -TimeoutSec 1 | Out-Null
            return
        } catch {}
    }
    throw "smartshd failed to start"
}

function Write-CursorTool {
    $cursorPath = Join-Path $OutDir "cursor-smartsh-tool.json"
    $commandPath = Join-Path $Root "scripts/integrations/cursor-smartsh.ps1"
    $json = @"
{
  "name": "smartsh-agent",
  "description": "Run terminal commands through smartshd and return compact summaries.",
  "command": "$commandPath",
  "args": [],
  "inputSchema": {
    "type": "object",
    "properties": {
      "instruction": { "type": "string" },
      "cwd": { "type": "string" },
      "dry_run": { "type": "boolean" },
      "unsafe": { "type": "boolean" },
      "async": { "type": "boolean" },
      "timeout_sec": { "type": "integer" },
      "allowlist_mode": { "type": "string", "enum": ["off", "warn", "enforce"] },
      "allowlist_file": { "type": "string" }
    },
    "required": ["instruction"]
  },
  "stdinTemplate": "{\"instruction\":\"{{instruction}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\"}"
}
"@
    Set-Content -Path $cursorPath -Value $json -Encoding UTF8
}

function Write-ClaudeTool {
    $claudePath = Join-Path $OutDir "claude-smartsh-tool.json"
    $commandPath = Join-Path $Root "scripts/integrations/claude-smartsh.ps1"
    $json = @"
{
  "tools": [
    {
      "name": "smartsh_agent",
      "description": "Execute instructions through smartshd and return compact summaries.",
      "command": "$commandPath",
      "args": [],
      "input_schema": {
        "type": "object",
        "properties": {
          "instruction": { "type": "string" },
          "cwd": { "type": "string" },
          "dry_run": { "type": "boolean" },
          "unsafe": { "type": "boolean" },
          "async": { "type": "boolean" },
          "timeout_sec": { "type": "integer" },
          "allowlist_mode": { "type": "string", "enum": ["off", "warn", "enforce"] },
          "allowlist_file": { "type": "string" }
        },
        "required": ["instruction"]
      },
      "stdin_template": "{\"instruction\":\"{{instruction}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\"}"
    }
  ]
}

function Write-CursorMCP {
    $mcpPath = Join-Path $OutDir "cursor-smartsh-mcp.json"
    $scriptPath = Join-Path $Root "scripts/integrations/smartsh-mcp.ps1"
    $json = @"
{
  "name": "smartsh",
  "command": "powershell",
  "args": ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "$scriptPath"],
  "env": {
    "SMARTSH_DAEMON_URL": "$DaemonUrl"
  }
}
"@
    Set-Content -Path $mcpPath -Value $json -Encoding UTF8
}

function Write-CursorMCPWorkspace {
    $mcpPath = Join-Path $OutDir "cursor-mcp.json"
    $scriptPath = Join-Path $Root "scripts/integrations/smartsh-mcp.ps1"
    $json = @"
{
  "mcpServers": {
    "smartsh": {
      "command": "powershell",
      "args": ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "$scriptPath"],
      "env": {
        "SMARTSH_DAEMON_URL": "$DaemonUrl"
      }
    }
  }
}
"@
    Set-Content -Path $mcpPath -Value $json -Encoding UTF8
}
"@
    Set-Content -Path $claudePath -Value $json -Encoding UTF8
}

function Write-AgentPrompt {
    $promptPath = Join-Path $OutDir "agent-instructions.txt"
    @"
For command execution, always use smartsh_run (or smartsh-agent/smartsh_agent if only custom tools are available).
Never use direct shell execution unless explicitly requested by the user.
Prefer summarized tool output and avoid dumping full terminal logs.
"@ | Set-Content -Path $promptPath -Encoding UTF8
}

Ensure-Ollama
Ensure-Daemon
Write-CursorTool
Write-CursorMCP
Write-CursorMCPWorkspace
Write-ClaudeTool
Write-AgentPrompt

Write-Host ""
Write-Host "smartsh quick setup complete."
Write-Host "Cursor tool file: $(Join-Path $OutDir 'cursor-smartsh-tool.json')"
Write-Host "Cursor MCP server file: $(Join-Path $OutDir 'cursor-smartsh-mcp.json')"
Write-Host "Cursor workspace mcp.json: $(Join-Path $OutDir 'cursor-mcp.json')"
Write-Host "Claude tool file: $(Join-Path $OutDir 'claude-smartsh-tool.json')"
Write-Host "Agent instruction snippet: $(Join-Path $OutDir 'agent-instructions.txt')"
Write-Host ""
Write-Host "Minimal next step:"
Write-Host "1) Import the generated tool JSON in Cursor/Claude."
Write-Host "2) Paste agent-instructions.txt into system instructions."
