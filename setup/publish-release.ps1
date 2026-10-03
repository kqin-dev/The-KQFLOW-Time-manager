# 用 GitHub API 发布 Kairos Release。
#
# 之所以不用 gh CLI：本机没有安装它。这里直接用 REST API，
# 凭据从 Git 凭据管理器读取（应用内不保存任何 token）。
#
# 用法：
#   pwsh -File setup/publish-release.ps1 -Token <PAT>            # 正式发布
#   pwsh -File setup/publish-release.ps1 -Token <PAT> -Draft     # 存成草稿
#   pwsh -File setup/publish-release.ps1 -Token <PAT> -WhatIfOnly # 只打印将要做什么
#
# 参数：
#   -Token       GitHub Personal Access Token（需要 repo 权限）
#   -Tag         默认 v<版本号>
#   -Version     默认读 internal/version/version.go
#   -Draft       创建为草稿而非直接发布
#   -NotesFile   自定义发布说明文件，默认 setup/release-notes.md
#   -WhatIfOnly  只校验与打印，不发任何请求
#   -Retag       把 tag 强制指到当前 HEAD 再发布（修已发布版本的产物时用）

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$Tag = '',
    [string]$Version = '',
    [switch]$Draft,
    [string]$NotesFile = '',
    [switch]$Retag,
    [switch]$WhatIfOnly
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$owner = 'kqin-dev'
$repo = 'The-Kairos-Time-manager'

