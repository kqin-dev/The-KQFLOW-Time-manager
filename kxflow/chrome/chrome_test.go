package chrome

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
)

// drawInto 在一块画布上画一行，返回纯文本结果。
func drawInto(t *testing.T, width int, draw func(c *canvas.Canvas, r geometry.Rect)) (string, *canvas.Canvas) {
	t.Helper()
	c := canvas.New(width, 1)
	draw(c, geometry.NewRect(0, 0, width, 1))
	return c.RowString(0), c
}

// assertNoLayoutDefect 断言"没有布局缺陷"：不越界、不覆盖。
//
// 这里刻意**不**要求 Diag.Clean()：裁剪（Clipped）在窄栏上是正常且必要的
// ——文本放不下就该被裁掉（而且调用方能看到 clipped 标志）。
// 越界（Overflow）与覆盖（Collisions）才是"布局算错了"的信号。
func assertNoLayoutDefect(t *testing.T, what string, width int, cv *canvas.Canvas) {
	t.Helper()
	if cv.Diag.Overflow != 0 {
		t.Errorf("%s：宽度 %d 下发生越界写入 %d 次", what, width, cv.Diag.Overflow)
	}
	if cv.Diag.Collisions != 0 {
		t.Errorf("%s：宽度 %d 下发生内容覆盖 %d 次", what, width, cv.Diag.Collisions)
	}
}

// TestHeaderNeverOverflows 在宽度网格上断言上栏永不超宽。
//
// 栏宽是"永远可能不够"的地方：面包屑与状态都是内容决定的。
// 因此这里的断言不是"某个宽度下好看"，而是**任何宽度下都不越界**。
func TestHeaderNeverOverflows(t *testing.T) {
	cases := []struct {
		left, right string
	}{
		{"KQFLOW", "今日专注 2h13m"},
		{"KQFLOW · 设置 · 手机推送说明", "今日专注 12h34m · 日界线 04:00"},
		{"很短", ""},
		{"", "只有状态"},
		{"", ""},
		{"一二三四五六七八九十", "一二三四五六七八九十"},
	}
	for width := 0; width <= 200; width++ {
		for _, c := range cases {
			h := &HeaderBar{Crumbs: []string{c.left}, Status: c.right}
			got, cv := drawInto(t, width, h.Draw)
			if w := canvas.StringWidth(got); w > width {
				t.Fatalf("宽度 %d 下上栏超宽 %d：%q（左=%q 右=%q）", width, w, got, c.left, c.right)
			}
			assertNoLayoutDefect(t, "上栏", width, cv)
		}
	}
}

// TestHeaderPrefersStatusWhenTight 验证空间不够时先牺牲左侧。
//
// 取舍理由：状态（今日专注）是用户最关心的信息；
// "当前在哪个页面"通常也能从界面本身看出来。
func TestHeaderPrefersStatusWhenTight(t *testing.T) {
	h := &HeaderBar{
		Crumbs: []string{"KQFLOW · 一个相当长的面包屑标题"},
		Status: "状态",
	}
	got, _ := drawInto(t, 10, h.Draw)
	if !strings.Contains(got, "状态") {
		t.Errorf("空间紧张时状态必须保留，实际 %q", got)
	}
	if strings.Contains(got, "面包屑") {
		t.Errorf("空间紧张时面包屑应被缩短，实际 %q", got)
	}
}

// TestHeaderExtrasReset 验证追加内容可以被清空。
//
// 这条防的是"跨帧累积"：上一版曾把每帧的提示 append 到同一个切片，
// 跑一会儿界面就被自己塞满。
func TestHeaderExtrasReset(t *testing.T) {
	h := &HeaderBar{Crumbs: []string{"KQFLOW"}}
	for i := 0; i < 5; i++ {
		h.AddLeft("左")
		h.AddRight("右")
	}
	before := h.LeftText()
	h.ResetExtras()
	after := h.LeftText()
	if after != "KQFLOW" {
		t.Errorf("清空追加内容后左半应只剩面包屑，实际 %q", after)
	}
	if canvas.StringWidth(before) <= canvas.StringWidth(after) {
		t.Error("追加内容应当确实影响了文本（否则这个用例没有验证到东西）")
	}
	if h.RightText() != "" {
		t.Errorf("清空后右半应为空，实际 %q", h.RightText())
	}
}

