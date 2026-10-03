package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderPreviewsForReview 把各界面渲染成纯文本，便于人工检查排版。
// 默认跳过，只在 KAIR_PREVIEW=1 时运行。
func TestRenderPreviewsForReview(t *testing.T) {
	if os.Getenv("KAIR_PREVIEW") == "" {
		t.Skip("需要设置 KAIR_PREVIEW=1 才渲染预览")
	}
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)

	write := func(name string, app *App) {
		plain := ansi.ReplaceAllString(app.View(), "")
		if err := os.WriteFile(name, []byte(plain), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 各分辨率下的尺寸核对表。
	sizes := [][2]int{{80, 24}, {100, 30}, {120, 36}, {140, 40}, {160, 44}}
	var report strings.Builder
	for _, s := range sizes {
		w, h := s[0], s[1]
		for _, view := range []struct {
			name string
			set  func(*App)
		}{
			{"dashboard", func(a *App) { a.view = ViewDashboard }},
			{"help", func(a *App) { a.view = ViewHelp }},
			{"settings", func(a *App) { a.view = ViewSettings }},
			{"timer", func(a *App) { a.startTimer() }},
			{"history", func(a *App) { a.view = ViewHistory }},
		} {
			app, s2, _ := newTestApp(t, at)
			app.width, app.height = w, h
			seedForViews(t, app, s2, at)
			view.set(app)
			out := ansi.ReplaceAllString(app.View(), "")
			lines := strings.Split(out, "\n")
			maxW := 0
			for _, l := range lines {
				if lw := lipgloss.Width(l); lw > maxW {
					maxW = lw
				}
			}
			flag := ""
			if maxW > w {
				flag += " 宽度超限!"
			}
			if len(lines) > h {
				flag += " 高度超限!"
			}
			fmt.Fprintf(&report, "%-10s %3dx%-3d -> %3dx%-3d%s\n", view.name, w, h, maxW, len(lines), flag)
			if w == 100 && h == 30 {
				write("preview-"+view.name+"-100x30.txt", app)
			}
		}
	}
	fmt.Print(report.String())
	if err := os.WriteFile("preview-report.txt", []byte(report.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