function Info($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Fail($m) { Write-Host "错误: $m" -ForegroundColor Red; exit 1 }

# ---------- 版本号与 tag ----------

if (-not $Version) {
    $vf = Join-Path $repoRoot 'internal\version\version.go'
    $line = Select-String -Path $vf -Pattern '^\s*var\s+Version\s*=\s*"([^"]+)"' | Select-Object -First 1
    if (-not $line) { Fail "在 $vf 里找不到 var Version" }
    $Version = $line.Matches[0].Groups[1].Value
}
if (-not $Tag) { $Tag = "v$Version" }
Info "版本 $Version，tag $Tag"

# ---------- 产物 ----------

$setupExe = Join-Path $repoRoot "dist\Kairos-$Version-setup.exe"
$portableExe = Join-Path $repoRoot 'kair.exe'
if (-not (Test-Path $setupExe)) { Fail "找不到安装包：$setupExe（先跑 setup\build-installer.ps1）" }
if (-not (Test-Path $portableExe)) { Fail "找不到绿色版：$portableExe" }

$assets = @(
    @{ Path = $setupExe;    Name = "Kairos-$Version-setup.exe"; Label = 'Windows 安装程序' },
    @{ Path = $portableExe; Name = "kair-$Version-windows-amd64.exe"; Label = 'Windows 绿色版' }
)
foreach ($a in $assets) {
    $mb = [math]::Round((Get-Item $a.Path).Length / 1MB, 2)
    Info "产物 $($a.Name)（$mb MB）"
}

# ---------- 发布说明 ----------

if (-not $NotesFile) { $NotesFile = Join-Path $PSScriptRoot 'release-notes.md' }
if (-not (Test-Path $NotesFile)) { Fail "找不到发布说明：$NotesFile" }
$body = [System.IO.File]::ReadAllText($NotesFile, [System.Text.Encoding]::UTF8)
Info "发布说明 $((Get-Item $NotesFile).Length) 字节"

if ($WhatIfOnly) {
    Write-Host ''
    Info "仅校验模式：以下请求不会真的发出"
    Write-Host "    POST https://api.github.com/repos/$owner/$repo/releases"
    Write-Host "      tag_name=$Tag  name=Kairos $Tag  draft=$($Draft.IsPresent)"
    foreach ($a in $assets) {
        Write-Host "    POST https://uploads.github.com/repos/$owner/$repo/releases/<id>/assets?name=$($a.Name)"
    }
    exit 0
}

# ---------- 请求头 ----------
$headers = @{
    Authorization          = "Bearer $Token"
    Accept                 = 'application/vnd.github+json'
    'X-GitHub-Api-Version' = '2022-11-28'
    'User-Agent'           = 'kairos-release-script'
}

function Invoke-GitHub {
    param([string]$Method, [string]$Uri, $Body = $null, [string]$ContentType = 'application/json')
    $args = @{ Method = $Method; Uri = $Uri; Headers = $headers }
    if ($Body -ne $null) {
        if ($ContentType -eq 'application/json') {
            $args.Body = ($Body | ConvertTo-Json -Depth 6)
            $args.ContentType = 'application/json; charset=utf-8'
        } else {
            $args.Body = $Body
            $args.ContentType = $ContentType
        }
    }
    return Invoke-RestMethod @args
}

# ---------- 0. 校验 token 与权限 ----------

try {
    $me = Invoke-GitHub -Method GET -Uri 'https://api.github.com/user'
    Info "已认证为 $($me.login)"
} catch {
    Fail "token 校验失败：$($_.Exception.Message)"
}

# ---------- 1. 已存在就先删掉，保证可重复执行 ----------

try {
    $existing = Invoke-GitHub -Method GET -Uri "https://api.github.com/repos/$owner/$repo/releases/tags/$Tag"
    Info "已存在 tag $Tag 的 Release（id=$($existing.id)），先删除以便重建"
    Invoke-GitHub -Method DELETE -Uri "https://api.github.com/repos/$owner/$repo/releases/$($existing.id)" | Out-Null
} catch {
    # 404 就是还没有，正常
}

# ---------- 1b. 需要时把 tag 指到当前 HEAD ----------

if ($Retag) {
    Push-Location $repoRoot
    # git 会把进度写到 stderr（例如 "To https://..."），
    # 而 $ErrorActionPreference='Stop' 会把原生命令的 stderr 当成错误中断脚本。
    # 这里临时放宽，只看退出码。
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $head = (git rev-parse HEAD).Trim()
        Info "把 tag $Tag 强制指到当前 HEAD（$($head.Substring(0,7))）"
        git tag -f -a $Tag -m "Kairos $Tag" 2>$null
        if ($LASTEXITCODE -ne 0) { Fail "本地打 tag 失败" }
        git push origin "refs/tags/$Tag" --force 2>$null
        if ($LASTEXITCODE -ne 0) { Fail "推送 tag 失败（需要 force push 权限）" }
        Info "tag 已更新"
    } finally {
        $ErrorActionPreference = $prevEAP
        Pop-Location
    }
}

# ---------- 2. 创建 Release ----------

$payload = @{
    tag_name         = $Tag
    name             = "Kairos $Tag"
    body             = $body
    draft            = [bool]$Draft
    prerelease       = $false
}
Info "创建 Release（draft=$($Draft.IsPresent)）"
$release = Invoke-GitHub -Method POST -Uri "https://api.github.com/repos/$owner/$repo/releases" -Body $payload
Info "Release 已创建：$($release.html_url)"

# ---------- 3. 上传附件 ----------

foreach ($a in $assets) {
    $uploadUri = "https://uploads.github.com/repos/$owner/$repo/releases/$($release.id)/assets?name=$($a.Name)"
    Info "上传 $($a.Name) …"
    try {
        $res = Invoke-RestMethod -Method POST -Uri $uploadUri -Headers $headers `
            -ContentType 'application/octet-stream' -InFile $a.Path
        Info "  完成：$($res.browser_download_url)"
    } catch {
        Fail "上传 $($a.Name) 失败：$($_.Exception.Message)"
    }
}

Write-Host ''
Info "发布完成"
Write-Host "    $($release.html_url)" -ForegroundColor Green
if ($Draft) {
    Write-Host "    注意：这是草稿，需要在网页上点 Publish 才会公开。" -ForegroundColor Yellow
}
