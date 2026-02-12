$ErrorActionPreference = "Stop"

$Repo = if ($env:SMARTSH_REPO) { $env:SMARTSH_REPO } else { "https://example.com/smartsh" }
$Version = if ($env:SMARTSH_VERSION) { $env:SMARTSH_VERSION } else { "latest" }
$InstallDir = if ($env:SMARTSH_INSTALL_DIR) { $env:SMARTSH_INSTALL_DIR } else { "$env:USERPROFILE\AppData\Local\Programs\smartsh" }

$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$Asset = "smartsh-windows-$Arch.exe"
$Url = "$Repo/releases/$Version/$Asset"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

$Target = Join-Path $InstallDir "smartsh.exe"

Write-Host "Downloading $Url"
Invoke-WebRequest -Uri $Url -OutFile $Target

Write-Host "Installed smartsh to $Target"
Write-Host "Add $InstallDir to your PATH if needed."
