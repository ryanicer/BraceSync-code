#Requires -Version 5
# BraceSync - WeChat Miniapp Automation Launcher (PowerShell)
# Usage: Right-click -> Run with PowerShell, or:
#   powershell -ExecutionPolicy Bypass -File .\scripts\wechat\start-automation.ps1

$ErrorActionPreference = 'Stop'

$cliPath = "C:\Program Files (x86)\Tencent\微信web开发者工具\cli.bat"
$root = Split-Path -Parent $PSScriptRoot  # scripts\wechat\..\..
$root = Split-Path -Parent $root

$techProject = Join-Path $root "apps\tech-miniapp\dist\build\mp-weixin"
$patientProject = Join-Path $root "apps\patient-miniapp\dist\build\mp-weixin"

if (-not (Test-Path $cliPath)) {
    Write-Host "ERROR: CLI not found at $cliPath" -ForegroundColor Red
    exit 1
}

function Test-PortOpen {
    param([int]$Port)
    $s = New-Object System.Net.Sockets.TcpClient
    try {
        $s.Connect("127.0.0.1", $Port)
        $s.Close()
        return $true
    } catch {
        return $false
    } finally {
        $s.Dispose()
    }
}

Write-Host "[1/2] Starting TECH miniapp automation (port 9420)..." -ForegroundColor Cyan
Start-Process cmd -ArgumentList "/c `"`"$cliPath`" auto --project `"$techProject`" --auto-port 9420`"" -WindowStyle Normal
Write-Host "  Waiting for port..."
for ($i = 0; $i -lt 30; $i++) {
    if (Test-PortOpen 9420) { Write-Host "  Port 9420 is OPEN" -ForegroundColor Green; break }
    Start-Sleep -Seconds 1
    Write-Host "." -NoNewline
}

Write-Host ""
Write-Host "[2/2] Starting PATIENT miniapp automation (port 9422)..." -ForegroundColor Cyan
Start-Process cmd -ArgumentList "/c `"`"$cliPath`" auto --project `"$patientProject`" --auto-port 9422`"" -WindowStyle Normal
Write-Host "  Waiting for port..."
for ($i = 0; $i -lt 30; $i++) {
    if (Test-PortOpen 9422) { Write-Host "  Port 9422 is OPEN" -ForegroundColor Green; break }
    Start-Sleep -Seconds 1
    Write-Host "." -NoNewline
}

Write-Host ""
Write-Host "============================================================" -ForegroundColor Yellow
Write-Host "  Both automation ports should be ready now."
Write-Host "    Tech    : port 9420"
Write-Host "    Patient : port 9422"
Write-Host ""
Write-Host "  Then run in Iris terminal:"
Write-Host "    `$env:CONNECT_ONLY=1; node scripts/wechat/smoke-tech.js"
Write-Host "    `$env:CONNECT_ONLY=1; node scripts/wechat/smoke-patient.js"
Write-Host "============================================================" -ForegroundColor Yellow
Write-Host ""
Read-Host "Press Enter to exit"
