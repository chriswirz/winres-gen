@echo off
rem Thin wrapper so `build.cmd <target>` works from cmd.exe; build.ps1 does the work.
rem Targets: build (default), clean, test, all, cross.
setlocal
set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=build"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" -Target %TARGET%
exit /b %ERRORLEVEL%
