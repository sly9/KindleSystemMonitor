@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0uninstall-windows.ps1"
set RC=%ERRORLEVEL%
pause
exit /b %RC%
