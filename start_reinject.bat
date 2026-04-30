@echo off
chcp 65001 >nul
title 明日方舟日服文本捕获 - 一键重新注入

cd /d "%~dp0"

set "ADB=%cd%\platform-tools\adb.exe"
set "PATH=%cd%\platform-tools;%PATH%"

echo ============================================================
echo  明日方舟日服文本捕获 - 一键重新注入
echo  当前目录: %cd%
echo ============================================================
echo.

if not exist "%ADB%" (
    echo [ERROR] 缺少新版 adb:
    echo %ADB%
    pause
    exit /b
)

echo [ADB] 使用:
echo %ADB%
echo.

echo [ADB] 版本:
"%ADB%" version
echo.

if not exist "start_fanyi.exe" (
    echo [ERROR] 缺少 start_fanyi.exe
    pause
    exit /b
)

if not exist "ui_text_hook.js" (
    echo [ERROR] 缺少 ui_text_hook.js
    pause
    exit /b
)

echo [1/8] 结束电脑端旧进程...
taskkill /f /im start_fanyi.exe >nul 2>nul
taskkill /f /im frida.exe >nul 2>nul
taskkill /f /im adb.exe >nul 2>nul

echo [2/8] 重启新版 ADB server...
"%ADB%" kill-server >nul 2>nul
timeout /t 2 >nul
"%ADB%" start-server
echo.

echo [3/8] 等待设备...
"%ADB%" wait-for-device

echo [4/8] 检查设备...
"%ADB%" devices
echo.

"%ADB%" get-state >nul 2>nul
if errorlevel 1 (
    echo [ERROR] 没检测到设备，请检查 USB 调试和授权弹窗。
    pause
    exit /b
)

echo [OK] ADB 正常
echo.

echo [5/8] 检查 Frida...
frida-ps -U >nul 2>nul
if errorlevel 1 (
    echo [ERROR] frida-ps -U 失败。
    echo 请先启动手机端 Frida 服务。
    pause
    exit /b
)

echo [OK] Frida 正常
echo.

echo [6/8] 清理 logcat...
"%ADB%" logcat -c >nul 2>nul

echo [7/8] 启动明日方舟...
"%ADB%" shell monkey -p com.YoStarJP.Arknights -c android.intent.category.LAUNCHER 1 >nul 2>nul

echo.
echo 等待游戏进程...
set "PID="

:wait_game
set "PID="
for /f "usebackq delims=" %%p in (`"%ADB%" shell pidof com.YoStarJP.Arknights 2^>nul`) do set "PID=%%p"

if "%PID%"=="" (
    timeout /t 2 >nul
    goto wait_game
)

echo [OK] 游戏 PID: %PID%
echo.

echo [8/8] 启动文本捕获...
echo ------------------------------------------------------------
echo  屏幕只显示当前捕获文本：
echo  有中文翻译 -^> 显示中文
echo  没中文翻译 -^> 显示日文
echo  新文本会写入 trans.json
echo.
echo  [注意] 如果游戏注入后反复崩溃，请关闭本窗口后改用：
echo     start_fanyi.exe --spawn
echo  spawn 模式由 frida 直接拉起游戏，可绕过运行时完整性检测
echo ------------------------------------------------------------
echo.

set SPAWN_MODE=
if /i "%1"=="--spawn" set SPAWN_MODE=--spawn
if /i "%SPAWN%"=="1" set SPAWN_MODE=--spawn

start_fanyi.exe %SPAWN_MODE%

echo.
echo 程序已退出。
pause