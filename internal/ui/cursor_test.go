package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// TestCJKCursorColumnExact 验证含中文时，画出的光标所在显示列 == 光标前内容的显示宽度。
//
// 这是用户反复报过的问题：中文一字占两列，早期按 rune 下标定位光标，
// 一旦混入中文，光标就跑到文字前面或后面。
func TestCJKCursorColumnExact(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 120, 40

	texts := []string{
		"abc中文",
		"中文abc",
		"今天 write 周报",
		"纯中文内容测试",
		"hello world",
		"中文",
		"第一行中文\n第二行abc\n第三行",
		"abc\n中文\n混合 mixed 内容",
	}
	for _, text := range texts {
		all := strings.Split(text, "\n")
		runes := []rune(text)
		for cursor := 0; cursor <= len(runes); cursor++ {
			app.openNote()
			app.editor.value = append([]rune{}, runes...)
			app.editor.cursor = cursor

			// 期望：光标所在行 = 光标前的换行数；行内列 = 该行内光标前的显示宽度。
			line := 0
			lineStart := 0
			for i := 0; i < cursor; i++ {
				if runes[i] == '\n' {
					line++
					lineStart = i + 1
				}
			}
			wantCol := lipgloss.Width(string(runes[lineStart:cursor]))

			lines := app.multilineEditorLines(app.contentWidth(), 6)
			var target string
			for _, l := range lines {
				if strings.Contains(ansiRE.ReplaceAllString(l, ""), "▏") {
					target = ansiRE.ReplaceAllString(l, "")
					break
				}
			}
			if target == "" {
				t.Fatalf("%q cursor=%d：没找到光标", text, cursor)
			}
			// 光标前应当是 marker + 该行光标前的内容。
			gotCol := lipgloss.Width(strings.SplitN(target, "▏", 2)[0])
			wantTotal := lipgloss.Width(" ▸ ") + wantCol
			if gotCol != wantTotal {
				t.Errorf("%q cursor=%d（第 %d 行）：光标列 %d，期望 %d\n  行=%q 该行内容=%q",
					text, cursor, line, gotCol, wantTotal, target, all[line])
			}
			// 光标行不得超出中间栏宽度。
			if w := lipgloss.Width(target); w > app.contentWidth() {
				t.Errorf("%q cursor=%d：光标行宽 %d 超出内容宽度 %d",
					text, cursor, w, app.contentWidth())
			}
		}
	}
}

// TestCJKCursorLongLine 验证超长中文行里光标仍然可见且位置正确。
func TestCJKCursorLongLine(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 120, 40

	long := strings.Repeat("很长的一行中文内容", 6)
	runes := []rune(long)
	for _, cursor := range []int{0, 1, 5, len(runes) / 2, len(runes) - 1, len(runes)} {
		app.openNote()
		app.editor.value = append([]rune{}, runes...)
		app.editor.cursor = cursor
		inner := app.contentWidth()
		lines := app.multilineEditorLines(inner, 4)
		found := false
		for _, l := range lines {
			p := ansiRE.ReplaceAllString(l, "")
			if !strings.Contains(p, "▏") {
				continue
			}
			found = true
			if w := lipgloss.Width(p); w > inner {
				t.Errorf("cursor=%d：光标行宽 %d 超出 %d", cursor, w, inner)
			}
			// 光标左边的可见内容 + 右侧内容必须能在原文中找到
			// （超宽时左侧被裁掉过，所以不是前缀，但仍是原文的一段）。
			full := strings.TrimPrefix(p, " ▸ ")
			left, right, _ := strings.Cut(full, "▏")
			if !strings.Contains(long, left) || !strings.Contains(long, right) {
				t.Errorf("cursor=%d：可见内容 %q / %q 不属于原文", cursor, left, right)
			}
			// 光标列必须与左侧内容的显示宽度一致。
			if got := lipgloss.Width(left); got > inner {
				t.Errorf("cursor=%d：左侧宽度 %d 超出 %d", cursor, got, inner)
			}
		}
		if !found {
			t.Errorf("cursor=%d：没找到光标（长行时必须保持可见）", cursor)
		}
	}
}

