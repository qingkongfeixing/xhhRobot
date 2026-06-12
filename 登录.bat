@echo off
title XhhRobot - Login
cd /d "%~dp0"

where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go not found!
    echo Install Go: https://go.dev/dl/
    pause
    exit /b 1
)

echo ========================================
echo   XhhRobot - Login (QR Code)
echo ========================================
echo.
go run main.go -mode login
echo.
echo Login finished.
pause
