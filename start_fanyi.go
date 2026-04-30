package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

var spawnMode = false

const (
	packageName = "com.YoStarJP.Arknights"

	scannerMaxBuf    = 4 * 1024 * 1024
	autoSaveInterval = 10 * time.Second
	injectTimeout    = 5 * time.Minute
)

var unicodeLine = regexp.MustCompile(`^\[unicode\]\s+(.+)$`)
var unicodeUnit = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)
var htmlTag = regexp.MustCompile(`<[^>]+>`)

var rePureNumber = regexp.MustCompile(`^\d+$`)
var reSlashNumber = regexp.MustCompile(`^/\d+$`)
var reNumberPair = regexp.MustCompile(`^\d+/\d+$`)
var rePercent = regexp.MustCompile(`^\d{1,3}%$`)
var reTimeText = regexp.MustCompile(`^\d+[日時間分秒]+$`)
var reEnglishOnly = regexp.MustCompile(`^[A-Za-z0-9_./:\- ]+$`)
var reNumberSymbolOnly = regexp.MustCompile(`^[0-9\s\/:%.\-+]+$`)
var reJapaneseOrHan = regexp.MustCompile(`[\p{Han}\x{3040}-\x{30ff}]`)

var transMap = map[string]string{}
var transFile = ""
var transDirty = false
var workDirGlobal = ""
var adbExe = "adb"

var transMu sync.Mutex
var stdinMu sync.Mutex
var currentStdin io.WriteCloser

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

func clearConsole() {
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
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

func decodeUnicodeEscaped(s string) string {
	matches := unicodeUnit.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return ""
	}
	units := make([]uint16, 0, len(matches))
	for _, m := range matches {
		var v uint16
		if _, err := fmt.Sscanf(m[1], "%04x", &v); err == nil {
			units = append(units, v)
		}
	}
	return string(utf16.Decode(units))
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

func stripRichText(text string) string {
	return strings.TrimSpace(htmlTag.ReplaceAllString(text, ""))
}

func displayTextOnly(text string) {
	text = normalizeText(stripRichText(text))
	if text == "" {
		return
	}
	clearConsole()
	fmt.Println(text)
}

func shouldSaveUntranslated(text string) bool {
	text = normalizeText(text)
	if text == "" {
		return false
	}
	plain := stripRichText(text)
	if plain == "" {
		return false
	}
	if rePureNumber.MatchString(plain) {
		return false
	}
	if reSlashNumber.MatchString(plain) {
		return false
	}
	if reNumberPair.MatchString(plain) {
		return false
	}
	if rePercent.MatchString(plain) {
		return false
	}
	if reTimeText.MatchString(plain) {
		return false
	}
	if reEnglishOnly.MatchString(plain) {
		return false
	}
	if reNumberSymbolOnly.MatchString(plain) {
		return false
	}
	if !reJapaneseOrHan.MatchString(plain) {
		return false
	}
	return true
}

func loadTransMap(path string) {
	transMu.Lock()
	defer transMu.Unlock()

	transFile = path
	data, err := os.ReadFile(path)
	if err != nil {
		transMap = map[string]string{}
		transDirty = true
		saveTransMapLocked()
		return
	}
	if strings.TrimSpace(string(data)) == "" {
		transMap = map[string]string{}
		transDirty = true
		saveTransMapLocked()
		return
	}
	if err := json.Unmarshal(data, &transMap); err != nil {
		fmt.Println("[WARN] trans.json 解析失败，不会覆盖原文件:", err)
		transMap = map[string]string{}
	}
}

func saveTransMapLocked() {
	if transFile == "" {
		return
	}
	data, err := json.MarshalIndent(transMap, "", "  ")
	if err != nil {
		return
	}
	tmpFile := transFile + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return
	}
	_ = f.Sync()
	_ = f.Close()
	if err := os.Rename(tmpFile, transFile); err != nil {
		return
	}
	transDirty = false
}

func startTransAutoSave() {
	go func() {
		ticker := time.NewTicker(autoSaveInterval)
		defer ticker.Stop()
		for range ticker.C {
			transMu.Lock()
			if transDirty {
				saveTransMapLocked()
			}
			transMu.Unlock()
		}
	}()
}

// ★ 修复：移除了遗留的 saveTransMapLocked() 实时写盘调用
//
//	只标记 dirty，由 autoSave goroutine 统一落盘
func addUntranslatedToTrans(text string) {
	text = normalizeText(text)
	if text == "" || !shouldSaveUntranslated(text) {
		return
	}
	plain := stripRichText(text)

	transMu.Lock()
	defer transMu.Unlock()

	if _, ok := transMap[text]; ok {
		return
	}
	if _, ok := transMap[plain]; ok {
		return
	}

	transMap[text] = ""
	transDirty = true // 仅标记，不在热路径上做 IO
}

func localTranslate(text string) string {
	text = normalizeText(text)
	if text == "" {
		return ""
	}
	plain := stripRichText(text)

	transMu.Lock()
	defer transMu.Unlock()

	if v, ok := transMap[text]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := transMap[plain]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}

	out := text
	changed := false
	for jp, zh := range transMap {
		jp = normalizeText(jp)
		zh = strings.TrimSpace(zh)
		if jp == "" || zh == "" {
			continue
		}
		if strings.Contains(out, jp) {
			out = strings.ReplaceAll(out, jp, zh)
			changed = true
		}
	}
	if changed {
		return normalizeText(stripRichText(out))
	}
	return ""
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

