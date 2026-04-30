package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── 全局 TUI 状态 ─────────────────────────────────────────────────────────

var (
	tuiProgram *tea.Program
	guiMode    bool
)

// ── 消息类型（由后台 goroutine 发送） ─────────────────────────────────────

type msgCapture struct {
	jp         string
	zh         string
	translated bool
}

type msgStatus struct {
	pid         string
	fridaOnline bool
	phase       string
}

type msgOCR struct {
	text   string
	errMsg string
}

type tickMsg time.Time

// ── Lipgloss 样式（Dark OLED Luxury · 明日方舟配色） ─────────────────────

var (
	clrAccent = lipgloss.Color("#00e5a0") // 明日方舟绿
	clrPurple = lipgloss.Color("#9b8cfc") // 紫色标签
	clrDim    = lipgloss.Color("#2e2e2e") // 深暗灰
	clrMid    = lipgloss.Color("#5e5e5e") // 中灰
	clrText   = lipgloss.Color("#d0d0d0") // 正文
	clrWarn   = lipgloss.Color("#f59e0b") // 琥珀警告
	clrErr    = lipgloss.Color("#ef4444") // 红色错误

	sAccent = lipgloss.NewStyle().Foreground(clrAccent)
	sPurple = lipgloss.NewStyle().Foreground(clrPurple)
	sDim    = lipgloss.NewStyle().Foreground(clrDim)
	sMid    = lipgloss.NewStyle().Foreground(clrMid)
	sText   = lipgloss.NewStyle().Foreground(clrText)
	sWarn   = lipgloss.NewStyle().Foreground(clrWarn)
	sErr    = lipgloss.NewStyle().Foreground(clrErr)
	sLabel  = lipgloss.NewStyle().Foreground(clrPurple).Bold(true)

	sBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#1e1e1e")).
		Padding(0, 1)
)

// ── 数据模型 ──────────────────────────────────────────────────────────────

type captureEntry struct {
	jp, zh     string
	translated bool
}

type panelModel struct {
	w, h int

	pid         string
	fridaOnline bool
	phase       string

	current  captureEntry
	hasEntry bool

	history    []captureEntry
	captured   int
	translated int

	ocrText string
	ocrErr  string
	spinIdx int
}

var spinFrames = []string{"◐", "◓", "◑", "◒"}

func newPanelModel() panelModel {
	return panelModel{phase: "等待游戏进程..."}
}

func doTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m panelModel) Init() tea.Cmd { return doTick() }

func (m panelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "o":
			return m, ocrCmd()
		}
	case tea.WindowSizeMsg:
		m.w, m.h = v.Width, v.Height
	case tickMsg:
		m.spinIdx = (m.spinIdx + 1) % len(spinFrames)
		return m, doTick()
	case msgCapture:
		m.current = captureEntry{v.jp, v.zh, v.translated}
		m.hasEntry = true
		m.captured++
		if v.translated {
			m.translated++
		}
		m.history = append(m.history, m.current)
		if len(m.history) > 60 {
			m.history = m.history[1:]
		}
	case msgStatus:
		m.pid, m.fridaOnline, m.phase = v.pid, v.fridaOnline, v.phase
	case msgOCR:
		m.ocrText, m.ocrErr = v.text, v.errMsg
	}
	return m, nil
}

