@echo off
title XhhRobot
cd /d "%~dp0"

where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go not found!
    echo Install Go: https://go.dev/dl/
    pause
    exit /b 1
)

if not exist "config.json" (
    echo [ERROR] config.json not found!
    echo Copy config.json.example to config.json and edit it.
    echo.
    pause
    exit /b 1
)

if not exist "cookie.json" (
    echo [INFO] cookie.json not found, please login via web console.
    echo.
)

echo ========================================
echo   XhhRobot
echo   http://localhost:8080
echo ========================================
echo.
go run main.go -mode start
echo.
echo Stopped.
pause