func captureCrashLog(reason string) {
	if workDirGlobal == "" {
		workDirGlobal = getExeDir()
	}
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

func handleUnicodeLine(line string) bool {
	m := unicodeLine.FindStringSubmatch(line)
	if len(m) != 2 {
		return false
	}
	decoded := normalizeText(decodeUnicodeEscaped(m[1]))
	if decoded == "" {
		return true
	}
	addUntranslatedToTrans(decoded)
	if zh := localTranslate(decoded); zh != "" {
		displayTextOnly(zh)
	} else {
		displayTextOnly(decoded)
	}
	return true
}

func handleLine(line string) {
	if handleUnicodeLine(line) {
		return
	}
	if strings.Contains(line, "Failed to attach") ||
		strings.Contains(line, "Unable to find process") ||
		strings.Contains(line, "unable to connect") ||
		strings.Contains(line, "[ERROR]") {
		fmt.Println(line)
	}
}

func checkFridaReady() bool {
	out := run("frida-ps", "-U")
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

func injectScriptStandard(pid string, localScript string, label string) {
	cmd := exec.Command("frida", "-U", "-p", pid, "-l", localScript)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println("[ERROR] 获取 stdin 失败:", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("[ERROR] 获取 stdout 失败:", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Println("[ERROR] 获取 stderr 失败:", err)
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("[ERROR] 注入启动失败:", err)
		return
	}

	stdinMu.Lock()
	if currentStdin != nil {
		_ = currentStdin.Close()
	}
	currentStdin = stdin
	stdinMu.Unlock()

	scanStream := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		buf := make([]byte, scannerMaxBuf)
		scanner.Buffer(buf, scannerMaxBuf)
		for scanner.Scan() {
			handleLine(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Printf("[WARN] scanner error (%s): %v\n", label, err)
		}
	}

	go scanStream(stdout)
	go scanStream(stderr)

	done := make(chan struct{}, 1)
	go func() {
		_ = cmd.Wait()
		stdinMu.Lock()
		if currentStdin == stdin {
			currentStdin = nil
		}
		stdinMu.Unlock()
		_ = stdin.Close()
		done <- struct{}{}
	}()

	select {
	case <-done:
		// 正常退出
	case <-time.After(injectTimeout):
		fmt.Printf("[WARN] %s 注入超时，强制终止\n", label)
		_ = cmd.Process.Kill()
		<-done
	}

	if getPid() == "" {
		captureCrashLog(label + " 注入结束后游戏进程消失")
	}
}

func injectMainHook(pid string, hookPath string) {
	if getPid() != pid {
		return
	}
	injectScriptStandard(pid, hookPath, "ui_text_hook.js")
}

// injectScriptSpawn 以 spawn 模式启动游戏：由 frida 负责拉起进程，
// 钩子在游戏任何代码跑之前就装好，可绕过运行时完整性检测。
func injectScriptSpawn(hookPath string) {
	cmd := exec.Command("frida", "-U", "-f", packageName, "-l", hookPath, "--no-pause")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println("[ERROR] spawn: 获取 stdin 失败:", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("[ERROR] spawn: 获取 stdout 失败:", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Println("[ERROR] spawn: 获取 stderr 失败:", err)
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("[ERROR] spawn: 启动失败:", err)
		return
	}

	stdinMu.Lock()
	if currentStdin != nil {
		_ = currentStdin.Close()
	}
	currentStdin = stdin
	stdinMu.Unlock()

	scanStream := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		buf := make([]byte, scannerMaxBuf)
		scanner.Buffer(buf, scannerMaxBuf)
		for scanner.Scan() {
			handleLine(scanner.Text())
		}
	}

	go scanStream(stdout)
	go scanStream(stderr)

	done := make(chan struct{}, 1)
	go func() {
		_ = cmd.Wait()
		stdinMu.Lock()
		if currentStdin == stdin {
			currentStdin = nil
		}
		stdinMu.Unlock()
		_ = stdin.Close()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(injectTimeout):
		fmt.Println("[WARN] spawn 模式超时，强制终止")
		_ = cmd.Process.Kill()
		<-done
	}

	if getPid() == "" {
		captureCrashLog("spawn 模式结束后游戏进程消失")
	}
}

func main() {
	_ = exec.Command("cmd", "/c", "chcp", "65001").Run()

	for _, arg := range os.Args[1:] {
		if arg == "--spawn" {
			spawnMode = true
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

	clearConsole()
	if spawnMode {
		fmt.Println("[模式] Spawn（游戏由 frida 拉起，早于反作弊初始化）")
	} else {
		fmt.Println("[模式] Attach（附加到已运行的游戏进程）")
		fmt.Println("提示: 如持续崩溃可改用 spawn 模式: start_fanyi.exe --spawn")
	}

	if !checkFridaReady() {
		fmt.Println("Frida 未就绪")
		return
	}

	clearLogcat()

	// ── Spawn 模式：直接由 frida 启动游戏，循环续命 ──
	if spawnMode {
		for {
			clearConsole()
			fmt.Println("[Spawn] 正在通过 frida 启动游戏并注入钩子...")
			injectScriptSpawn(hookLocal)
			fmt.Println("[Spawn] 会话结束，3 秒后重新启动...")
			time.Sleep(3 * time.Second)
		}
	}

	// ── Attach 模式（原有逻辑）──
	fmt.Println("等待游戏进程...")
	lastInjectedPid := ""

	for {
		pid := getPid()

		if pid == "" {
			if lastInjectedPid != "" {
				captureCrashLog("监控循环发现游戏进程消失")
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
		fmt.Println("正在注入文本捕获...")
		time.Sleep(3 * time.Second)

		currentPid := getPid()
		if currentPid == "" {
			captureCrashLog("等待主 Hook 前游戏进程消失")
			time.Sleep(3 * time.Second)
			continue
		}
		if currentPid != pid {
			continue
		}

		lastInjectedPid = pid
		injectMainHook(pid, hookLocal)
		time.Sleep(3 * time.Second)
	}
}
