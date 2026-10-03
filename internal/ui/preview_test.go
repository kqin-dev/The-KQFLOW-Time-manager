package ui

import (
	"os"
	"regexp"
	"testing"
	"time"
)

// TestRenderPreviewsForReview 把几个界面渲染成纯文本，便于人工检查排版。
// 默认跳过，只在本机检查排版时用 -run 显式打开。
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

	app, _, _ := newTestApp(t, at)
	app.openCustom()
	write("preview-custom.txt", app)

	// 故意用较窄的终端，检查编辑器是否会溢出。
	app.width, app.height = 80, 26
	write("preview-custom-narrow.txt", app)
}
