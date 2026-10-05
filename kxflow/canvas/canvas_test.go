package canvas

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
)

// assertFrameShape 断言一帧输出满足输出契约。
//
// 契约是「行数正好 H、每行**不超过** W 列」，不是"每行正好 W 列"：
// 画布行尾的空格在输出时被裁掉，这是刻意的——只有边框落在最后一个列上时，
// 面板右边界才会紧贴终端右缘（v2.1.0 的"边框没对齐/右边框消失"就与这类
// 行尾空白处理有关）。所以这里同时断言两件事：
//  1. 没有任何一行超过 W（**这才是要防的溢出**）；
//  2. 裁掉行尾空白后，内容仍然完整（不会被误裁）。
func assertFrameShape(t *testing.T, name string, out string, w, h int) {
	t.Helper()
	if w <= 0 || h <= 0 {
		if out != "" {
			t.Fatalf("%s：%dx%d 应输出空字符串，实际 %q", name, w, h, out)
		}
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("%s：%dx%d 输出 %d 行，应恰好 %d 行", name, w, h, len(lines), h)
	}
	for i, l := range lines {
		if got := StringWidth(l); got > w {
			t.Errorf("%s：第 %d 行宽度 %d 超过 %d 列：%q", name, i, got, w, l)
		}
		if trimmed := strings.TrimRight(l, " "); StringWidth(trimmed) > w {
			t.Errorf("%s：第 %d 行去掉行尾空白后仍超宽：%q", name, i, trimmed)
		}
	}
}

// TestCanvasNeverOverflows 是核心结构性测试：**任意尺寸 × 任意内容都不会超宽超行**。
//
// 覆盖 1×1 到 200×60：小到"窗口太小提示"那一档，大到用户报过四次问题的
// 160×44 宽终端。随机内容包含中文、宽字符、控制字符与超长串。
func TestCanvasNeverOverflows(t *testing.T) {
	// 固定种子：失败必须可复现，不能是"偶发红"。
	rng := rand.New(rand.NewSource(20261005))

	sizes := []geometry.Size{
		{W: 1, H: 1}, {W: 2, H: 1}, {W: 1, H: 2}, {W: 5, H: 3},
		{W: 10, H: 3}, {W: 20, H: 5}, {W: 40, H: 10}, {W: 59, H: 15},
		{W: 60, H: 16}, {W: 80, H: 24}, {W: 100, H: 30}, {W: 120, H: 30},
		{W: 160, H: 44}, {W: 200, H: 60},
	}
	samples := []string{
		"",
		"KQFLOW",
		"今日待办 3/7 · 已完成",
		"风险提示：手机推送使用第三方服务，频道默认全网公开，请勿泄露频道地址给他人。",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"中",
		"宽字符测试：一个汉字占两列，绝不能跨行劈开",
		"tab\t换行\n第二行",
		"控制字符\x00\x07应被拒绝",
		strings.Repeat("中文混排ABC123", 40),
		"╭─┬─╮│┼│╰─┴─╯",
	}

	for _, size := range sizes {
		for _, s := range samples {
			c := New(size.W, size.H)
			label := fmt.Sprintf("%dx%d 输入 %q", size.W, size.H, snippet(s))

			// 随机铺若干矩形与文本，模拟"每个插件各画各的"。
			for i := 0; i < 6; i++ {
				r := geometry.NewRect(
					rng.Intn(size.W+3)-2,
					rng.Intn(size.H+3)-2,
					rng.Intn(size.W+3),
					rng.Intn(size.H+3),
				)
				c.DrawBox(r, FrameRounded, DefaultStyle)
				inner := r.Inset(1, 1)
				restore := c.PushClip(inner)
				for row := 0; row < inner.H && row < 3; row++ {
					c.Text(inner.X, inner.Y+row, s, StyleID(rng.Intn(3)))
				}
				restore()
			}
			c.Text(0, 0, s, 1)
			c.Text(size.W/2, size.H/2, s, 2)

			out := c.Render(PlainPalette{})
			assertFrameShape(t, "随机内容 "+snippet(s), out, size.W, size.H)
			// 画布自洽性：宽字符必须成对（见 CheckInvariants 的说明）。
			if bad := c.CheckInvariants(); bad != "" {
				t.Fatalf("%s：画布不变量被破坏：%s", label, bad)
			}
			// 裁剪区恢复后必须回到整块画布：否则说明 PushClip 没成对使用。
			if c.Clip() != c.Bounds() {
				t.Fatalf("%dx%d：裁剪区未恢复：%v", size.W, size.H, c.Clip())
			}
		}
	}
}

