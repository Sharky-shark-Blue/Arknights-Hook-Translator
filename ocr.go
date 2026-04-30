package main

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// OCRProvider 是 OCR 引擎的可插拔接口。
// 接入 Tesseract / Google Vision / PaddleOCR 时实现此接口即可。
type OCRProvider interface {
	Name() string
	Recognize(pngData []byte) (string, error)
}

// ocrCmd 是 bubbletea 命令：触发一次 OCR 截图 + 识别，
// 结果通过 msgOCR 消息回传给 Update。
// 目前为存根，等待接入真实 OCR 引擎。
func ocrCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := takeADBScreenshot()
		if err != nil {
			return msgOCR{errMsg: "截图失败: " + err.Error()}
		}
		if len(data) == 0 {
			return msgOCR{errMsg: "截图为空"}
		}
		// TODO: 接入真实 OCR 引擎
		_ = data
		return msgOCR{errMsg: "OCR 引擎暂未配置"}
	}
}

// takeADBScreenshot 通过 ADB 截取当前屏幕，返回 PNG 字节。
func takeADBScreenshot() ([]byte, error) {
	cmd := exec.Command(adbExe, "exec-out", "screencap", "-p")
	return cmd.Output()
}
