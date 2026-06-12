@echo off
title XhhRobot - Build Linux
cd /d "%~dp0"

echo ========================================
echo   Build Linux xhhRobot
echo ========================================
echo.

if exist xhhRobot (
    echo [1/3] Cleaning old files...
    del xhhRobot
)

echo [2/3] go mod tidy...
go mod tidy

echo [3/3] Building...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -o xhhRobot main.go

if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo   Success! -^> xhhRobot (Linux)
    echo ========================================
) else (
    echo.
    echo ========================================
    echo   Build failed!
    echo ========================================
)
pause
