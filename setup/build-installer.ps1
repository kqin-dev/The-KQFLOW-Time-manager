# 构建 KQFLOW 安装包。
#
# 做三件事：
#   1. 从 internal/version/version.go 读出唯一权威的版本号；
#   2. 用它编译 kqf.exe（同时把版本号注入二进制）；
#   3. 调用 Inno Setup 的 ISCC 生成 dist\KQFLOW-<版本>-setup.exe。
#
# 用法：
#   pwsh -File setup\build-installer.ps1                 # 正常构建
#   pwsh -File setup\build-installer.ps1 -SkipGoBuild    # 复用已有 kqf.exe
#   pwsh -File setup\build-installer.ps1 -Iscc <路径>    # 指定 ISCC.exe
#
# 参数：
#   -SkipGoBuild  跳过 go build，直接用仓库根目录已有的 kqf.exe
#   -Iscc         指定 ISCC.exe 路径，默认自动查找
#   -OutDir       输出目录，默认 <仓库根>\dist

[CmdletBinding()]
param(
    [switch]$SkipGoBuild,
    [string]$Iscc = '',
    [string]$OutDir = ''
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$setupDir = $PSScriptRoot

function Info($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Fail($msg) { Write-Host "错误: $msg" -ForegroundColor Red; exit 1 }

# ---------- 1. 读版本号 ----------

$versionFile = Join-Path $repoRoot 'internal\version\version.go'
if (-not (Test-Path $versionFile)) {
    Fail "找不到版本文件：$versionFile"
}

# 只认形如 var Version = "1.2.3" 的那一行。改版本号只需改这一处。
$versionLine = Select-String -Path $versionFile -Pattern '^\s*var\s+Version\s*=\s*"([^"]+)"' |
    Select-Object -First 1
if (-not $versionLine) {
    Fail "在 $versionFile 里找不到 var Version = `"...`" 声明"
}
$version = $versionLine.Matches[0].Groups[1].Value
Info "版本号：$version（来自 internal/version/version.go）"

if ($version -notmatch '^\d+\.\d+\.\d+') {
    Write-Host "警告: 版本号 `"$version`" 看起来不是语义化版本，安装包文件名会照用。" -ForegroundColor Yellow
}

# ---------- 2. 编译 kqf.exe ----------

$exePath = Join-Path $repoRoot 'kqf.exe'

if ($SkipGoBuild) {
    if (-not (Test-Path $exePath)) {
        Fail "指定了 -SkipGoBuild，但 $exePath 不存在"
    }
    Info "跳过编译，复用已有的 kqf.exe"
} else {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Fail "PATH 里找不到 go，无法编译。请安装 Go 或用 -SkipGoBuild 复用已有 exe。"
    }

    # 版本号注入二进制，保证 kqf -version 与安装包一致。
    $ldflags = "-s -w -X github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version.Version=$version"
    Info "编译 kqf.exe（-ldflags `"$ldflags`"）"

    Push-Location $repoRoot
    try {
        & go build -trimpath -ldflags $ldflags -o $exePath ./cmd/kqf
        if ($LASTEXITCODE -ne 0) { Fail "go build 失败（退出码 $LASTEXITCODE）" }
    } finally {
        Pop-Location
    }
}

$sizeMB = [math]::Round((Get-Item $exePath).Length / 1MB, 2)
Info "kqf.exe 就绪（$sizeMB MB）"

# 顺手确认二进制里的版本号真的是我们要发的那个。
try {
    $reported = (& $exePath -version 2>&1 | Out-String).Trim()
    Info "二进制自报版本：$reported"
    if ($reported -notmatch [regex]::Escape($version)) {
        Write-Host "警告: 二进制自报版本与 $version 不一致，请检查 -SkipGoBuild 是否复用了旧文件。" -ForegroundColor Yellow
    }
} catch {
    Write-Host "警告: 无法执行 kqf.exe -version 校验版本（$($_.Exception.Message)）" -ForegroundColor Yellow
}

# ---------- 3. 找 ISCC ----------

function Resolve-Iscc {
    param([string]$Explicit)

    if ($Explicit) {
        if (Test-Path $Explicit) { return (Resolve-Path $Explicit).Path }
        Fail "指定的 ISCC 不存在：$Explicit"
    }

    $cmd = Get-Command iscc.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    $candidates = @(
        "$env:ProgramFiles\Inno Setup 7\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 7\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        'D:\Inno_Setup_7\ISCC.exe',
        'D:\Inno_Setup_6\ISCC.exe',
        'C:\Inno_Setup_7\ISCC.exe'
    )
    foreach ($c in $candidates) {
        if ($c -and (Test-Path $c)) { return $c }
    }

    # 最后在常见根目录里浅层找一遍。
    foreach ($root in @("$env:ProgramFiles", "${env:ProgramFiles(x86)}", 'C:\', 'D:\')) {
        if (-not $root -or -not (Test-Path $root)) { continue }
        $hit = Get-ChildItem -Path $root -Filter 'ISCC.exe' -Recurse -Depth 3 -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }

    Fail @"
找不到 ISCC.exe（Inno Setup 的命令行编译器）。
请先安装 Inno Setup 6 或 7：https://jrsoftware.org/isdl.php
或用 -Iscc 指定路径，例如：
  pwsh -File setup\build-installer.ps1 -Iscc 'D:\Inno_Setup_7\ISCC.exe'
"@
}

$isccPath = Resolve-Iscc -Explicit $Iscc
Info "使用 ISCC：$isccPath"

# ---------- 4. 生成安装包 ----------

if (-not $OutDir) { $OutDir = Join-Path $repoRoot 'dist' }
if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }
$OutDir = (Resolve-Path $OutDir).Path

$issPath = Join-Path $setupDir 'kqflow.iss'
if (-not (Test-Path $issPath)) { Fail "找不到安装脚本：$issPath" }

Info "编译安装脚本（输出到 $OutDir）"
# 用 /D 把版本号与仓库根目录传给 .iss；脚本里不硬编码版本号。
# ISCC 会打一大堆解析日志，所以先收起来：成功只留一行，失败再整段贴出。
$isccLog = & $isccPath "/Q" "/DAppVersion=$version" "/DRepoRoot=$repoRoot" "/O$OutDir" $issPath 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host ($isccLog | Out-String) -ForegroundColor DarkGray
    Fail "ISCC 编译失败（退出码 $LASTEXITCODE）"
}
Info "ISCC 编译通过"

# ---------- 5. 汇报产物 ----------

$installer = Join-Path $OutDir "KQFLOW-$version-setup.exe"
if (-not (Test-Path $installer)) {
    $found = Get-ChildItem $OutDir -Filter '*-setup.exe' | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($found) { $installer = $found.FullName } else { Fail "编译成功但没找到输出文件，请检查 $OutDir" }
}

$installerMB = [math]::Round((Get-Item $installer).Length / 1MB, 2)
Write-Host ''
Info "安装包已生成"
Write-Host "    文件：$installer"
Write-Host "    大小：$installerMB MB"
Write-Host "    版本：$version"
Write-Host ''
Write-Host "下一步：把安装包挂到 GitHub Release 上。" -ForegroundColor Green