// TestOverwritingWideRuneKeepsRowWidth 覆盖"改写宽字符附近格子"这一整类情况。
//
// 这一类曾经真的把画布写坏过，而且坏得很隐蔽：某格被写成了宽字符的右半格标记，
// 但它的左邻并不是那个宽字符。后果是纯文本输出少一个字符、行宽却按多一列算——
// **所有基于宽度的诊断会一起开始骗人**。所以这里把四种改写姿势全列出来逐个断言。
func TestOverwritingWideRuneKeepsRowWidth(t *testing.T) {
	const w = 8
	cases := []struct {
		name string
		run  func(c *Canvas)
	}{
		{"整行先填满再写中文", func(c *Canvas) {
			c.Fill(c.Bounds(), '─', 1)
			c.Text(2, 0, "中文", 2)
		}},
		{"中文上再写中文（错位半格）", func(c *Canvas) {
			c.Text(0, 0, "中文", 2)
			c.Text(2, 0, "字", 2)
		}},
		{"窄字符覆盖宽字符左半格", func(c *Canvas) {
			c.Text(0, 0, "中文", 2)
			c.Text(0, 0, "A", 1)
		}},
		{"窄字符覆盖宽字符右半格", func(c *Canvas) {
			c.Text(0, 0, "中文", 2)
			c.Text(1, 0, "A", 1)
		}},
		{"宽字符覆盖窄字符", func(c *Canvas) {
			c.Text(0, 0, "ABCD", 1)
			c.Text(1, 0, "中", 1)
		}},
		{"行尾放不下的宽字符被整体拒绝", func(c *Canvas) {
			c.Text(w-1, 0, "中", 1)
		}},
		{"连续三次改写同一格", func(c *Canvas) {
			c.Text(3, 0, "中", 1)
			c.Text(3, 0, "x", 1)
			c.Text(3, 0, "文", 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(w, 1)
			tc.run(c)
			if bad := c.CheckInvariants(); bad != "" {
				t.Fatalf("不变量被破坏：%s\n画布=%q", bad, c.RowString(0))
			}
			if got := StringWidth(c.RowString(0)); got != w {
				t.Fatalf("行宽应为 %d，实际 %d：%q", w, got, c.RowString(0))
			}
			// 不允许出现孤立的右半格标记。
			for x := 0; x < w; x++ {
				if cell := c.CellAt(x, 0); cell.Cont && x == 0 {
					t.Fatal("第 0 列不应带右半格标记")
				}
			}
		})
	}
}

// TestWideRuneRejectedAtRowEnd 验证"宽字符放不下就整个不写"。
//
// 写半个汉字在终端上表现为乱码或错位，比"没写上"糟得多。
func TestWideRuneRejectedAtRowEnd(t *testing.T) {
	c := New(5, 1)
	if cols, clipped := c.Text(4, 0, "中", 1); cols != 0 || !clipped {
		t.Fatalf("行末放不下宽字符时应整体拒绝（写入 0 列且标记裁剪），实际 %d 列 clipped=%v", cols, clipped)
	}
	if got := c.RowString(0); got != "     " {
		t.Fatalf("被拒绝后画布应保持空白，实际 %q", got)
	}
	// Text 层的"有内容没画出来"记在 Clipped 上；Set 层的"写不进去"才记 Overflow。
	// 这里必须断言 Clipped：它正是"丢字"这件事的留痕，不能让它悄悄发生。
	if c.Diag.Clipped == 0 {
		t.Error("被裁剪的写入必须记入诊断（Clipped），否则丢字不留痕")
	}
	if c.Diag.Clean() {
		t.Error("发生过截断时 Diagnostics.Clean() 不应为真")
	}
}

func snippet(s string) string {
	r := []rune(s)
	if len(r) > 12 {
		return string(r[:12]) + "…"
	}
	return s
}

// TestRenderShapeIsExactEvenWhenEmpty 验证"什么都不画"也输出正好 W×H 的空白帧。
//
// 这一点很重要：TUI 的每一帧都必须占满屏幕，否则上一帧的残留会留在终端上。
func TestRenderShapeIsExactEvenWhenEmpty(t *testing.T) {
	for _, size := range []geometry.Size{{W: 10, H: 3}, {W: 1, H: 5}, {W: 80, H: 24}} {
		c := New(size.W, size.H)
		out := c.Render(PlainPalette{})
		assertFrameShape(t, "空画布", out, size.W, size.H)
		for _, l := range strings.Split(out, "\n") {
			if strings.TrimSpace(l) != "" {
				t.Fatalf("空画布应全是空白，实际 %q", l)
			}
		}
	}
}

// TestDiagnosticsCleanOnNormalPath 验证正常绘制路径下诊断恒为 0。
//
// 这是"开发期必须为 0"的那条约束：它不是"尽量小"，而是硬性断言。
// 一旦某个渲染路径开始偷偷越界或覆盖边框，这条测试立刻红。
func TestDiagnosticsCleanOnNormalPath(t *testing.T) {
	c := New(80, 24)
	full := c.Bounds()
	header, rest := full.CutTop(1)
	body, footer := rest.CutBottom(1)
	left, tmp := body.CutLeft(22)
	center, right := tmp.CutRight(20)

	c.Text(header.X, header.Y, "KQFLOW · 2026-10-03 周六 · 日界线 04:00", 2)
	c.DrawBox(left, FrameRounded, 3)
	c.DrawBox(center, FrameRounded, 1)
	c.DrawBox(right, FrameRounded, 3)
	c.Text(footer.X, footer.Y, "tab 切换栏位 · j/k 移动 · space 勾选 · ? 帮助 · q 退出", 2)

	// 每个面板的内容都画在自己的内容区里。
	drawIn := func(r geometry.Rect, lines ...string) {
		inner := r.Inset(2, 1)
		restore := c.PushClip(inner)
		defer restore()
		for i, l := range lines {
			if i >= inner.H {
				break
			}
			c.Text(inner.X, inner.Y+i, Truncate(l, inner.W), 2)
		}
	}
	drawIn(left, "TODAY · 固定 (3)", "○ 写架构设计文档")
	drawIn(center, "KQFLOW", "今日待办 3/7", "  随手记 / Note", "  专注计时 / Focus")
	drawIn(right, "GOAL (2)", "○ 发布 v3.0.0")

	if !c.Diag.Clean() {
		t.Fatalf("正常渲染路径不应有任何诊断，实际 overflow=%d collisions=%d clipped=%d events=%v",
			c.Diag.Overflow, c.Diag.Collisions, c.Diag.Clipped, c.Diag.Events)
	}
	out := c.Render(PlainPalette{})
	assertFrameShape(t, "正常看板", out, 80, 24)
	// 边框必须完整：这是"弹窗把左右面板边框切出断口"那个事故的直接断言。
	// 取第 1 行（主体第一行），第 0 行是上栏文本、本来就没有边框。
	// 注意要先去掉行尾空白再按显示列取字符——行尾空格被裁掉是契约的一部分。
	lines := strings.Split(out, "\n")
	borderRow := strings.TrimRight(lines[1], " ")
	for _, x := range []int{0, 21, 22, 59, 60, 79} {
		if got := runeAtColumn(borderRow, x); !strings.ContainsRune("╭╮─│", got) {
			t.Errorf("第 1 行第 %d 列应是边框字符，实际 %q（整行 %q）", x, string(got), borderRow)
		}
	}
}

// runeAtColumn 返回一行纯文本里第 col 个**显示列**上的字符。
//
// 测试必须用显示列取字符，不能直接索引 rune 切片或字节切片——
// KQFLOW 历史上就因为测试里用错坐标系，导致"实现有 bug 而测试是绿的"。
func runeAtColumn(line string, col int) rune {
	w := 0
	for _, r := range line {
		if w == col {
			return r
		}
		w += StringWidth(string(r))
		if w > col {
			// col 落在一个宽字符的右半格上：如实返回该字符，
			// 这样"中文把边框挤掉"这类问题仍然会被看到。
			return r
		}
	}
	return 0
}

// TestClipConfinesWrites 验证裁剪区真的"关得住"。
//
// 攻击场景：一个磁贴拿到自己的矩形后，试图画到整块屏幕（相当于 v2.1.0 里
// "弹窗横跨三栏"）。结果必须是：一个字符都画不出去，且诊断抓到越界。
func TestClipConfinesWrites(t *testing.T) {
	c := New(80, 24)
	panel := geometry.NewRect(30, 5, 20, 10)
	c.DrawBox(panel, FrameRounded, 0)

	inner := panel.Inset(1, 1)
	restore := c.PushClip(inner)
	// 恶意写入：从自己的左上角开始写一屏那么长，并且往画布左上角写。
	c.Text(inner.X, inner.Y, strings.Repeat("X", 500), 1)
	c.Text(0, 0, "越界内容", 1)
	c.Text(inner.X, 0, "越界内容", 1)
	c.Set(79, 23, 'Y', 1)
	restore()

	// 画布四角与裁剪区之外必须一个 X 都没有。
	out := c.String()
	lines := strings.Split(out, "\n")
	for y, l := range lines {
		for x, r := range []rune(l) {
			if r == 'X' && !inner.Contains(x, y) {
				t.Fatalf("字符画到了裁剪区之外 (%d,%d)", x, y)
			}
			if r == 'Y' {
				t.Fatalf("越界写入 (79,23) 竟然成功了")
			}
		}
	}
	// 越界的那三处文本一个字都不该出现在画布上——拒绝是彻底的，不是"部分写入"。
	for _, bad := range []string{"越界", "Y"} {
		if strings.Contains(out, bad) {
			t.Errorf("越界内容 %q 不应出现在画布上", bad)
		}
	}
	if c.Diag.Overflow == 0 {
		t.Error("越界写入必须被记入诊断，实际 Overflow=0")
	}
	assertFrameShape(t, "裁剪后的画布", c.Render(PlainPalette{}), 80, 24)
}

// TestClipIntersectsWithParent 验证嵌套裁剪区只会更小、不会更大。
func TestClipIntersectsWithParent(t *testing.T) {
	c := New(40, 10)
	outer := geometry.NewRect(5, 2, 20, 6)
	restore1 := c.PushClip(outer)
	// Text 返回 (实际写入列数, 是否被裁剪)。裁剪为真且写入 0 列 = 一个字都没写进去。
	colsOut, clippedOut := c.Text(4, 3, "X", 0)
	if !clippedOut || colsOut != 0 {
		t.Errorf("外层裁剪区之外应一个字都写不进去，实际写入 %d 列、clipped=%v", colsOut, clippedOut)
	}
	// 外层裁剪区之内必须写得进去——否则上面那条断言在"什么都没实现"时也是绿的。
	colsIn, clippedIn := c.Text(6, 3, "X", 0)
	if clippedIn || colsIn != 1 {
		t.Errorf("裁剪区之内应完整写入 1 列，实际写入 %d 列、clipped=%v", colsIn, clippedIn)
	}
	// 子裁剪区故意超出外层：应被夹到外层之内。
	inner := geometry.NewRect(0, 0, 100, 100)
	restore2 := c.PushClip(inner)
	if c.Clip() != outer {
		t.Fatalf("子裁剪区超出父级时应被夹紧为父级 %v，实际 %v", outer, c.Clip())
	}
	restore2()
	restore1()
	if c.Clip() != c.Bounds() {
		t.Fatalf("裁剪区应完全恢复，实际 %v", c.Clip())
	}
}

// TestWideRuneNotSplit 验证宽字符不会被劈成半个。
//
// 场景：裁剪区右边界正好落在一个汉字的中间。正确行为是**整个字不写**，
// 而不是写一半（写一半在终端上表现为乱码或错位）。
func TestWideRuneNotSplit(t *testing.T) {
	c := New(6, 1)
	restore := c.PushClip(geometry.NewRect(0, 0, 5, 1)) // 奇数宽度：第 5 列放不下宽字符
	cols, clipped := c.Text(0, 0, "中中中", 0)
	restore()

	if cols > 4 {
		t.Fatalf("应只写下两个汉字（4 列），实际写入 %d 列", cols)
	}
	if !clipped {
		t.Error("被裁剪时必须如实报告 clipped=true，不能悄悄丢字")
	}
	out := c.String()
	// 只比较实际写入的内容，忽略画布剩余的空列。
	if got := StringWidth(strings.TrimRight(out, " ")); got != 4 {
		t.Fatalf("写入内容的显示宽度应为 4，实际 %d（%q）", got, out)
	}
	if strings.ContainsRune(out, '\ufffd') {
		t.Error("不应出现半个字符")
	}
}

// TestWideRuneRightHalfKeepsRune 是上面那个"数字骗人"bug 的墓碑。
//
// 宽字符的右半格必须保留同一个字符，只用 Cont 标记"输出时跳过"。
// 曾经它被写成 Cell{Cont:true} 丢掉了字符，于是纯文本输出会多出一个 NUL：
// 一行 7 个字符却报出 11 列宽，所有基于纯文本的诊断都跟着错。
func TestWideRuneRightHalfKeepsRune(t *testing.T) {
	c := New(4, 1)
	c.Text(0, 0, "中文", 0)

	right := c.CellAt(1, 0)
	if !right.Cont {
		t.Fatal("第 1 列应是宽字符的右半格（Cont=true）")
	}
	if right.R != '中' {
		t.Fatalf("右半格必须保留同一个字符 '中'，实际 %q", string(right.R))
	}

	// 纯文本输出必须与画布宽度一致：不能多出字符，也不能出现 NUL。
	plain := c.String()
	if strings.ContainsRune(plain, 0) {
		t.Fatalf("纯文本输出里不应出现 NUL：%q", plain)
	}
	if plain != "中文" {
		t.Fatalf("纯文本应为 %q，实际 %q", "中文", plain)
	}
	if got := StringWidth(plain); got != 4 {
		t.Fatalf("纯文本宽度应为 4（两个汉字各占 2 列），实际 %d", got)
	}

	// RowString 与 String 必须一致（诊断与测试都依赖这一点）。
	if got := c.RowString(0); got != plain {
		t.Fatalf("RowString 应为 %q，实际 %q", plain, got)
	}
}

// TestRowWidthSumEqualsCanvasWidth 验证"画布每行显示宽度恰好等于 W"。
//
// 这是画布自洽性的核心断言：格子布局与显示宽度必须是同一个坐标系，
// 一旦分裂，后面所有基于宽度的判断都会骗人。
func TestRowWidthSumEqualsCanvasWidth(t *testing.T) {
	for _, size := range []geometry.Size{{W: 7, H: 1}, {W: 8, H: 2}, {W: 21, H: 3}, {W: 80, H: 4}} {
		c := New(size.W, size.H)
		c.Text(0, 0, "中文混排ABC123", 0)
		c.Text(0, 1, "汉字与ASCII混排x", 1)
		c.DrawBox(geometry.NewRect(0, size.H-1, size.W, 1), FrameRounded, 0)
		for y := 0; y < size.H; y++ {
			if got, want := StringWidth(c.RowString(y)), size.W; got != want {
				t.Errorf("%dx%d 第 %d 行显示宽度 %d ≠ %d：%q",
					size.W, size.H, y, got, want, c.RowString(y))
			}
		}
	}
}

// TestSetRejectsControlAndZeroWidth 验证非法字符被拒绝且计入诊断。
func TestSetRejectsControlAndZeroWidth(t *testing.T) {
	c := New(10, 1)
	if c.Set(0, 0, '\x00', 0) {
		t.Error("NUL 应被拒绝")
	}
	if c.Set(1, 0, '\x07', 0) {
		t.Error("响铃字符不应被当作可见字符画进画布")
	}
	if c.Set(2, 0, '\u0301', 0) {
		t.Error("零宽组合记号应被忽略")
	}
	if !c.Set(4, 0, '好', 0) {
		t.Error("正常宽字符应写入成功")
	}
	if c.Diag.Overflow != 3 {
		t.Errorf("三次非法写入应记入 Overflow=3，实际 %d", c.Diag.Overflow)
	}
}

// TestCollisionDetected 验证覆盖已有内容会被抓出来。
//
// 这是"跨栏串字/边框被切断"的报警器：v2.1.0 这类事故只能靠用户截图发现。
func TestCollisionDetected(t *testing.T) {
	c := New(20, 3)
	c.DrawBox(geometry.NewRect(0, 0, 20, 3), FrameRounded, 0)
	if c.Diag.Collisions != 0 {
		t.Fatalf("首次画边框不应有覆盖，实际 %d", c.Diag.Collisions)
	}
	// 直接在边框行上写文本：必然覆盖 '─'。
	c.Text(2, 0, "标题", 1)
	if c.Diag.Collisions == 0 {
		t.Fatal("覆盖边框必须被诊断为 Collisions")
	}
	if len(c.Diag.Events) == 0 {
		t.Fatal("诊断事件应被记录，便于定位是谁画的")
	}
	if c.Diag.Events[0].Kind != DiagCollision {
		t.Errorf("事件类别应为 DiagCollision，实际 %v", c.Diag.Events[0].Kind)
	}
}

// TestStyleRunsMerge 验证相邻同样式合并成一段、样式切换处断开。
//
// 这是"带样式的宽度不需要再反推"的机制：画布按格子记样式，输出时按段上色。
func TestStyleRunsMerge(t *testing.T) {
	c := New(6, 1)
	c.Text(0, 0, "ab", 1)
	c.Text(2, 0, "cd", 2)
	c.Text(4, 0, "ef", 1)

	pal := &recordingPalette{}
	out := c.Render(pal)
	if out != "abcdef" {
		t.Fatalf("纯文本应可拼接还原，实际 %q", out)
	}
	// 期望三段：样式1("ab")、样式2("cd")、样式1("ef")。
	want := []string{"1:ab", "2:cd", "1:ef"}
	if len(pal.calls) != len(want) {
		t.Fatalf("应上色 %d 段，实际 %d 段：%v", len(want), len(pal.calls), pal.calls)
	}
	for i := range want {
		if pal.calls[i] != want[i] {
			t.Errorf("第 %d 段应为 %q，实际 %q", i, want[i], pal.calls[i])
		}
	}
}

// recordingPalette 记录每次上色调用，用于断言分段与样式下标。
type recordingPalette struct{ calls []string }

func (p *recordingPalette) Render(st StyleID, text string) string {
	p.calls = append(p.calls, string(rune('0'+int(st)))+":"+text)
	return text
}
func (p *recordingPalette) Len() int { return 4 }

// TestRenderWidthMatchesLipgloss 验证画布宽度与 lipgloss 的宽度口径一致。
//
// 项目约定"量宽度只信 lipgloss.Width / runewidth"（SKILL conventions 第 1 条），
// 因此画布输出的每行都必须被 lipgloss 认为正好 W 列，否则两个口径就分裂了。
func TestRenderWidthMatchesLipgloss(t *testing.T) {
	c := New(30, 5)
	c.DrawBox(c.Bounds(), FrameDouble, 1)
	c.Text(2, 2, "中文与 ASCII 混排 test", 2)
	c.Text(2, 3, "宽字符：汉字占两列", 3)
	out := c.Render(PlainPalette{})
	assertFrameShape(t, "宽度口径", out, 30, 5)
}

// TestResetReusesBuffer 验证画布可复用（动画帧每秒重绘数次）。
func TestResetReusesBuffer(t *testing.T) {
	c := New(80, 24)
	c.Text(0, 0, "第一帧", 1)
	if c.Diag.Clean() != true {
		t.Fatal("正常写入不应产生诊断")
	}
	c.Reset(80, 24)
	if got := c.String(); strings.Contains(got, "第一帧") {
		t.Error("Reset 应清空内容")
	}
	if !c.Diag.Clean() {
		t.Error("Reset 应清空诊断")
	}
	if c.Clip() != c.Bounds() {
		t.Error("Reset 应恢复裁剪区")
	}
	// 换尺寸也必须正确重排。
	c.Reset(10, 2)
	assertFrameShape(t, "换尺寸后", c.Render(PlainPalette{}), 10, 2)
}

// TestZeroSizeCanvas 验证 0 尺寸不 panic 且输出空串。
func TestZeroSizeCanvas(t *testing.T) {
	for _, s := range []geometry.Size{{W: 0, H: 0}, {W: 0, H: 5}, {W: 5, H: 0}, {W: -3, H: -4}} {
		c := New(s.W, s.H)
		if out := c.Render(PlainPalette{}); out != "" {
			t.Errorf("%dx%d 应输出空串，实际 %q", s.W, s.H, out)
		}
		if c.Set(0, 0, 'X', 0) {
			t.Errorf("%dx%d 不应接受任何写入", s.W, s.H)
		}
		c.Clear()
		c.Reset(0, 0)
	}
}

// BenchmarkRender 给出单帧渲染的预算基线。
//
// 动画帧每 120ms 一次（v2.1.0 的 animMsg 周期），160×60 是宽终端的上界，
// 因此单帧必须远低于 120ms；这里给的是"有没有数量级问题"的护栏。
func BenchmarkRender(b *testing.B) {
	c := New(160, 60)
	full := c.Bounds()
	header, rest := full.CutTop(1)
	body, footer := rest.CutBottom(1)
	left, tmp := body.CutLeft(34)
	center, right := tmp.CutRight(30)
	draw := func() {
		c.Clear()
		c.Text(header.X, header.Y, "KQFLOW · 2026-10-03 周六 · 日界线 04:00 · 今日专注 2h13m", 2)
		c.DrawBox(left, FrameRounded, 3)
		c.DrawBox(center, FrameRounded, 1)
		c.DrawBox(right, FrameRounded, 3)
		c.Text(footer.X, footer.Y, "tab 切换栏位 · j/k 移动 · space 勾选 · ? 帮助 · q 退出", 2)
		for _, r := range []geometry.Rect{left, center, right} {
			inner := r.Inset(2, 1)
			restore := c.PushClip(inner)
			for i := 0; i < inner.H; i++ {
				c.Text(inner.X, inner.Y+i, "中文内容与 ASCII 混排的一行示例文本", 2)
			}
			restore()
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		draw()
		_ = c.Render(PlainPalette{})
	}
}
