package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

var (
	stdinMu      sync.Mutex
	currentStdin io.WriteCloser
)

// runFridaCmd 统一处理 frida 子进程的管道、输出流、超时和崩溃捕获。
// onLine 接收每一行输出，由调用方决定如何处理（attach/spawn 行为不同）。
func runFridaCmd(cmd *exec.Cmd, onLine func(string), label string) {
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
		fmt.Println("[ERROR] frida 启动失败:", err)
		return
	}

	stdinMu.Lock()
	if currentStdin != nil {
		_ = currentStdin.Close()
	}
	currentStdin = stdin
	stdinMu.Unlock()

	scanPipe := func(r io.Reader) {
		s := bufio.NewScanner(r)
		buf := make([]byte, scannerMaxBuf)
		s.Buffer(buf, scannerMaxBuf)
		for s.Scan() {
			onLine(s.Text())
		}
		if err := s.Err(); err != nil {
			fmt.Printf("[WARN] scanner error (%s): %v\n", label, err)
		}
	}

	go scanPipe(stdout)
	go scanPipe(stderr)

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
		fmt.Printf("[WARN] %s 超时，强制终止\n", label)
		_ = cmd.Process.Kill()
		<-done
	}

	if getPid() == "" {
		captureCrashLog(label + " 结束后游戏进程消失")
	}
}

func injectAttach(pid, hookPath string) {
	cmd := exec.Command("frida", "-H", "127.0.0.1:"+fridaPort, "-p", pid, "-l", hookPath)
	runFridaCmd(cmd, handleLine, "attach")
}

func injectSpawn(hookPath string) {
	cmd := exec.Command("frida", "-H", "127.0.0.1:"+fridaPort, "-f", packageName, "-l", hookPath)
	runFridaCmd(cmd, func(line string) {
		if !handleUnicodeLine(line) {
			fmt.Println("[frida]", line)
		}
	}, "spawn")
}

// injectMainHook 在确认 PID 未变后执行 attach 注入，返回注入结束时游戏是否存活。
func injectMainHook(pid, hookPath string) bool {
	if getPid() != pid {
		return false
	}
	injectAttach(pid, hookPath)
	return getPid() == pid
}
