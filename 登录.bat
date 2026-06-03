@echo off
chcp 65001 >nul
title XhhRobot - Login
cd /d "%~dp0"

where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [错误] 未检测到 Go 环境，请先安装 Go：https://go.dev/dl/
    pause
    exit /b 1
)

echo ========================================
echo   小黑盒扫码登录 - 获取 Cookie
echo ========================================
echo.
go run main.go -mode login
echo.
echo 登录流程结束。
pause
