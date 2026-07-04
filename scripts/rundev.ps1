# scripts/rundev.ps1
#
# Dev build helper for wor (Windows / PowerShell). Runs `go vet` (static
# check) then `go build` for whatever OS/arch the local Go toolchain
# targets by default, and writes the binary to dist/dev/wor.exe (or
# dist/dev/wor if run under PowerShell Core on a non-Windows OS).
#
# Usage:
#   ./scripts/rundev.ps1
#
# This is a dev-only convenience script; it does not cross-compile for
# other platforms (see README.md for the GOOS/GOARCH release matrix).

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir
Set-Location $RootDir

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "go is not installed or not on PATH."
    exit 1
}

$GOOS = (go env GOOS).Trim()
$GOARCH = (go env GOARCH).Trim()

$BinName = "wor"
if ($GOOS -eq "windows") {
    $BinName = "wor.exe"
}

$OutDir = Join-Path $RootDir "dist/dev"
$OutPath = Join-Path $OutDir $BinName

Write-Host "==> wor dev build"
Write-Host "    OS/Arch : $GOOS/$GOARCH"
Write-Host "    Output  : $OutPath"
Write-Host ""

Write-Host "==> go vet ./..."
go vet ./...
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host "==> go build ./cmd/wor"
go build -o $OutPath ./cmd/wor
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Host ""
Write-Host "[OK] Build complete: $OutPath"
