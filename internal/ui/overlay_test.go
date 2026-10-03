package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TestWrapBalancedKeepsLinesEven 验证字条折行不会出现“最后一行只剩一个句号”。
func TestWrapBalancedKeepsLinesEven(t *testing.T) {
	// 中文没有空格可依，贪心折行会把收尾的「。」挤到最后一行。
	text := "「时机成熟时，一切都会水到渠成。」"
	const maxW = 31
	lines := wrapBalanced(text, maxW)
	if len(lines) != 2 {
		t.Fatalf("应折成 2 行，实际 %d 行: %q", len(lines), lines)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > maxW {
			t.Errorf("行宽 %d 超过上限 %d: %q", w, maxW, l)
		}
	}
	// 两行长度应接近，不能一行很长一行只剩一两个字。
	if spread := lineSpread(lines); spread > 6 {
		t.Errorf("两行长度差 %d 过大: %q", spread, lines)
	}
	// 收尾标点必须留在最后一行。
	if !strings.HasSuffix(lines[len(lines)-1], "」") {
		t.Errorf("收尾标点应留在最后一行: %q", lines)
	}
}

// TestWrapFitsInPanel 验证每条字条在任何终端宽度下都能折进中间栏。
func TestWrapFitsInPanel(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	for _, w := range []int{60, 70, 80, 100, 120, 160} {
		app.width, app.height = w, 30
		leftW, rightW := 24, 22
		if w >= 110 {
			leftW, rightW = 34, 30
		}
		inner := w - leftW - rightW - 4
		if inner < 1 {
			continue
		}
		for i, q := range Quotes {
			app.quoteIdx = i
			for _, l := range wrapBalanced("「"+q+"」", inner) {
				if lw := lipgloss.Width(l); lw > inner {
					t.Errorf("宽度 %d：字条 %d 的行宽 %d 超过内宽 %d", w, i, lw, inner)
				}
			}
		}
	}
}

// TestOverlayAlignsWithWideCharacters 是浮层对齐的回归测试。
//
// 看板左栏含中文、中间栏的 Logo 含 U+2001 等宽字符，因此每行的“字符数”小于“显示宽度”。
// 早期 overlay 用 rune 下标裁剪，弹窗右侧会残留面板边框，看起来像界面被撕开。
func TestOverlayAlignsWithWideCharacters(t *testing.T) {
	// 基线的显示宽度为 22：中文 4 列 + 18 个制表符。
	base := strings.Repeat("─", 18) + "中文"
	const baseW = 22
	if got := lipgloss.Width(base); got != baseW {
		t.Fatalf("基线宽度应为 %d，实际 %d", baseW, got)
	}

	modal := "╔════╗\n║ 弹 ║\n╚════╝"
	modalW := 0
	for _, l := range strings.Split(modal, "\n") {
		if w := lipgloss.Width(l); w > modalW {
			modalW = w
		}
	}

	// 宽度取基线宽度，弹窗居中后两侧都应保留原始内容。
	out := overlay(base, modal, baseW, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("行数应为 3，实际 %d", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != baseW {
			t.Errorf("第 %d 行显示宽度应保持 %d，实际 %d: %q", i, baseW, w, l)
		}
	}

	// 弹窗右侧必须保留原始内容；旧实现会按 rune 下标把它截断。
	if !strings.Contains(lines[0], "╔════╗") {
		t.Fatalf("弹窗首行应完整可见，实际: %q", lines[0])
	}
	if w := lipgloss.Width(lines[0]); w != baseW {
		t.Errorf("浮层行不应改变总宽度，实际 %d: %q", w, lines[0])
	}
	startX := (baseW - modalW) / 2
	if startX <= 0 {
		t.Fatalf("测试前提不成立：弹窗应能居中留出两侧，startX=%d", startX)
	}
}

// TestOverlayPreservesRightSide 验证弹窗右侧的原始内容不会被吞掉。
func TestOverlayPreservesRightSide(t *testing.T) {
	right := "RIGHT-SIDE-MARKER"
	base := strings.Repeat("L", 30) + right // 显示宽度 47
	width := 47
	modal := "MM"

	out := overlay(base, modal, width, 1)
	line := strings.Split(out, "\n")[0]
	if !strings.Contains(line, "MM") {
		t.Fatalf("弹窗内容应出现，实际: %q", line)
	}
	if !strings.Contains(line, right) {
		t.Errorf("弹窗右侧内容应保留，实际: %q", line)
	}
	if w := lipgloss.Width(line); w != width {
		t.Errorf("行宽应保持 %d，实际 %d: %q", width, w, line)
	}
}

// TestOverlayKeepsModalIntact 验证弹窗自身内容完整可见。
func TestOverlayKeepsModalIntact(t *testing.T) {
	base := strings.Repeat("A", 40) + "\n" + strings.Repeat("B", 40)
	modal := "MODAL"
	out := overlay(base, modal, 40, 2)
	if !strings.Contains(out, "MODAL") {
		t.Errorf("浮层内容应完整可见，实际:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w != 40 {
			t.Errorf("每行显示宽度应保持 40，实际 %d: %q", w, l)
		}
	}
}

// TestOverlaySmallTerminal 验证极小尺寸下浮层不会越界或崩溃。
func TestOverlaySmallTerminal(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {3, 2}, {10, 4}} {
		out := overlay("ab\ncd", "MODAL", size[0], size[1])
		if out == "" {
			t.Errorf("尺寸 %v 时浮层渲染为空", size)
		}
	}
}

// TestSpliceCellsReplacesInPlace 验证按显示列替换的实现。
func TestSpliceCellsReplacesInPlace(t *testing.T) {
	// “中文”占 4 列，紧随其后的 AB 占第 5、6 列。
	src := "中文AB"
	if got := lipgloss.Width(src); got != 6 {
		t.Fatalf("前提不成立：%q 宽度应为 6，实际 %d", src, got)
	}
	got := spliceCells(src, "XY", 4)
	if got != "中文XY" {
		t.Errorf("spliceCells(%q, XY, 4) = %q，期望 %q", src, got, "中文XY")
	}
}
