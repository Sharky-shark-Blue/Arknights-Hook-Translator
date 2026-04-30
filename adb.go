package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	adbExe        = "adb"
	workDirGlobal = ""
)

func run(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			fmt.Printf("[!] %s %v\n%s\n", name, args, text)
		} else {
			fmt.Printf("[!] %s %v: %v\n", name, args, err)
		}
	}
	return string(out)
}

func mustExist(path string) {
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("[ERROR] 缺少文件：%s\n", path)
		os.Exit(1)
	}
}

func getExeDir() string {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("[ERROR] 获取程序路径失败:", err)
		os.Exit(1)
	}
	dir, err := filepath.Abs(filepath.Dir(exePath))
	if err != nil {
		fmt.Println("[ERROR] 获取程序目录失败:", err)
		os.Exit(1)
	}
	return dir
}

func initAdbPath(workDir string) {
	bundledAdb := filepath.Join(workDir, "platform-tools", "adb.exe")
	if _, err := os.Stat(bundledAdb); err == nil {
		adbExe = bundledAdb
	}
	fmt.Println("[ADB] 使用:", adbExe)
	fmt.Print(run(adbExe, "version"))
}

func clearConsole() {
	if guiMode {
		return
	}
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

func adbShell(args ...string) string {
	all := append([]string{"shell"}, args...)
	return run(adbExe, all...)
}

func getPid() string {
	out := adbShell("pidof", packageName)
	out = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(out, "\r", ""), "\n", " "))
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func clearLogcat() {
	run(adbExe, "logcat", "-c")
}

func cleanupResiduals() {
	adbShell("su", "-c", "pkill -f "+packageName+" 2>/dev/null; pkill -f frida-inject 2>/dev/null; pkill -x fserver 2>/dev/null; true")
	time.Sleep(2 * time.Second)
}

func ensureFridaServer() {
	// 如果设备上还没有改名后的二进制，先推送并 chmod
	exists := strings.TrimSpace(run(adbExe, "shell", "su -c '[ -f "+fridaServerDevicePath+" ] && echo 1 || echo 0'"))
	if exists != "1" {
		localBin := filepath.Join(workDirGlobal, fridaServerLocalBin)
		fmt.Println("[INFO] 推送 frida-server → fserver ...")
		run(adbExe, "push", localBin, fridaServerDevicePath)
		adbShell("su", "-c", "chmod 755 "+fridaServerDevicePath)
	}

	// 检测自定义端口是否已在监听
	listening := run(adbExe, "shell", "su -c 'ss -tlnp 2>/dev/null | grep -c "+fridaPort+"'")
	if strings.TrimSpace(listening) != "" && strings.TrimSpace(listening) != "0" {
		run(adbExe, "forward", "tcp:"+fridaPort, "tcp:"+fridaPort)
		return
	}

	fmt.Println("[INFO] fserver 未运行，正在以端口", fridaPort, "启动...")
	adbShell("su", "-c", "nohup "+fridaServerDevicePath+" -l 0.0.0.0:"+fridaPort+" > /data/local/tmp/frida.log 2>&1 &")
	time.Sleep(3 * time.Second)
	run(adbExe, "forward", "tcp:"+fridaPort, "tcp:"+fridaPort)

	listening2 := run(adbExe, "shell", "su -c 'ss -tlnp 2>/dev/null | grep -c "+fridaPort+"'")
	if strings.TrimSpace(listening2) == "" || strings.TrimSpace(listening2) == "0" {
		fmt.Println("[WARN] fserver 启动失败，查看日志:")
		fmt.Println(run(adbExe, "shell", "su -c 'cat /data/local/tmp/frida.log'"))
	}
}

func checkFridaReady() bool {
	ensureFridaServer()
	out := run("frida-ps", "-H", "127.0.0.1:"+fridaPort)
	lower := strings.ToLower(out)
	if strings.Contains(lower, "failed") ||
		strings.Contains(lower, "unable") ||
		strings.Contains(lower, "closed") ||
		strings.Contains(lower, "error") {
		fmt.Println("[ERROR] frida-ps -U 失败")
		fmt.Println(out)
		return false
	}
	return true
}

func captureCrashLog(reason string) {
	crashDir := filepath.Join(workDirGlobal, "crash_logs")
	_ = os.MkdirAll(crashDir, 0755)

	now := time.Now().Format("20060102_150405")
	fullLog := filepath.Join(crashDir, "crash_"+now+".txt")
	keyLog := filepath.Join(crashDir, "crash_"+now+"_key.txt")

	time.Sleep(2 * time.Second)

	keywords := []string{
		"FATAL", "AndroidRuntime", "SIGSEGV", "SIGABRT", "SIGBUS",
		"libil2cpp", "libunity", "YoStar", "Arknights",
		"Exception", "CRASH", "crash", "tombstone",
	}

	cmd := exec.Command(adbExe, "logcat", "-d")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("[WARN] 获取 logcat pipe 失败:", err)
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("[WARN] 启动 logcat 失败:", err)
		return
	}

	fullF, err := os.Create(fullLog)
	if err != nil {
		_ = cmd.Wait()
		return
	}
	keyF, err := os.Create(keyLog)
	if err != nil {
		_ = fullF.Close()
		_ = cmd.Wait()
		return
	}

	hasKeyLines := false
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, scannerMaxBuf)
	scanner.Buffer(buf, scannerMaxBuf)

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(fullF, line)
		for _, kw := range keywords {
			if strings.Contains(line, kw) {
				_, _ = fmt.Fprintln(keyF, line)
				hasKeyLines = true
				break
			}
		}
	}

	_ = fullF.Close()
	_ = keyF.Close()
	_ = cmd.Wait()

	clearConsole()
	fmt.Println("游戏进程已结束或崩溃")
	fmt.Println("崩溃日志已保存：", fullLog)
	if hasKeyLines {
		fmt.Println("关键日志：", keyLog)
	} else {
		_ = os.Remove(keyLog)
	}
	fmt.Println("原因：", reason)
}
