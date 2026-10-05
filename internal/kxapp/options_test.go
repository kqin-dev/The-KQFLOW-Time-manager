package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
)

// TestDashboardOptionGutter 验证看板上选项列表的排版。
//
// 断言三件事：
//  1. 每个选项的说明**完整出现**（可能折行，但不丢字）；
//  2. **不出现数字编号**——用户反馈"有 10 个选项怎么办呢"；
//  3. 续行对齐到记号右侧，而不是从最左边顶格开始
//     （那样看起来像另起一条选项）。
func TestDashboardOptionGutter(t *testing.T) {
	bindings := []plugin.OptionBindingView{
		{Label: "打标签「拿快递」", Context: true},
		{Label: "设截止时间「拿快递」", Context: true},
		{Label: "帮助 / Help"},
	}
	k := &kernel{options: func() []plugin.OptionBindingView { return bindings }}

	const w, h = 44, 14
	raw := renderInRect(t, w, h, func(ctx plugin.RenderCtx) {
		k.drawOptions(ctx, 0, ctx.Rect)
	})
	// 画布总行数多于实际用到的行；只保留写了内容的部分。
	out := usedRows(trimTrailing(raw))
	joined := strings.Join(out, "\n")
	t.Logf("渲染结果：\n%s", joined)

	for _, b := range bindings {
		// 说明文字按显示宽度拆开后必须都在（可能跨行）。
		if !strings.Contains(flattenForTextMatch(joined), flattenForTextMatch(b.Label)) {
			t.Errorf("选项 %q 的说明未完整出现：\n%s", b.Label, joined)
		}
	}
	// 选项行必须以记号开头（○ 或 ▸），**不能是数字**。
	foundMark := false
	for _, l := range out {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "可用操作") {
			continue
		}
		if trimmed[0] >= '1' && trimmed[0] <= '9' {
			t.Errorf("选项行不该以数字开头（已去掉数字快捷键）：%q", l)
			continue
		}
		if strings.HasPrefix(trimmed, "▸") || strings.HasPrefix(trimmed, "·") {
			foundMark = true
		}
	}
	if !foundMark {
		t.Errorf("选项行应以记号开头，实际各行：%v", out)
	}
	// 每行都不越界。
	for i, l := range out {
		if got := canvas.StringWidth(l); got > w {
			t.Errorf("第 %d 行宽 %d 超过 %d：%q", i, got, w, l)
		}
	}

	// 第三个选项的说明很长，在 24 列下必然折行；续行应当缩进（不是顶格）。
	narrow := renderInRect(t, 24, 10, func(ctx plugin.RenderCtx) {
		k.drawOptions(ctx, 0, ctx.Rect)
	})
	narrowRows := usedRows(trimTrailing(narrow))
	t.Logf("24 列下的渲染结果：\n%s", strings.Join(narrowRows, "\n"))
	if len(narrowRows) <= len(bindings) {
		t.Fatalf("24 列下应当出现续行（选项 %d 个，实际 %d 行）", len(bindings), len(narrowRows))
	}
	for i, l := range narrowRows {
		if canvas.StringWidth(l) > 24 {
			t.Errorf("24 列下第 %d 行宽 %d 越界：%q", i, canvas.StringWidth(l), l)
		}
	}
	// 找一条续行：它应当以缩进开头且不含记号。
	for _, l := range narrowRows {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "可用操作") {
			continue
		}
		if strings.HasPrefix(trimmed, "▸") || strings.HasPrefix(trimmed, "·") {
			continue
		}
		lead := len(l) - len(strings.TrimLeft(l, " "))
		if lead != len(optionGutter) {
			t.Errorf("续行缩进应为 %d 个空格，实际 %d：%q", len(optionGutter), lead, l)
		}
		break
	}
}
