@echo off
title XhhRobot - Build Windows
cd /d "%~dp0"

echo ========================================
echo   Build Windows xhhRobot
echo ========================================
echo.

if exist xhhRobot.exe (
    echo [1/3] Cleaning old files...
    del xhhRobot.exe
)

echo [2/3] go mod tidy...
go mod tidy

echo [3/3] Building...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -o xhhRobot.exe main.go

if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo   Success! -^> xhhRobot.exe
    echo ========================================
) else (
    echo.
    echo ========================================
    echo   Build failed!
    echo ========================================
)
pause
