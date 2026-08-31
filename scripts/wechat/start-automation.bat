@echo off
REM ============================================================
REM  BraceSync - 微信小程序自动化启动脚本
REM  用法：双击运行，或在 CMD 中执行
REM  作用：用微信开发者工具 CLI 启动双端小程序自动化端口
REM  技师端：端口 9420
REM  患者端：端口 9422
REM ============================================================

setlocal

set "CLI=C:\Program Files (x86)\Tencent\微信web开发者工具\cli.bat"
set "ROOT=%~dp0..\.."

set "TECH_PROJECT=%ROOT%\apps\tech-miniapp\dist\build\mp-weixin"
set "PATIENT_PROJECT=%ROOT%\apps\patient-miniapp\dist\build\mp-weixin"

echo [1/2] 启动技师端自动化（端口 9420）...
start "" cmd /c ""%CLI%" auto --project "%TECH_PROJECT%" --auto-port 9420"
echo   已启动，等待端口开放...
timeout /t 8 /nobreak >nul

echo [2/2] 启动患者端自动化（端口 9422）...
start "" cmd /c ""%CLI%" auto --project "%PATIENT_PROJECT%" --auto-port 9422"
echo   已启动，等待端口开放...
timeout /t 8 /nobreak >nul

echo.
echo ============================================================
echo  双端自动化端口启动完成！
echo  技师端: 端口 9420
echo  患者端: 端口 9422
echo.
echo  接下来在 Iris 环境执行:
echo    node scripts/wechat/smoke-tech.js
echo    node scripts/wechat/smoke-patient.js
echo  （脚本会自动连接已运行的自动化端口）
echo ============================================================
echo.
pause
