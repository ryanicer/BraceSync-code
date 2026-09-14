#Requires -Version 5
# T054 双端小程序「真实模式」E2E 一键编排（Windows 侧，需微信开发者工具 + staging 构建）
#
# 与 T053（staging 服务器 run-real-e2e.sh，Playwright）不同：小程序真实模式必须由 miniprogram-automator
# 连接运行中的微信开发者工具自动化端口，故本脚本运行在 Windows/Boss 侧，做「构建 → 起自动化 → 跑 driver →
# 汇总 → 上报 staging runbook」全链路。
#
# 前置：
#   1) 微信开发者工具已授权自动化（工具→设置→安全→服务端口），Boss 侧可启动 IDE
#   2) 本机 npm 依赖已装（miniprogram-automator / lib-target 自证基于产物）
#   3) 可选 env：TECH_PHONE/TECH_PASSWORD/TECH_DEVICE_ID/TECH_PATIENT_ID/PATIENT_TOKEN/PATIENT_ID
#
# 用法（PowerShell）：
#   $env:AUTO_CONNECT_ONLY='1'   # 若 devtools 端口已由 start-automation.ps1 启动
#   .\e2e-miniapp\run-real-e2e.ps1
param(
    [switch]$SkipBuild,
    [switch]$OnlyTech,
    [switch]$OnlyPatient,
    [string]$TECH_DEVICE_ID = $env:TECH_DEVICE_ID,
    [string]$TECH_PATIENT_ID = $env:TECH_PATIENT_ID,
    [string]$PATIENT_TOKEN = $env:PATIENT_TOKEN,
    [string]$PATIENT_ID = $env:PATIENT_ID
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$resultsDir = Join-Path $PSScriptRoot "test-results"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

function Invoke-TechDriver {
    param([string]$Name)
    Write-Host "`n[driver] tech-$Name (port 9420)..." -ForegroundColor Cyan
    $env:AUTO_PORT = '9420'
    & node (Join-Path $PSScriptRoot "tech-$Name.spec.js")
    if ($LASTEXITCODE -ne 0) { Write-Host "  tech-$Name FAIL (exit=$LASTEXITCODE)" -ForegroundColor Red; return $false }
    Write-Host "  tech-$Name PASS" -ForegroundColor Green
    return $true
}

function Invoke-PatientDriver {
    param([string]$Name)
    Write-Host "`n[driver] patient-$Name (port 9422)..." -ForegroundColor Cyan
    $env:AUTO_PORT = '9422'
    & node (Join-Path $PSScriptRoot "patient-$Name.spec.js")
    if ($LASTEXITCODE -ne 0) { Write-Host "  patient-$Name FAIL (exit=$LASTEXITCODE)" -ForegroundColor Red; return $false }
    Write-Host "  patient-$Name PASS" -ForegroundColor Green
    return $true
}

# ── 1) 构建（USE_MOCK=false + TARGET=staging，安全默认）──
if (-not $SkipBuild) {
    Write-Host "[1/5] 构建双端 staging（USE_MOCK=false / TARGET=staging）..." -ForegroundColor Cyan
    $env:TARGET = 'staging'
    Push-Location $root
    try {
        npm -w apps/tech-miniapp run build:mp-weixin; if ($LASTEXITCODE -ne 0) { throw "tech build failed" }
        npm -w apps/patient-miniapp run build:mp-weixin; if ($LASTEXITCODE -ne 0) { throw "patient build failed" }
    } finally { Pop-Location; Remove-Item Env:TARGET -ErrorAction SilentlyContinue }
} else {
    Write-Host "[1/5] 跳过构建（-SkipBuild），使用现有 staging 产物（driver 会自证 target）" -ForegroundColor Yellow
}

# ── 2) 自动化端口：可用时调用既有 start-automation.ps1；否则要求先由 CONNECT_ONLY 就绪 ──
Write-Host "[2/5] 确保微信开发者工具自动化端口就绪..." -ForegroundColor Cyan
function Test-PortOpen {
    param([int]$Port)
    $s = New-Object System.Net.Sockets.TcpClient
    try { $s.Connect("127.0.0.1", $Port); $s.Close(); return $true }
    catch { return $false } finally { $s.Dispose() }
}
$connectOnly = $env:AUTO_CONNECT_ONLY -eq '1' -or (Test-PortOpen 9420 -and Test-PortOpen 9422)
if (-not $connectOnly) {
    Write-Host "  自动化端口未就绪，调用 scripts/wechat/start-automation.ps1 启动（需桌面 IDE）..." -ForegroundColor Yellow
    & (Join-Path $root "scripts\wechat\start-automation.ps1")
}
$env:AUTO_CONNECT_ONLY = '1'

# ── 3) 跑 driver ──
$techOk = $true; $patientOk = $true
Write-Host "[3/5] 执行 driver..." -ForegroundColor Cyan
if (-not $OnlyPatient) {
    $techOk = (Invoke-TechDriver "login") -and (Invoke-TechDriver "bind") -and (Invoke-TechDriver "records")
}
if (-not $OnlyTech) {
    $env:PATIENT_TOKEN = $PATIENT_TOKEN
    $env:PATIENT_ID = $PATIENT_ID
    $patientOk = (Invoke-PatientDriver "login") -and (Invoke-PatientDriver "monitor") -and (Invoke-PatientDriver "history")
}

# ── 4) 汇总 ──
Write-Host "[4/5] 汇总结果..." -ForegroundColor Cyan
$enabled = @()
if (-not $OnlyPatient) { $enabled += @('tech-login','tech-bind','tech-records') }
if (-not $OnlyTech)    { $enabled += @('patient-login','patient-monitor','patient-history') }
$passed = 0; $failed = 0
foreach ($n in $enabled) {
    $p = Join-Path $resultsDir "$n.json"
    if (Test-Path $p) {
        $j = Get-Content $p -Raw | ConvertFrom-Json
        if ($j.pass) { $passed++ } else { $failed++ }
        Write-Host ("  {0,-20} {1}" -f $n, ($(if ($j.pass) { 'PASS' } else { 'FAIL' })))
    } else {
        Write-Host ("  {0,-20} NO-RESULT" -f $n); $failed++
    }
}
Write-Host "总： 通过 $passed / $($passed + $failed)" -ForegroundColor $(if ($failed -eq 0) { 'Green' } else { 'Red' })

# ── 5) 上报 staging runbook（供 PM 查看；需已配 scp/ssh 可到 106.52.39.208）──
Write-Host "[5/5] 上报 staging runbook /opt/bracesync-staging/e2e-miniapp-results/..." -ForegroundColor Cyan
$remote = $env:STAGING_RUNBOOK_HOST
if ($remote) {
    scp $resultsDir\*.json "${remote}:/opt/bracesync-staging/e2e-miniapp-results/"
    if ($LASTEXITCODE -eq 0) { Write-Host "  已上报" -ForegroundColor Green } else { Write-Host "  上报失败（不影响本地结果）" -ForegroundColor Yellow }
} else {
    Write-Host "  跳过上报（未配置 STAGING_RUNBOOK_HOST）" -ForegroundColor DarkGray
}

exit $(if ($failed -eq 0) { 0 } else { 1 })