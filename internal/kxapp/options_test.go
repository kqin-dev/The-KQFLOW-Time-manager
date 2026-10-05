package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
)

// TestDashboardOptionGutter 验证看板选项的"键位栏目 + 说明折行"排版。
//
// 断言两件事：
//  1. 每个选项的说明**完整出现**（可能折行，但不丢字）；
//  2. 续行对齐到键位右侧（缩进宽度 = 键位栏目宽度），
//     而不是从最左边顶格开始（那样看起来像另起一条）。
func TestDashboardOptionGutter(t *testing.T) {
	bindings := []plugin.OptionBindingView{
		{Key: "1", Label: "打标签「拿快递」", Context: true},
		{Key: "2", Label: "设截止时间「拿快递」", Context: true},
		{Key: "3", Label: "帮助 / Help"},
	}
	k := &kernel{options: func() []plugin.OptionBindingView { return bindings }}

	const w, h = 44, 12
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
	// 每个选项的第一行都应以键位栏目开头（`  N  `）。
	for i, b := range bindings {
		if i >= len(out) {
			t.Fatalf("选项 %d 没有对应行：%v", i, out)
		}
		want := "  " + b.Key + "  "
		if !strings.HasPrefix(out[i], want) {
			t.Errorf("第 %d 个选项应以 %q 开头，实际 %q", i, want, out[i])
		}
	}
	// 每行都不越界。
	for i, l := range out {
		if got := canvas.StringWidth(l); got > w {
			t.Errorf("第 %d 行宽 %d 超过 %d：%q", i, got, w, l)
		}
	}
	// 第二个选项的说明很长，在 24 列下必然折行；续行应当缩进到键位右侧
	// （不是顶格）——顶格会看起来像另起一条选项。
	narrow := renderInRect(t, 24, 8, func(ctx plugin.RenderCtx) {
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
	// 第 2 行是选项 1 的续行：应当以缩进开头，且不含键位。
	if !strings.HasPrefix(narrowRows[1], optionGutter) {
		t.Errorf("续行应以 %d 个空格缩进，实际 %q", len(optionGutter), narrowRows[1])
	}
	if strings.HasPrefix(strings.TrimSpace(narrowRows[1]), "1") {
		t.Errorf("续行不应重复键位，实际 %q", narrowRows[1])
	}
}