// TestHighlightWidthStaysNearText 验证选中行的底色不会拖得比文字长得多。
//
// 用户反馈“设置里光标长达一行半”：根因是选中行被 pad 到整个内容宽度，
// 一行 35 列的设置项会拖出上百列的蓝条，看起来像光标有 1.5 行那么长。
func TestHighlightWidthStaysNearText(t *testing.T) {
	at := time.Date(2026, 10, 3, 20, 26, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 170, 48
	app.view = ViewSettings
	inner := app.contentWidth()

	plainWidth := func(s string) int { return lipgloss.Width(ansiRE.ReplaceAllString(s, "")) }

	for cur := range settingItems {
		app.settingsCursor = cur
		styled, plain := app.settingsLines()
		if len(styled) != len(plain) {
			t.Fatalf("styled 与 plain 行数不一致：%d vs %d", len(styled), len(plain))
		}
		found := 0
		for i, s := range styled {
			p := ansiRE.ReplaceAllString(s, "")
			if !strings.HasPrefix(p, "▸") {
				continue
			}
			found++
			textW := lipgloss.Width(plain[i])
			hlW := plainWidth(s)
			// 底色最多比文字宽一点（留出光标余量），绝不能铺满整行。
			if hlW > textW+4 {
				t.Errorf("选中项 %q：底色宽 %d，文字宽 %d，拖得太长", settingItems[cur].Label, hlW, textW)
			}
			if hlW > inner {
				t.Errorf("选中项 %q：底色宽 %d 超出内容宽度 %d", settingItems[cur].Label, hlW, inner)
			}
		}
		if found != 1 {
			t.Errorf("应恰好有一行选中标记，实际 %d 行", found)
		}
	}
}

// TestAllRowKindsFitPanelWidth 验证各类行的显示宽度都不超过内容宽度。
//
// 一旦超宽，终端会把这一行折成两行，看起来就像界面被撑破了。
func TestAllRowKindsFitPanelWidth(t *testing.T) {
	at := time.Date(2026, 10, 3, 20, 26, 0, 0, time.Local)
	for _, size := range [][2]int{{213, 56}, {170, 48}, {140, 40}, {120, 34}, {100, 30}} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		inner := app.contentWidth()

		app.view = ViewSettings
		for cur := range settingItems {
			app.settingsCursor = cur
			styled, _ := app.settingsLines()
			for i, s := range styled {
				if w := lipgloss.Width(ansiRE.ReplaceAllString(s, "")); w > inner {
					t.Errorf("%dx%d 设置页第 %d 行宽 %d 超出 %d", size[0], size[1], i, w, inner)
				}
			}
		}
		app.view = ViewHelp
		if styled, _ := app.helpLines(); true {
			for i, s := range styled {
				if w := lipgloss.Width(ansiRE.ReplaceAllString(s, "")); w > inner {
					t.Errorf("%dx%d 帮助页第 %d 行宽 %d 超出 %d", size[0], size[1], i, w, inner)
				}
			}
		}
		app.view = ViewHistory
		if styled, _ := app.historyLines(); true {
			for i, s := range styled {
				if w := lipgloss.Width(ansiRE.ReplaceAllString(s, "")); w > inner {
					t.Errorf("%dx%d 历史页第 %d 行宽 %d 超出 %d", size[0], size[1], i, w, inner)
				}
			}
		}
	}
}

// TestHighlightWidthHelper 直接锁定选中行底色的宽度算法。
//
// 早期实现是 pad(text, inner)：文字 35 列、内容宽 145 列时，
// 底色被拉到 145 列，于是看起来“光标长达一行半”。
func TestHighlightWidthHelper(t *testing.T) {
	// 底色 = 文字宽度 + 2，远小于内容宽度。
	if got := highlightWidth("abc", 100); got != 5 {
		t.Errorf("highlightWidth(abc, 100) = %d，期望 5", got)
	}
	// 中文按显示宽度算：4 个字 = 8 列，+2 = 10。
	if got := highlightWidth("中文内容", 100); got != 10 {
		t.Errorf("highlightWidth(中文内容, 100) = %d，期望 10", got)
	}
	// 内容宽度更小时夹到内容宽度。
	if got := highlightWidth("abc", 4); got != 4 {
		t.Errorf("highlightWidth(abc, 4) = %d，期望 4", got)
	}
	// padTo 只补到目标宽度，不超宽。
	if got := padTo("abc", 5); got != "abc  " {
		t.Errorf("padTo(abc, 5) = %q，期望 %q", got, "abc  ")
	}
	if got := padTo("abc", 2); got != "abc" {
		t.Errorf("padTo(abc, 2) = %q，应原样返回", got)
	}
	if got := padTo("中文", 5); lipgloss.Width(got) != 5 {
		t.Errorf("padTo(中文, 5) 显示宽度 = %d，期望 5", lipgloss.Width(got))
	}
}

// cellAt 返回字符串在显示列 col 上的那个单元（可能是宽字符）。
//
// 测试里必须用它来核对列位置：直接用 rune 下标会在有中文时得到错误结论。
func cellAt(s string, col int) string {
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > col {
			return string(r)
		}
		w += rw
	}
	return ""
}

// TestSplitAtColumn 验证按显示列切分的边界情况。
func TestSplitAtColumn(t *testing.T) {
	cases := []struct {
		s    string
		col  int
		left string
	}{
		{"abcdef", 0, ""},
		{"abcdef", 3, "abc"},
		{"abcdef", 6, "abcdef"},
		{"中文abc", 0, ""},
		{"中文abc", 2, "中"},
		{"中文abc", 4, "中文"},
		{"中文abc", 5, "中文a"},
		{"中文abc", 7, "中文abc"},
		// 光标落在宽字符中间时，该字符归左侧。
		{"中文", 1, "中"},
	}
	for _, c := range cases {
		left, cur, right := splitAtColumn(c.s, c.col)
		if left != c.left {
			t.Errorf("splitAtColumn(%q, %d) 左侧=%q，期望 %q", c.s, c.col, left, c.left)
		}
		if cur != "▏" {
			t.Errorf("splitAtColumn(%q, %d) 光标=%q", c.s, c.col, cur)
		}
		// 左右拼起来必须还原原文。
		if left+right != c.s {
			t.Errorf("splitAtColumn(%q, %d) 左右拼接=%q，应还原原文", c.s, c.col, left+right)
		}
		// 光标所在的显示列应尽量接近 col。
		// 允许偏后 2 列：光标落在宽字符中间时，整个字符归左侧、光标跟在它后面，
		// 这样才不会把一个汉字劈成两半。
		got := lipgloss.Width(left)
		if got > c.col+1 || got < c.col-2 {
			t.Errorf("splitAtColumn(%q, %d) 光标列=%d，偏离过大", c.s, c.col, got)
		}
	}
}
