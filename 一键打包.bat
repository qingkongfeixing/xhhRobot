@echo off
title XhhRobot - Build Windows
cd /d "%~dp0"

echo ========================================
echo   编译 Windows 版 xhhRobot
echo ========================================
echo.

:: 清理旧文件
if exist xhhRobot.exe (
    echo [1/3] 清理旧文件...
    del xhhRobot.exe
)

:: 下载依赖
echo [2/3] 同步依赖...
go mod tidy

:: 编译
echo [3/3] 编译中...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -o xhhRobot.exe main.go

if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo   编译成功! -> xhhRobot.exe
    echo ========================================
) else (
    echo.
    echo ========================================
    echo   编译失败，请检查上方错误信息
    echo ========================================
)
pause
