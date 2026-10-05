package canvas

import (
	"strings"
	"testing"
)

// TestWrapNeverLosesText 是折行的硬要求：**一个字都不能少**。
//
// 用户连续两次报「风险提示显示不全」，根因都是折行/截断把字弄丢了。
// 断言方式照搬 KQFLOW 已有的 TestDisclaimerCompleteInBothViews：
// 把结果各行拼接、去掉所有空白后，必须**逐字包含**原文。只断言"某几句还在"
// 是不够的——丢的往往正是中间那几句。
func TestWrapNeverLosesText(t *testing.T) {
	samples := []string{
		"风险提示：手机推送使用第三方服务 ntfy，频道默认全网公开，知道频道名的人都能收到通知，因此请不要把频道地址泄露给他人。",
		"设置页不再承载整段声明，改为「地址 + 一句关键警示 + 指路」。",
		"短",
		"",
		"abcdefghijklmnopqrstuvwxyz",
		"第一行\n第二行",
		"第一行\n\n第三行",
		"中文与 ASCII 混排 mixed content 12345",
		strings.Repeat("很长的一段中文", 30),
		"汉字english汉字",
	}
	widths := []int{1, 2, 3, 8, 20, 36, 51, 92, 120}

	for _, s := range samples {
		for _, w := range widths {
			lines := Wrap(s, w)
			if len(lines) == 0 {
				t.Fatalf("width=%d 输入 %q：结果不应为空切片", w, s)
			}
			joined := stripAllSpace(strings.Join(lines, ""))
			want := stripAllSpace(s)
			if !strings.Contains(joined, want) {
				t.Errorf("width=%d 输入 %q 丢字：折行后为 %q（拼接 %q）", w, s, lines, joined)
			}
			// 每行都不得超过给定宽度——但 width=1 是物理上做不到的例外：
			// 最窄的中文也要 2 列，只能让它独占一行（见 TestWrapSingleColumnWideRune）。
			// 宿主不该在这么窄的地方渲染真实内容（KQFLOW 会在 60×16 以下改显提示）。
			if w < 2 {
				continue
			}
			for i, l := range lines {
				if got := StringWidth(l); got > w {
					t.Errorf("width=%d 第 %d 行宽 %d 超宽：%q", w, i, got, l)
				}
			}
			// 硬换行必须保留：段数不能少于原文的换行数 + 1。
			if need := strings.Count(s, "\n") + 1; len(lines) < need && StringWidth(s) <= w {
				t.Errorf("width=%d 输入 %q：硬换行应被保留（至少 %d 段），实际 %d 段 %q",
					w, s, need, len(lines), lines)
			}
		}
	}
}

func stripAllSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// TestWrapWideRuneNotSplit 验证宽字符不跨行劈开。
func TestWrapWideRuneNotSplit(t *testing.T) {
	// 宽度 3：只能放一个汉字（2 列）加上一个 ASCII，第二个汉字必须另起一行。
	lines := Wrap("中a中b", 3)
	for i, l := range lines {
		if got := StringWidth(l); got > 3 {
			t.Fatalf("第 %d 行宽 %d 超过 3：%q", i, got, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("宽度 3 下「中a中b」应折成 2 行，实际 %d 行：%q", len(lines), lines)
	}
	if lines[0] != "中a" || lines[1] != "中b" {
		t.Errorf("折行结果应为 [中a, 中b]，实际 %q", lines)
	}
}

// TestWrapSingleColumnWideRune 验证 width=1 遇到宽字符时既不死循环也不丢字。
//
// 这是极端但真实的情况：终端被拖到 1 列宽时，宽字符放不下，
// 引擎的选择是"让它独占一行并如实呈现"，而不是丢字或无限循环。
func TestWrapSingleColumnWideRune(t *testing.T) {
	lines := Wrap("中文", 1)
	joined := strings.Join(lines, "")
	if joined != "中文" {
		t.Fatalf("width=1 下不应丢字，实际 %q", joined)
	}
	if len(lines) != 2 {
		t.Fatalf("两个汉字在 width=1 下应各占一行，实际 %d 行：%q", len(lines), lines)
	}
}

// TestWrapTrimsTrailingSpace 验证行尾空格被裁掉。
//
// 否则看不见的空格会把面板右边框顶出去——这正是"渲染坏了"的一种。
func TestWrapTrimsTrailingSpace(t *testing.T) {
	lines := Wrap("abc   def", 4)
	for i, l := range lines {
		if strings.HasSuffix(l, " ") {
			t.Errorf("第 %d 行尾部不应有空格：%q", i, l)
		}
	}
}

// TestWrapBalancedImprovesLastLine 验证均衡折行比贪心折行更均匀。
//
// 用例就是 SKILL 里记着的那种观感问题：中文按最大宽度贪心折，最后一行常只剩一个句号。
func TestWrapBalancedImprovesLastLine(t *testing.T) {
	s := "把今天过好，就是对未来最好的投资。"
	const w = 12
	greedy := Wrap(s, w)
	balanced := WrapBalanced(s, w)

	if len(balanced) > len(greedy) {
		t.Fatalf("均衡折行不应比贪心折行行数更多：%d vs %d（%q）", len(balanced), len(greedy), balanced)
	}
	// 逐字不丢。
	if got, want := stripAllSpace(strings.Join(balanced, "")), stripAllSpace(s); got != want {
		t.Fatalf("均衡折行丢了字：%q vs %q", got, want)
	}
	for i, l := range balanced {
		if StringWidth(l) > w {
			t.Errorf("第 %d 行超宽：%q", i, l)
		}
	}
	// 行数相同时，均衡方案的行长极差不应更大。
	if len(balanced) == len(greedy) {
		if lineSpread(balanced) > lineSpread(greedy) {
			t.Errorf("行数相同但更不均衡：balanced=%v spread=%d, greedy=%v spread=%d",
				balanced, lineSpread(balanced), greedy, lineSpread(greedy))
		}
	}
}

// TestTruncateFunctionsWidths 覆盖各截断工具的宽度契约。
func TestTruncateFunctionsWidths(t *testing.T) {
	s := "中文abc"
	if got := Truncate(s, 4); got != "中文" {
		t.Errorf("Truncate(4) 应为 %q，实际 %q", "中文", got)
	}
	if got := Truncate(s, 0); got != "" {
		t.Errorf("Truncate(0) 应为空，实际 %q", got)
	}
	if got := Truncate(s, 100); got != s {
		t.Errorf("足够宽时应原样返回，实际 %q", got)
	}
	if got := TruncateEllipsis(s, 5); StringWidth(got) > 5 {
		t.Errorf("带省略号截断不得超过 5 列：%q（%d 列）", got, StringWidth(got))
	}
	if got := TruncateEllipsis(s, 1); got != "…" {
		t.Errorf("宽度 1 应只放一个省略号，实际 %q", got)
	}
	if got := TruncateFromEnd(s, 3); got != "abc" {
		t.Errorf("从末尾保留 3 列应为 %q，实际 %q", "abc", got)
	}
}

// TestPadWidths 验证补齐工具不缩水、不超宽。
func TestPadWidths(t *testing.T) {
	if got := Pad("中文", 6); StringWidth(got) != 6 {
		t.Errorf("Pad 应补到 6 列，实际 %d 列（%q）", StringWidth(got), got)
	}
	if got := Pad("中文", 4); got != "中文" {
		t.Errorf("已够宽时不应改动，实际 %q", got)
	}
	// "中" 占 2 列，在 5 列里居中 = 左侧补 1 个空格，总宽 3 列（PadCenter 不补齐右侧）。
	if got := PadCenter("中", 5); got != " 中" || StringWidth(got) != 3 {
		t.Errorf("PadCenter 应在左侧补 1 个空格（中占 2 列），实际 %q（%d 列）", got, StringWidth(got))
	}
	// 右侧不补齐是刻意的：v2.1.0 用"铺满整行"补底色，看起来像光标拖了一行半。
	if got := PadCenter("中", 4); got != " 中" {
		t.Errorf("居中只需补左侧，实际 %q", got)
	}
}

// TestWidthContractIsDeterministic 固化宽度口径，防止它随环境漂移。
//
// 这是本模块最重要的一条"环境无关"保证。go-runewidth 默认按环境自动判断宽度，
// 于是同一份代码在不同机器上会得到不同的「中」宽度，所有列位置判断一起偏，
// 而测试还是绿的。canvas 在 init 里把口径显式钉死，这条测试守着它。
func TestWidthContractIsDeterministic(t *testing.T) {
	if EastAsianWidth() {
		t.Fatal("歧义宽度口径必须是确定的 false：设成 true 会把框线与几何图形也算成 2 列，" +
			"本项目赖以拼版面的 ╭ ─ │ ○ ◐ ◈ ▏ 会整体错位")
	}
	// 中日韩文字与全角标点：2 列。
	for _, r := range []rune{'中', '好', '，', '。', '：', '（', '）', '「', '」'} {
		if got := RuneWidth(r); got != 2 {
			t.Errorf("%q 应算 2 列，实际 %d", r, got)
		}
	}
	// ASCII、框线与几何图形：1 列。若这里失败，说明版面元素会整体错位。
	for _, r := range []rune{'a', ' ', '0', '╭', '╮', '╰', '─', '│', '┼', '✔', '○', '◐', '◈', '▏', '…'} {
		if got := RuneWidth(r); got != 1 {
			t.Errorf("%q 应算 1 列，实际 %d", r, got)
		}
	}
	// 混排整行的宽度必须等于逐字宽度之和——两个口径不能分裂。
	line := "   ╭─────今日待办 3/7"
	want := 0
	for _, r := range line {
		want += RuneWidth(r)
	}
	if got := StringWidth(line); got != want {
		t.Errorf("StringWidth 与逐字 RuneWidth 求和必须一致：%d vs %d", got, want)
	}
}

// TestRuneWidthBasics 固化宽度口径（约定：中文两列、ASCII 一列）。
func TestRuneWidthBasics(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1}, {' ', 1}, {'中', 2}, {'好', 2}, {'，', 2}, {'。', 2},
		{'╭', 1}, {'─', 1}, {'◈', 1}, {'✔', 1}, {'○', 1},
		{'\u0301', 0}, {'\x00', 0},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != c.want {
			t.Errorf("RuneWidth(%q) = %d，期望 %d", c.r, got, c.want)
		}
	}
	if got := StringWidth("中文abc"); got != 7 {
		t.Errorf("StringWidth(中文abc) = %d，期望 7", got)
	}
}
