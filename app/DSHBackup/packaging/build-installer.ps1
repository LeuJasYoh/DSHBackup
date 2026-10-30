#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Version,
    [string]$Source,
    [string]$Output,
    [string]$NsisPath
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = "Stop"
$packagingRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$appRoot = (Resolve-Path (Join-Path $packagingRoot "..")).Path
$repoRoot = (Resolve-Path (Join-Path $appRoot "..\..")).Path
$distRoot = Join-Path $repoRoot "dist"

if (-not $Version) {
    $wails = Get-Content (Join-Path $appRoot "wails.json") -Raw | ConvertFrom-Json
    $Version = [string]$wails.info.productVersion
}
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "版本号必须是 x.y.z：$Version"
}
if (-not $Source) { $Source = Join-Path $distRoot "DSHBackup" }
if (-not (Test-Path $Source)) { throw "找不到安装源目录：$Source" }
$Source = (Resolve-Path $Source).Path
if (-not $Output) { $Output = Join-Path $distRoot "DSHBackup-$Version-win64-setup.exe" }
$Output = [System.IO.Path]::GetFullPath($Output)
if (-not (Test-Path (Split-Path -Parent $Output))) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null
}

$required = @(
    "DSHBackup.exe",
    "dsh-backup-core.exe",
    "icon.ico",
    "README.txt",
    "THIRD_PARTY_NOTICES.txt",
    "7-Zip-SOURCE.txt",
    "BUILD.txt",
    "tools\7za.exe",
    "tools\License.txt"
)
foreach ($relative in $required) {
    if (-not (Test-Path (Join-Path $Source $relative))) {
        throw "安装源目录缺少文件：$relative"
    }
}
$buildInfo = Get-Content (Join-Path $Source "BUILD.txt") -Raw
if ($buildInfo -notmatch "DSH Backup $([regex]::Escape($Version))") {
    throw "安装源 BUILD.txt 不是 $Version，拒绝混装不同版本"
}

function Resolve-Makensis([string]$Explicit) {
    if ($Explicit) {
        if (Test-Path $Explicit) { return (Resolve-Path $Explicit).Path }
        throw "指定的 makensis.exe 不存在：$Explicit"
    }
    $command = Get-Command makensis.exe -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    $candidates = @()
    if (${env:ProgramFiles(x86)}) { $candidates += Join-Path ${env:ProgramFiles(x86)} "NSIS\makensis.exe" }
    if ($env:ProgramFiles) { $candidates += Join-Path $env:ProgramFiles "NSIS\makensis.exe" }
    foreach ($key in @(
        "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\NSIS",
        "HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\NSIS"
    )) {
        $entry = Get-ItemProperty $key -ErrorAction SilentlyContinue
        if ($entry -and $entry.InstallLocation) {
            $candidates += Join-Path $entry.InstallLocation "Bin\makensis.exe"
            $candidates += Join-Path $entry.InstallLocation "makensis.exe"
        }
    }
    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) { return $candidate }
    }
    throw "找不到 makensis.exe。请安装 NSIS 3.x，或用 -NsisPath 指定路径。"
}

$makensis = Resolve-Makensis $NsisPath
$script = Join-Path $packagingRoot "installer.nsi"
$icon = Join-Path $appRoot "build\windows\icon.ico"
if (-not (Test-Path $icon)) { throw "找不到安装包图标：$icon" }
if (Test-Path $Output) { Remove-Item -Force $Output }

Write-Host ""
Write-Host "==> 生成 NSIS 安装包" -ForegroundColor Cyan
Write-Host "makensis: $makensis"
Write-Host "安装源：$Source"
$nsisArgs = @(
    "/V3",
    "/WX",
    "/DVERSION=$Version",
    "/DVERSION4=$Version.0",
    "/DSOURCE=$Source",
    "/DOUT=$Output",
    "/DICON=$icon",
    $script
)
& $makensis @nsisArgs
if ($LASTEXITCODE -ne 0) { throw "NSIS 安装包构建失败" }
if (-not (Test-Path $Output)) { throw "NSIS 没有生成安装包：$Output" }

$hash = (Get-FileHash -Algorithm SHA256 $Output).Hash.ToLowerInvariant()
Write-Host ""
Write-Host "安装包：$Output" -ForegroundColor Green
Write-Host "SHA-256：$hash" -ForegroundColor Green
