package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

var stdinReader = bufio.NewReader(os.Stdin)

// skipMenuNewline tracks the Enter key that may follow a numeric shortcut.
// Numeric shortcuts confirm immediately, so a subsequent nested menu must not
// interpret that same Enter as its own selection.
var skipMenuNewline bool

// selectMenu 交互式上下键选择菜单，返回选中索引；-1 表示取消。
// 支持：↑↓/jk 选择、Enter 确认、q/Ctrl+C 退出、数字键直选。
func selectMenu(options []string, prompt string) int {
	// 空选项列表防护：避免调用方对空列表索引导致 panic
	if len(options) == 0 {
		return -1
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// 管道/重定向输入必须按行读取，避免数字菜单留下换行影响嵌套菜单。
		return fallbackSelect(options, prompt)
	}
	old, err := makeRaw(fd)
	if err != nil {
		// 非 TTY 或失败时退化为序号输入
		return fallbackSelect(options, prompt)
	}
	defer restoreTerm(fd, old)

	// 兜底：若 SIGINT/SIGTERM 来自外部（如 kill -INT），确保恢复终端后再退出
	// 使用 done channel 防止 goroutine 泄漏：selectMenu 返回时关闭 done，goroutine 随即退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigCh:
			restoreTerm(fd, old)
			os.Exit(130)
		case <-done:
		}
	}()
	defer signal.Stop(sigCh)

	m := &menu{out: os.Stdout}
	m.render(options, 0, prompt)

	selected := 0
	for {
		b, err := stdinReader.ReadByte()
		if err != nil {
			return -1
		}
		if skipMenuNewline {
			if b == '\r' || b == '\n' {
				continue
			}
			skipMenuNewline = false
		}
		switch b {
		case 0x1b: // ESC 序列
			b2, err := stdinReader.ReadByte()
			if err != nil {
				return -1
			}
			if b2 == '[' {
				b3, _ := stdinReader.ReadByte()
				switch b3 {
				case 'A': // ↑
					if selected > 0 {
						selected--
						m.render(options, selected, prompt)
					}
				case 'B': // ↓
					if selected < len(options)-1 {
						selected++
						m.render(options, selected, prompt)
					}
				}
			}
		case '\r', '\n': // Enter 确认
			fmt.Fprintln(os.Stdout)
			return selected
		case 3, 'q', 'Q': // Ctrl+C / q 退出
			fmt.Fprintln(os.Stdout)
			return -1
		case 'k', 'w': // vim 风格上移
			if selected > 0 {
				selected--
				m.render(options, selected, prompt)
			}
		case 'j', 's': // vim 风格下移
			if selected < len(options)-1 {
				selected++
				m.render(options, selected, prompt)
			}
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			idx := int(b - '1')
			if idx < len(options) {
				skipMenuNewline = true
				fmt.Fprintln(os.Stdout)
				return idx
			}
		}
	}
}

// menu 跟踪上次渲染行数以实现原地重绘。
type menu struct {
	lastLines int
	out       *os.File
}

func (m *menu) render(options []string, selected int, prompt string) {
	// 上移到菜单起始处，并清除该行及以下所有内容
	if m.lastLines > 0 {
		fmt.Fprintf(m.out, "\r\033[%dA\033[J", m.lastLines)
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\r\n")
	for i, opt := range options {
		if i == selected {
			sb.WriteString(fmt.Sprintf("  \033[36m❯\033[0m \033[1m%s\033[0m", opt))
		} else {
			sb.WriteString(fmt.Sprintf("    %s", opt))
		}
		sb.WriteString("\033[K\r\n")
	}
	fmt.Fprint(m.out, sb.String())
	m.lastLines = len(options) + 1
}

// fallbackSelect 非 TTY 环境下的序号选择。
func fallbackSelect(options []string, prompt string) int {
	fmt.Println(prompt)
	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt)
	}
	fmt.Print("请输入序号: ")
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(line)
	var n int
	fmt.Sscanf(line, "%d", &n)
	if n >= 1 && n <= len(options) {
		return n - 1
	}
	return -1
}

// prompt 从标准输入读取一行（去空白）。
func prompt(label string) string {
	fmt.Print(label)
	s, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(s)
}

// ──────────────────── termios 原始模式（跨平台 golang.org/x/term） ────────────────────

func makeRaw(fd int) (*term.State, error) {
	return term.MakeRaw(fd)
}

func restoreTerm(fd int, state *term.State) {
	if state != nil {
		_ = term.Restore(fd, state)
	}
}
