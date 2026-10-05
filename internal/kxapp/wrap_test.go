package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// TestItemRowWidthMatchesDrawPath 断言"滚动用的行数"与"绘制用的宽度"同源。
//
// 这是踩过的坑：滚动按一个宽度算、绘制按另一个（漏算续行缩进）算，
// 于是长条目算少一行，光标跑到可视区外。现在两边都走
// itemRowWidth / textAreaWidth，这条测试守住这个约定。
func TestItemRowWidthMatchesDrawPath(t *testing.T) {
	for w := 0; w <= 40; w++ {
		want := w - markerWidth - listIndentWidth
		if got := itemRowWidth(geometry.Rect{W: w}); got != want {
			t.Errorf("内容区 %d 列：itemRowWidth=%d，应为 %d", w, got, want)
		}
	}
	// 极窄时不能为负（负数会让 canvas.Wrap 直接返回空行，列表整块消失）。
	if got := itemRowWidth(geometry.Rect{W: 1}); got >= 1 {
		t.Errorf("1 列宽时正文宽度应为非正数（表示放不下），实际 %d", got)
	}
}

// renderInRect 在给定内容矩形里渲染一次，返回逐行文本。
//
// 用真实画布而不是字符串拼装：显示宽度、裁剪、宽字符右半格这些
// 正是要测的东西，手工拼字符串会把这些细节掩盖掉。
func renderInRect(t *testing.T, w, h int, fn func(plugin.RenderCtx)) []string {
	t.Helper()
	c := canvas.New(w, h)
	r := geometry.Rect{X: 0, Y: 0, W: w, H: h}
	restore := c.PushClip(r)
	defer restore()
	fn(plugin.RenderCtx{Canvas: c, Rect: r, Frame: r})
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = c.RowString(y)
	}
	return rows
}

// TestDrawWrappedInsetWrapsToRect 直接验证折行辅助的行为。
//
// 断言的是**受控输入**下的输出：给定 16 列、前缀 2 列、缩进 2 列，
// 正文可用 12 列，每个汉字 2 列 → 每行 6 个汉字。
// 文本长度**由宽度推导**（不再手数字数——本文件写过三次长度断言，
// 三次都数错了，这正是"测试里的魔法数字"最典型的下场）。
func TestDrawWrappedInsetWrapsToRect(t *testing.T) {
	const canvasW = 16
	const prefix, indent = "▸ ", "  "
	rowW := textAreaWidth(canvasW, canvas.StringWidth(prefix), canvas.StringWidth(indent))
	perLine := rowW / 2 // 汉字占 2 列
	if perLine < 2 {
		t.Fatalf("每行只放得下 %d 个汉字，用例无意义", perLine)
	}
	// 取 5.5 行的量：既保证整行都是满的，又保证最后一行不满。
	text := strings.Repeat("中", perLine*5+3)
	wantRows := 6

	const rows = 8
	out := renderInRect(t, canvasW, rows, func(ctx plugin.RenderCtx) {
		drawWrappedInset(ctx, 0, ctx.Rect, prefix, indent, text, 1)
	})
	// 只保留真正写了内容的前几行：画布总行数比使用的行数多。
	got := usedRows(trimTrailing(out))

	// 每行都不应超过 canvasW 列。
	for i, l := range got {
		if w := canvas.StringWidth(l); w > canvasW {
			t.Errorf("第 %d 行宽 %d 超过 %d：%q", i, w, canvasW, l)
		}
	}
	// 拼起来（去掉前缀与缩进）应当就是原文——这是"没有丢字"的断言。
	var joined strings.Builder
	for i, l := range got {
		body := l
		if i == 0 {
			body = strings.TrimPrefix(l, prefix)
		} else {
			body = strings.TrimPrefix(l, indent)
		}
		joined.WriteString(body)
	}
	if joined.String() != text {
		t.Errorf("折行后拼接应还原原文\n得到：%q\n原文：%q\n各行：%v", joined.String(), text, got)
	}
	if len(got) != wantRows {
		t.Errorf("%d 个汉字在 %d 列（%d 字/行）下应折成 %d 行，实际 %d 行：%v",
			len([]rune(text)), rowW, perLine, wantRows, len(got), got)
	}
	// 除最后一行外，每行正文都应折满。
	for i, l := range got[:len(got)-1] {
		body := strings.TrimPrefix(strings.TrimPrefix(l, prefix), indent)
		if n := len([]rune(body)); n != perLine {
			t.Errorf("第 %d 行正文应有 %d 个汉字（折满），实际 %d：%q", i, perLine, n, l)
		}
	}
}

// usedRows 去掉尾部全空的行。
func usedRows(rows []string) []string {
	last := -1
	for i, r := range rows {
		if strings.TrimSpace(r) != "" {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return rows[:last+1]
}

// TestDrawWrappedInsetNeverExceedsWidth 在多种宽度下断言不越界。
func TestDrawWrappedInsetNeverExceedsWidth(t *testing.T) {
	text := "一段比较长的中文说明文字用来验证折行宽度约束是否成立"
	for w := 4; w <= 40; w++ {
		for _, prefix := range []string{"", "▸ ", "0123456789 "} {
			rows := renderInRect(t, w, 8, func(ctx plugin.RenderCtx) {
				drawWrappedInset(ctx, 0, ctx.Rect, prefix, "  ", text, 1)
			})
			for i, l := range trimTrailing(rows) {
				if got := canvas.StringWidth(l); got > w {
					t.Errorf("宽 %d 前缀 %q：第 %d 行宽 %d 越界：%q", w, prefix, i, got, l)
				}
			}
		}
	}
}

func trimTrailing(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, trimRightSpaces(r))
	}
	return out
}

func trimRightSpaces(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == ' ' {
		i--
	}
	return s[:i]
}
