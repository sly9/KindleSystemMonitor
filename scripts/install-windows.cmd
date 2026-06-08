@echo off
rem Wrapper that lets users double-click the install without fighting
rem PowerShell's ExecutionPolicy. -NoProfile keeps startup fast.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0install-windows.ps1"
set RC=%ERRORLEVEL%
if not "%RC%"=="0" pause
exit /b %RC%