func (m panelModel) View() string {
	if m.w == 0 {
		return "初始化中..."
	}
	// sw = 样式宽度(.Width 参数)；外框 = sw+2 理论铺满终端
	// cw = 内容宽度 = sw - 2*padding(1+1)
	sw := m.w - 2
	cw := sw - 4

	// ── 标题栏 ──────────────────────────────────────────────────────
	titleStr := sAccent.Copy().Bold(true).Render("ARKNIGHTS") +
		sMid.Render("  日文文本汉化")

	dot := sDim.Render("●")
	pidStr := sDim.Render("断开")
	if m.pid != "" {
		dot = sAccent.Render("●")
		pidStr = sText.Render("PID " + m.pid)
	}
	fridaDot := sDim.Render("◉")
	fridaStr := sDim.Render("Frida离线")
	if m.fridaOnline {
		fridaDot = sPurple.Render("◉")
		fridaStr = sPurple.Render("Frida在线")
	}
	rightStr := dot + " " + pidStr + "  " + fridaDot + " " + fridaStr
	gap := cw - lipgloss.Width(titleStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	headerRow := titleStr + strings.Repeat(" ", gap) + rightStr
	sep := sMid.Render(strings.Repeat("─", cw))
	headerBox := sBox.Copy().Width(sw).Render(headerRow + "\n" + sep)

	// ── 当前捕获 ──────────────────────────────────────────────────
	var capContent string
	if !m.hasEntry {
		capContent = sMid.Render("等待捕获...")
	} else {
		jpLine := sLabel.Render("JP") + "  " + sText.Render(truncateRune(m.current.jp, cw-6))
		var zhLine string
		if m.current.translated {
			zhLine = sLabel.Render("ZH") + "  " + sAccent.Copy().Bold(true).Render(truncateRune(m.current.zh, cw-6))
		} else {
			zhLine = sLabel.Render("ZH") + "  " + sWarn.Render("（未翻译）")
		}
		capContent = jpLine + "\n" + zhLine
	}
	capBox := sBox.Copy().Width(sw).Render(capContent)

	// ── 统计 + 进度条 ────────────────────────────────────────────
	var pct float64
	if m.captured > 0 {
		pct = float64(m.translated) / float64(m.captured) * 100
	}
	barStr := renderBar(pct, 14)
	statsRow := fmt.Sprintf("  捕获 %s · 已翻译 %s · %s %s   %s  %s",
		sAccent.Render(fmt.Sprintf("%d", m.captured)),
		sAccent.Render(fmt.Sprintf("%d", m.translated)),
		barStr,
		sMid.Render(fmt.Sprintf("%.0f%%", pct)),
		sMid.Render("[o]OCR"),
		sMid.Render("[q]退出"),
	)

	// ── 翻译历史 ─────────────────────────────────────────────────
	histH := m.h - 11
	if histH < 2 {
		histH = 2
	}
	start := 0
	if len(m.history) > histH {
		start = len(m.history) - histH
	}
	rows := make([]string, 0, histH)
	for i, e := range m.history[start:] {
		var row string
		if e.translated {
			c := clrAccent
			if i%2 != 0 {
				c = lipgloss.Color("#00c28a")
			}
			row = "  " + lipgloss.NewStyle().Foreground(c).Render(truncateRune(e.zh, cw-2))
		} else {
			row = "  " + sMid.Render(truncateRune(e.jp, cw-2))
		}
		rows = append(rows, row)
	}
	for len(rows) < histH {
		rows = append(rows, "")
	}
	hisBox := sBox.Copy().Width(sw).Render(
		sLabel.Render("翻译历史") + "\n" + strings.Join(rows, "\n"),
	)

	// ── 状态栏 ──────────────────────────────────────────────────
	spin := sAccent.Render(spinFrames[m.spinIdx])
	var statusBar string
	switch {
	case m.ocrErr != "":
		statusBar = "  " + sErr.Render("OCR") + " " + sMid.Render(m.ocrErr)
	case m.ocrText != "":
		statusBar = "  " + sPurple.Render("OCR→") + " " + sText.Render(m.ocrText)
	default:
		statusBar = "  " + spin + " " + sMid.Render(m.phase)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerBox, capBox, statsRow, hisBox, statusBar,
	)
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────

func truncateRune(s string, maxCols int) string {
	runes := []rune(s)
	if len(runes) <= maxCols {
		return s
	}
	return string(runes[:maxCols-1]) + "…"
}

func renderBar(pct float64, width int) string {
	n := int(pct / 100 * float64(width))
	if n > width {
		n = width
	}
	return sAccent.Render(strings.Repeat("█", n)) +
		sDim.Render(strings.Repeat("░", width-n))
}

// sendStatus 从后台 goroutine 向 TUI 发送状态更新；非 GUI 模式下无操作。
func sendStatus(pid string, fridaOnline bool, phase string) {
	if guiMode && tuiProgram != nil {
		tuiProgram.Send(msgStatus{pid, fridaOnline, phase})
	}
}

// displayCapture 替代原 displayTextOnly，在 GUI 模式下发送到 TUI，
// 在控制台模式下维持原有行为。
func displayCapture(jp, zh string, translated bool) {
	if guiMode && tuiProgram != nil {
		tuiProgram.Send(msgCapture{jp, zh, translated})
		return
	}
	if translated {
		displayTextOnly(zh)
	} else {
		displayTextOnly(jp)
	}
}
