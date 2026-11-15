@echo off
setlocal
set "WUBU_PWSH=%LOCALAPPDATA%\Programs\PowerShell\7\pwsh.exe"
if not exist "%WUBU_PWSH%" set "WUBU_PWSH=pwsh.exe"
"%WUBU_PWSH%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start.ps1" %*
set "WUBU_EXIT=%ERRORLEVEL%"
pause
exit /b %WUBU_EXIT%
