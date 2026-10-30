#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Version = "1.3.0",
    [switch]$SkipFrontendInstall,
    [switch]$SkipZip
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = "Stop"
$appRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $appRoot "..\..")).Path
$distRoot = Join-Path $repoRoot "dist"
$outDir = Join-Path $distRoot "DSHBackup"
$sevenZipSource = Join-Path $repoRoot "tools\7zip\7z2605-extra\x64\7za.exe"
$sevenZipLicense = Join-Path $repoRoot "tools\7zip\7z2605-extra\License.txt"
$expectedSevenZipHash = "7ddf0a3bc451e166679296be630d6f0573eb386159c04184761c43020c484291"
$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

function Write-Step([string]$Message) {
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Resolve-Cargo {
    $command = Get-Command cargo -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    $candidates = Get-ChildItem "$env:USERPROFILE\.rustup\toolchains\*\bin\cargo.exe" -ErrorAction SilentlyContinue |
        Sort-Object FullName -Descending
    if ($candidates) { return $candidates[0].FullName }
    throw "找不到 cargo。请安装 Rust stable (MSVC) 并重新打开终端。"
}

function Resolve-Wails {
    $command = Get-Command wails -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    throw "找不到 wails CLI。请先运行：go install github.com/wailsapp/wails/v2/cmd/wails@latest"
}

if (-not (Test-Path $sevenZipSource)) {
    throw "找不到 7za.exe：$sevenZipSource"
}
$actualHash = (Get-FileHash -Algorithm SHA256 $sevenZipSource).Hash.ToLowerInvariant()
if ($actualHash -ne $expectedSevenZipHash) {
    throw "7za.exe 哈希不匹配。期望 $expectedSevenZipHash，实际 $actualHash"
}

$cargo = Resolve-Cargo
$wails = Resolve-Wails

if (-not $SkipFrontendInstall -and -not (Test-Path (Join-Path $appRoot "frontend\node_modules"))) {
    Write-Step "安装前端依赖"
    Push-Location (Join-Path $appRoot "frontend")
    try {
        npm install
        if ($LASTEXITCODE -ne 0) { throw "npm install 失败" }
    } finally { Pop-Location }
}

Write-Step "运行前端测试"
Push-Location (Join-Path $appRoot "frontend")
try {
    npm run test
    if ($LASTEXITCODE -ne 0) { throw "前端测试失败" }
} finally { Pop-Location }

Write-Step "构建 Rust sidecar (release, static CRT)"
Push-Location (Join-Path $appRoot "sidecar")
try {
    & $cargo build --release
    if ($LASTEXITCODE -ne 0) { throw "Rust sidecar 构建失败" }
} finally { Pop-Location }

$sidecarPath = Join-Path $appRoot "sidecar\target\release\dsh-backup-core.exe"
if (-not (Test-Path $sidecarPath)) { throw "找不到 sidecar 输出：$sidecarPath" }

Write-Step "构建 Wails 应用"
Push-Location $appRoot
try {
    $ldflags = "-X main.Version=$Version -X main.BuildTime=$buildTime"
    & $wails build -clean -platform windows/amd64 -ldflags $ldflags
    if ($LASTEXITCODE -ne 0) { throw "Wails 构建失败" }
} finally { Pop-Location }

$appExe = Join-Path $appRoot "build\bin\DSHBackup.exe"
if (-not (Test-Path $appExe)) { throw "找不到 Wails 输出：$appExe" }

Write-Step "在干净暂存目录组装便携版"
$stageDir = Join-Path $distRoot (".build-DSHBackup-" + [guid]::NewGuid().ToString("N"))
try {
    New-Item -ItemType Directory -Force -Path (Join-Path $stageDir "tools") | Out-Null
    Copy-Item $appExe (Join-Path $stageDir "DSHBackup.exe")
    Copy-Item $sidecarPath (Join-Path $stageDir "dsh-backup-core.exe")
    Copy-Item $sevenZipSource (Join-Path $stageDir "tools\7za.exe")
    if (Test-Path $sevenZipLicense) {
        Copy-Item $sevenZipLicense (Join-Path $stageDir "tools\License.txt")
    } else {
        throw "找不到 7-Zip License.txt：$sevenZipLicense"
    }

    $icon = Join-Path $appRoot "build\windows\icon.ico"
    if (Test-Path $icon) { Copy-Item $icon (Join-Path $stageDir "icon.ico") }

    $packaging = Join-Path $appRoot "packaging"
    Copy-Item (Join-Path $packaging "README.txt") (Join-Path $stageDir "README.txt")
    Copy-Item (Join-Path $packaging "THIRD_PARTY_NOTICES.txt") (Join-Path $stageDir "THIRD_PARTY_NOTICES.txt")
    Copy-Item (Join-Path $packaging "7-Zip-SOURCE.txt") (Join-Path $stageDir "7-Zip-SOURCE.txt")

    $sidecarHash = (Get-FileHash -Algorithm SHA256 (Join-Path $stageDir "dsh-backup-core.exe")).Hash.ToLowerInvariant()
    $exeHash = (Get-FileHash -Algorithm SHA256 (Join-Path $stageDir "DSHBackup.exe")).Hash.ToLowerInvariant()
    $buildInfo = @"
DSH Backup $Version
Build time (UTC): $buildTime
Platform: windows/amd64
7-Zip: 26.05 x64 ($expectedSevenZipHash)
DSHBackup.exe SHA-256: $exeHash
dsh-backup-core.exe SHA-256: $sidecarHash
"@
    Set-Content -Encoding UTF8 -Path (Join-Path $stageDir "BUILD.txt") -Value $buildInfo

    Write-Step "验证复制后的 7za 独立运行"
    $probe = & (Join-Path $stageDir "tools\7za.exe") i
    if ($LASTEXITCODE -ne 0) { throw "复制后的 7za.exe 无法运行" }
    $probe | Select-Object -First 2

    Write-Step "更新便携目录的运行文件（保留 data 和 backups）"
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $outDir "tools") | Out-Null
    Copy-Item (Join-Path $stageDir "*") $outDir -Recurse -Force

    if (-not $SkipZip) {
        Write-Step "从干净暂存目录生成 ZIP"
        $zipPath = Join-Path $distRoot "DSHBackup-$Version-win64.zip"
        if (Test-Path $zipPath) { Remove-Item -Force $zipPath }
        Compress-Archive -Path (Join-Path $stageDir "*") -DestinationPath $zipPath -CompressionLevel Optimal
        Write-Host "ZIP: $zipPath" -ForegroundColor Green
    }
} finally {
    if (Test-Path $stageDir) { Remove-Item -Recurse -Force $stageDir }
}

Write-Host ""
Write-Host "便携版目录：$outDir" -ForegroundColor Green
Write-Host "主程序：$(Join-Path $outDir 'DSHBackup.exe')" -ForegroundColor Green
