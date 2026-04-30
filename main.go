package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	packageName           = "com.YoStarJP.Arknights"
	scannerMaxBuf         = 4 * 1024 * 1024
	autoSaveInterval      = 10 * time.Second
	injectTimeout         = 5 * time.Minute
	fridaPort             = "1234"
	fridaServerLocalBin   = "frida-server-16.1.2-android-arm64"
	fridaServerDevicePath = "/data/local/tmp/fserver"
)

func main() {
	_ = exec.Command("cmd", "/c", "chcp", "65001").Run()

	spawnMode := false
	for _, arg := range os.Args[1:] {
		if arg == "--spawn" {
			spawnMode = true
		}
		if arg == "--gui" {
			guiMode = true
		}
	}

	workDir := getExeDir()
	workDirGlobal = workDir

	initAdbPath(workDir)

	hookLocal := filepath.Join(workDir, "ui_text_hook.js")
	transPath := filepath.Join(workDir, "trans.json")

	mustExist(hookLocal)

	loadTransMap(transPath)
	startTransAutoSave()

	if !guiMode {
		clearConsole()
		if spawnMode {
			fmt.Println("[模式] Spawn（游戏由 frida 拉起，早于反作弊初始化）")
		} else {
			fmt.Println("[模式] Attach（附加到已运行的游戏进程）")
			fmt.Println("提示: 如持续崩溃可改用 spawn 模式: start_fanyi.exe --spawn")
		}
	}

	// GUI 模式：TUI 先启动，frida 就绪检查由 attach/spawn 循环内部负责。
	// 控制台模式：保留原有预检，快速失败提示。
	if !guiMode && !checkFridaReady() {
		fmt.Println("Frida 未就绪")
		return
	}

	clearLogcat()

	if guiMode {
		p := tea.NewProgram(newPanelModel(), tea.WithAltScreen())
		tuiProgram = p
		go func() {
			if spawnMode {
				runSpawnLoop(hookLocal)
			} else {
				runAttachLoop(hookLocal)
			}
		}()
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI 错误: %v\n", err)
		}
		return
	}

	if spawnMode {
		runSpawnLoop(hookLocal)
	} else {
		runAttachLoop(hookLocal)
	}
}

func runSpawnLoop(hookLocal string) {
	for {
		clearConsole()
		if pid := getPid(); pid != "" {
			fmt.Println("[Spawn] 清理残留进程 PID:", pid)
			adbShell("su", "-c", "kill -9 "+pid)
			time.Sleep(1 * time.Second)
		}
		fmt.Println("[Spawn] 正在通过 frida 启动游戏并注入钩子...")
		injectSpawn(hookLocal)
		fmt.Println("[Spawn] 会话结束，3 秒后重新启动...")
		time.Sleep(3 * time.Second)
	}
}

func runAttachLoop(hookLocal string) {
	fmt.Println("等待游戏进程...")
	lastInjectedPid := ""

	for {
		pid := getPid()

		if pid == "" {
			if lastInjectedPid != "" {
				captureCrashLog("监控循环发现游戏进程消失")
				cleanupResiduals()
			}
			lastInjectedPid = ""
			clearConsole()
			fmt.Println("等待游戏进程...")
			time.Sleep(3 * time.Second)
			continue
		}

		if pid == lastInjectedPid {
			time.Sleep(2 * time.Second)
			continue
		}

		clearLogcat()
		clearConsole()

		if waitForAntiCheat(pid) {
			captureCrashLog("等待反作弊期间游戏进程消失")
			cleanupResiduals()
			continue
		}

		currentPid := getPid()
		if currentPid == "" {
			captureCrashLog("等待主 Hook 前游戏进程消失")
			cleanupResiduals()
			continue
		}
		if currentPid != pid {
			continue
		}

		ensureFridaServer()

		if injectMainHook(pid, hookLocal) {
			lastInjectedPid = pid
		} else {
			cleanupResiduals()
		}
		time.Sleep(3 * time.Second)
	}
}

// waitForAntiCheat 等待反作弊完成启动阶段扫描，返回 true 表示等待中游戏进程消失。
func waitForAntiCheat(pid string) (aborted bool) {
	for i := 25; i > 0; i-- {
		fmt.Printf("\r等待反作弊初始化完成，%d 秒后注入...   ", i)
		time.Sleep(1 * time.Second)
		if getPid() != pid {
			fmt.Println()
			return true
		}
	}
	fmt.Println()
	return false
}
