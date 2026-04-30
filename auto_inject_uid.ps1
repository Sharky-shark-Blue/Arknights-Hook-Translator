$package = "com.YoStarJP.Arknights"
$injector = "/data/adb/modules/magisk-frida-inject/system/bin/frida-inject"
$remoteScript = "/data/local/tmp/ui_text_hook.js"
$localScript = ".\ui_text_hook.js"

Write-Host "[*] push hook script..."
adb push $localScript $remoteScript

$lastGamePid = ""

while ($true) {
    $rawPid = adb shell pidof $package
    $gamePid = ($rawPid -replace "`r", "" -replace "`n", " ").Trim()

    if ([string]::IsNullOrWhiteSpace($gamePid)) {
        Write-Host "[!] game not running, starting..."
        adb shell monkey -p $package 1 | Out-Null
        Start-Sleep -Seconds 5
        continue
    }

    # 如果 pidof 返回多个 PID，只取第一个
    $gamePid = ($gamePid -split "\s+")[0]

    if ($gamePid -ne $lastGamePid) {
        Write-Host "[*] new PID detected: $gamePid"
        $lastGamePid = $gamePid

        Write-Host "[*] injecting..."
        adb shell su -c "$injector -p $gamePid -s $remoteScript"

        Write-Host "[!] inject process ended, waiting..."
        Start-Sleep -Seconds 2
    } else {
        Start-Sleep -Seconds 2
    }
}