$ErrorActionPreference = "Stop"

$Repo = if ($env:SMARTSH_REPO) { $env:SMARTSH_REPO } else { "BegaDeveloper/smartsh" }
$Version = if ($env:SMARTSH_VERSION) { $env:SMARTSH_VERSION } else { "latest" }
$InstallDir = if ($env:SMARTSH_INSTALL_DIR) { $env:SMARTSH_INSTALL_DIR } else { "$env:USERPROFILE\AppData\Local\Programs\smartsh" }

$Components = if ($env:SMARTSH_COMPONENTS) { $env:SMARTSH_COMPONENTS } else { "smartsh smartshd" }

$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$Checksums = "checksums.txt"

$BaseUrl = if ($Version -eq "latest") {
    "https://github.com/$Repo/releases/latest/download"
} else {
    "https://github.com/$Repo/releases/download/$Version"
}
$ChecksumsUrl = "$BaseUrl/$Checksums"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

$TempDir = Join-Path $env:TEMP ("smartsh-install-" + [Guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $TempDir | Out-Null

try {
    # Support both underscore and hyphen artifact naming styles.
    $AssetCandidates = @(
        "smartsh_windows_$Arch.zip",
        "smartsh-windows-$Arch.zip"
    )
    $Asset = $null
    foreach ($Candidate in $AssetCandidates) {
        $ProbeUrl = "$BaseUrl/$Candidate"
        try {
            Invoke-WebRequest -Method Head -Uri $ProbeUrl | Out-Null
            $Asset = $Candidate
            break
        } catch {
            # Try next candidate.
        }
    }
    if (-not $Asset) {
        throw "No compatible release asset found for windows/$Arch"
    }

    $ZipPath = Join-Path $TempDir $Asset
    $ChecksumsPath = Join-Path $TempDir $Checksums
    $ZipUrl = "$BaseUrl/$Asset"

    Write-Host "Downloading $ZipUrl"
    Invoke-WebRequest -Uri $ZipUrl -OutFile $ZipPath

    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $ChecksumsPath

    $ExpectedLine = (Get-Content $ChecksumsPath | Select-String -SimpleMatch ("  " + $Asset) | Select-Object -First 1).Line
    if (-not $ExpectedLine) {
        $AltAsset = if ($Asset -like "*_*") { $Asset -replace "_", "-" } else { $Asset -replace "-", "_" }
        $ExpectedLine = (Get-Content $ChecksumsPath | Select-String -SimpleMatch ("  " + $AltAsset) | Select-Object -First 1).Line
    }
    if (-not $ExpectedLine) {
        throw "Checksum entry not found for $Asset in $Checksums"
    }
    $ExpectedHash = ($ExpectedLine -split "\s+")[0].ToLowerInvariant()
    $ActualHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ExpectedHash -ne $ActualHash) {
        throw "Checksum mismatch for $Asset"
    }

    $ExtractDir = Join-Path $TempDir "extract"
    Expand-Archive -Path $ZipPath -DestinationPath $ExtractDir -Force

    $ComponentList = $Components -split "[, ]+" | Where-Object { $_ -and $_.Trim().Length -gt 0 }
    foreach ($Component in $ComponentList) {
        $Source = Join-Path $ExtractDir ($Component + ".exe")
        if (-not (Test-Path $Source)) {
            throw "Component not found in archive: $Component"
        }
        $Target = Join-Path $InstallDir ($Component + ".exe")
        Write-Host "Installing $Component to $Target"
        Copy-Item -Force -Path $Source -Destination $Target
    }
} finally {
    if (Test-Path $TempDir) {
        Remove-Item -Recurse -Force $TempDir
    }
}

Write-Host "Installed smartsh components to $InstallDir"

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $UserPath) { $UserPath = "" }
if ($UserPath -notlike "*$InstallDir*") {
    $NewUserPath = ($UserPath.TrimEnd(";") + ";" + $InstallDir).TrimStart(";")
    [Environment]::SetEnvironmentVariable("Path", $NewUserPath, "User")
}
if ($env:Path -notlike "*$InstallDir*") {
    $env:Path = ($env:Path.TrimEnd(";") + ";" + $InstallDir)
}

Write-Host "Added $InstallDir to PATH (user + current session)."
$SmartshBin = Join-Path $InstallDir "smartsh.exe"
if (Test-Path $SmartshBin) {
    Write-Host "Running one-time setup: smartsh setup-agent"
    try {
        & $SmartshBin setup-agent
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "setup-agent failed during installer run (exit code: $LASTEXITCODE)."
            Write-Host "You can retry later with: smartsh setup-agent"
        } else {
            Write-Host "setup-agent completed."
        }
    } catch {
        Write-Warning "setup-agent failed during installer run: $($_.Exception.Message)"
        Write-Host "You can retry later with: smartsh setup-agent"
    }
}