// TestFooterNeverOverflows 在宽度网格上断言下栏永不超宽。
func TestFooterNeverOverflows(t *testing.T) {
	hints := []KeymapHint{
		{Key: "tab", Desc: "切换栏位"},
		{Key: "j/k", Desc: "移动"},
		{Key: "space", Desc: "勾选完成"},
		{Key: "?", Desc: "帮助"},
		{Key: "q", Desc: "退出"},
	}
	texts := []string{"", "今日已过 68%", "一个相当长的状态文本用来试探下栏会不会被撑破"}
	for width := 0; width <= 200; width++ {
		for _, txt := range texts {
			f := &FooterBar{Text: txt, Hints: hints, HasProgress: true, Progress: 0.68}
			got, cv := drawInto(t, width, f.Draw)
			if w := canvas.StringWidth(got); w > width {
				t.Fatalf("宽度 %d 下下栏超宽 %d：%q", width, w, got)
			}
			assertNoLayoutDefect(t, "下栏", width, cv)
		}
	}
}

// TestFooterPrefersHintsWhenTight 验证空间不够时先牺牲左侧状态文本。
//
// 取舍与上栏相反：按键提示是"我现在能做什么"的唯一提示，
// 而左侧状态通常是可再获取的信息。
func TestFooterPrefersHintsWhenTight(t *testing.T) {
	f := &FooterBar{
		Text:  "一个相当长的状态文本",
		Hints: []KeymapHint{{Key: "q", Desc: "退出"}},
	}
	got, _ := drawInto(t, 12, f.Draw)
	if !strings.Contains(got, "q:退出") {
		t.Errorf("空间紧张时按键提示必须保留，实际 %q", got)
	}
	if strings.Contains(got, "相当长") {
		t.Errorf("空间紧张时状态文本应被缩短，实际 %q", got)
	}
}

// TestProgressBarShape 验证进度条宽度固定、比例正确。
func TestProgressBarShape(t *testing.T) {
	f := &FooterBar{HasProgress: true, Progress: 0.5}
	got, _ := drawInto(t, 40, f.Draw)
	filled := strings.Count(got, "█")
	empty := strings.Count(got, "░")
	if filled == 0 || empty == 0 {
		t.Fatalf("半进度应同时有实心与空心块，实际 %q", got)
	}
	// 总量应当稳定（宽度由布局给，不由内容撑）。
	if total := filled + empty; total < 8 || total > 20 {
		t.Errorf("进度条总宽应在 8~20 之间，实际 %d（%q）", total, got)
	}
	// 比例不能反：0.5 时两侧都应存在，且实心不超过空心太多。
	if filled > empty+2 {
		t.Errorf("0.5 进度下实心 %d 不应明显多于空心 %d", filled, empty)
	}
}

// TestProgressClamped 验证越界的进度值被夹紧而不是画出怪东西。
func TestProgressClamped(t *testing.T) {
	for _, p := range []float64{-1, 0, 1, 2} {
		f := &FooterBar{HasProgress: true, Progress: p}
		got, _ := drawInto(t, 30, f.Draw)
		if strings.Contains(got, "\x00") {
			t.Errorf("进度 %v 产生了非法字符", p)
		}
		if canvas.StringWidth(got) > 30 {
			t.Errorf("进度 %v 导致超宽：%q", p, got)
		}
	}
	// 进度为 0 时不该出现实心块。
	f := &FooterBar{HasProgress: true, Progress: 0}
	got, _ := drawInto(t, 30, f.Draw)
	if strings.Contains(got, "█") {
		t.Errorf("进度 0 时不应有实心块，实际 %q", got)
	}
}

// TestEmptyRectDrawsNothing 验证退化矩形下不画也不 panic。
func TestEmptyRectDrawsNothing(t *testing.T) {
	c := canvas.New(20, 3)
	h := &HeaderBar{Crumbs: []string{"KQFLOW"}}
	f := &FooterBar{Text: "状态"}
	h.Draw(c, geometry.Rect{X: 0, Y: 0})
	f.Draw(c, geometry.Rect{X: 0, Y: 1, W: 0, H: 0})
	// 高为 0 的行也不应被画。
	h.Draw(c, geometry.NewRect(0, 2, 20, 0))
	if got := strings.TrimSpace(c.String()); got != "" {
		t.Errorf("退化矩形下不应画出内容，实际 %q", got)
	}
}

// TestHintsTextIsPlain 验证提示文本是纯文本（无样式残留）。
//
// 有样式残留会破坏"按显示宽度算"的前提——那是本项目出过三次事故的地方。
func TestHintsTextIsPlain(t *testing.T) {
	f := &FooterBar{Hints: []KeymapHint{
		{Key: "tab", Desc: "切换"},
		{Key: "", Desc: ""}, // 空提示应被跳过
		{Key: "q", Desc: "退出"},
	}}
	got := f.HintsText()
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("提示文本不应含转义序列：%q", got)
	}
	if want := "tab:切换  q:退出"; got != want {
		t.Errorf("提示文本应为 %q，实际 %q", want, got)
	}
}
