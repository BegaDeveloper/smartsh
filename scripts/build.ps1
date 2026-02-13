$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$DistDir = Join-Path $RootDir "dist"
$BinDir = Join-Path $DistDir "bin"
$ReleaseDir = Join-Path $DistDir "release"

if (-not (Test-Path $DistDir)) {
    New-Item -ItemType Directory -Path $DistDir | Out-Null
}
if (-not (Test-Path $BinDir)) {
    New-Item -ItemType Directory -Path $BinDir | Out-Null
}
if (-not (Test-Path $ReleaseDir)) {
    New-Item -ItemType Directory -Path $ReleaseDir | Out-Null
}

$WindowsBinDir = Join-Path $BinDir "smartsh_windows_amd64"
$DarwinAmd64BinDir = Join-Path $BinDir "smartsh_darwin_amd64"
$DarwinArm64BinDir = Join-Path $BinDir "smartsh_darwin_arm64"
$LinuxAmd64BinDir = Join-Path $BinDir "smartsh_linux_amd64"
$LinuxArm64BinDir = Join-Path $BinDir "smartsh_linux_arm64"
foreach ($Directory in @($WindowsBinDir, $DarwinAmd64BinDir, $DarwinArm64BinDir, $LinuxAmd64BinDir, $LinuxArm64BinDir)) {
    if (-not (Test-Path $Directory)) {
        New-Item -ItemType Directory -Path $Directory | Out-Null
    }
}

Write-Host "Building Windows amd64..."
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o (Join-Path $WindowsBinDir "smartsh.exe") (Join-Path $RootDir "cmd/smartsh")
go build -o (Join-Path $WindowsBinDir "smartshd.exe") (Join-Path $RootDir "cmd/smartshd")

Write-Host "Building macOS amd64..."
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -o (Join-Path $DarwinAmd64BinDir "smartsh") (Join-Path $RootDir "cmd/smartsh")
go build -o (Join-Path $DarwinAmd64BinDir "smartshd") (Join-Path $RootDir "cmd/smartshd")

Write-Host "Building macOS arm64..."
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -o (Join-Path $DarwinArm64BinDir "smartsh") (Join-Path $RootDir "cmd/smartsh")
go build -o (Join-Path $DarwinArm64BinDir "smartshd") (Join-Path $RootDir "cmd/smartshd")

Write-Host "Building Linux amd64..."
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o (Join-Path $LinuxAmd64BinDir "smartsh") (Join-Path $RootDir "cmd/smartsh")
go build -o (Join-Path $LinuxAmd64BinDir "smartshd") (Join-Path $RootDir "cmd/smartshd")

Write-Host "Building Linux arm64..."
$env:GOOS = "linux"
$env:GOARCH = "arm64"
go build -o (Join-Path $LinuxArm64BinDir "smartsh") (Join-Path $RootDir "cmd/smartsh")
go build -o (Join-Path $LinuxArm64BinDir "smartshd") (Join-Path $RootDir "cmd/smartshd")

Write-Host "Packaging archives..."
$WindowsArchive = Join-Path $ReleaseDir "smartsh_windows_amd64.zip"
$DarwinAmd64Archive = Join-Path $ReleaseDir "smartsh_darwin_amd64.tar.gz"
$DarwinArm64Archive = Join-Path $ReleaseDir "smartsh_darwin_arm64.tar.gz"
$LinuxAmd64Archive = Join-Path $ReleaseDir "smartsh_linux_amd64.tar.gz"
$LinuxArm64Archive = Join-Path $ReleaseDir "smartsh_linux_arm64.tar.gz"

if (Test-Path $WindowsArchive) {
    Remove-Item $WindowsArchive
}
Compress-Archive -Path @((Join-Path $WindowsBinDir "smartsh.exe"), (Join-Path $WindowsBinDir "smartshd.exe")) -DestinationPath $WindowsArchive

tar -czf $DarwinAmd64Archive -C $DarwinAmd64BinDir smartsh smartshd
tar -czf $DarwinArm64Archive -C $DarwinArm64BinDir smartsh smartshd
tar -czf $LinuxAmd64Archive -C $LinuxAmd64BinDir smartsh smartshd
tar -czf $LinuxArm64Archive -C $LinuxArm64BinDir smartsh smartshd

Write-Host "Writing checksums..."
$ChecksumLines = @()
foreach ($Artifact in @($DarwinAmd64Archive, $DarwinArm64Archive, $LinuxAmd64Archive, $LinuxArm64Archive, $WindowsArchive)) {
    $FileHash = Get-FileHash -Path $Artifact -Algorithm SHA256
    $ChecksumLines += "$($FileHash.Hash.ToLowerInvariant())  $([System.IO.Path]::GetFileName($Artifact))"
}
Set-Content -Path (Join-Path $ReleaseDir "checksums.txt") -Value $ChecksumLines -Encoding ASCII

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue

Write-Host "Done. Release artifacts in $ReleaseDir"
