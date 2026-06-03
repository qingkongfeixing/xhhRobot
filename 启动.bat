@echo off
chcp 65001 >nul
title XhhRobot
cd /d "%~dp0"

where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [错误] 未检测到 Go 环境！
    echo 请先安装 Go：https://go.dev/dl/
    pause
    exit /b 1
)

if not exist "config.json" (
    echo [错误] 未找到 config.json！
    echo 请复制 config.json.example 为 config.json 并填入你的配置。
    echo.
    echo 命令：copy config.json.example config.json
    echo.
    pause
    exit /b 1
)

if not exist "cookie.json" (
    echo [提示] 未检测到 cookie.json，首次使用请在 Web 控制台扫码登录。
    echo.
)

echo ========================================
echo   XhhRobot - AI 回复机器人
echo   http://localhost:8080
echo ========================================
echo.
go run main.go -mode start
echo.
echo 程序已停止。
pause
