@echo off
REM ============================================================
REM  BraceSync - WeChat Miniapp Automation Launcher
REM  Usage: double-click or run from CMD
REM  Launches two WeChat DevTools instances with automation ports:
REM    Tech miniapp   : port 9420
REM    Patient miniapp: port 9422
REM ============================================================

setlocal

set "CLI=C:\Program Files (x86)\Tencent\微信web开发者工具\cli.bat"
set "ROOT=%~dp0..\.."

set "TECH_PROJECT=%ROOT%\apps\tech-miniapp\dist\build\mp-weixin"
set "PATIENT_PROJECT=%ROOT%\apps\patient-miniapp\dist\build\mp-weixin"

echo [1/2] Starting TECH miniapp automation (port 9420)...
start "" cmd /c ""%CLI%" auto --project "%TECH_PROJECT%" --auto-port 9420"
echo   Started, waiting for port...
timeout /t 10 /nobreak >nul

echo [2/2] Starting PATIENT miniapp automation (port 9422)...
start "" cmd /c ""%CLI%" auto --project "%PATIENT_PROJECT%" --auto-port 9422"
echo   Started, waiting for port...
timeout /t 10 /nobreak >nul

echo.
echo ============================================================
echo  Both automation ports should be ready now.
echo    Tech    : port 9420
echo    Patient : port 9422
echo.
echo  Then run in Iris terminal:
echo    node scripts/wechat/smoke-tech.js
echo    node scripts/wechat/smoke-patient.js
echo  (scripts auto-connect to running automation ports)
echo ============================================================
echo.
pause
