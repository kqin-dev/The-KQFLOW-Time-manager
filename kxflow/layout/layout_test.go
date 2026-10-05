package layout

import (
	"fmt"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
)

// allSizes 覆盖从 1×1 到 200×60 的尺寸网格。
//
// 穷举而不是抽查，是因为布局的错误恰恰出现在"没人想到的那一档"
// （v2.1.0 的排版问题就只在特定宽度下出现，开发环境复现不了）。
// 200×60 覆盖用户那种宽终端；1×1 覆盖"窗口被拖到极小"。
func allSizes() []geometry.Size {
	var out []geometry.Size
	for w := 1; w <= 200; w += 1 {
		for _, h := range []int{1, 2, 3, 5, 10, 16, 24, 30, 44, 60} {
			out = append(out, geometry.Size{W: w, H: h})
		}
	}
	return out
}

// TestLayoutInvariantsExhaustive 是布局的宪法：在全部尺寸上断言不变量。
//
// 不变量清单：
//  1. 任何矩形宽高都不为负（调用方不必层层判负数）；
//  2. Header/Body/Footer 首尾相接、正好铺满，不重不漏；
//  3. Left/Center/Right 等高等宽拼满 Body，不重不漏；
//  4. 槽位都落在自己栏位之内；
//  5. Primary 落在 Body 之内。
func TestLayoutInvariantsExhaustive(t *testing.T) {
	cfgs := []LayoutConfig{
		DefaultConfig(),
		func() LayoutConfig { c := DefaultConfig(); c.LeftTiles = 1; c.RightTiles = 1; return c }(),
		func() LayoutConfig { c := DefaultConfig(); c.LeftTiles = 2; c.RightTiles = 2; return c }(),
		func() LayoutConfig {
			c := DefaultConfig()
			c.LeftTiles, c.RightTiles, c.DockTiles = 2, 2, 2
			return c
		}(),
		func() LayoutConfig { c := DefaultConfig(); c.DockVisible = false; c.DockTiles = 2; return c }(),
		func() LayoutConfig { c := DefaultConfig(); c.HeaderRows = 2; c.FooterRows = 2; return c }(),
		func() LayoutConfig { c := DefaultConfig(); c.HeaderRows = 3; c.FooterRows = 1; return c }(),
		// 病态输入：上下栏吃掉整屏。
		func() LayoutConfig { c := DefaultConfig(); c.HeaderRows = 100; c.FooterRows = 100; return c }(),
	}

	for ci, cfg := range cfgs {
		for _, size := range allSizes() {
			sh := Layout(size, cfg)
			fail := func(format string, args ...any) {
				t.Helper()
				t.Fatalf("cfg#%d %dx%d：%s", ci, size.W, size.H, fmt.Sprintf(format, args...))
			}

			// 1) 非负。
			for _, r := range rectsOf(sh) {
				if r.W < 0 || r.H < 0 {
					fail("出现负宽高：%v", r)
				}
			}

			// 2) 上中下三段相接且铺满。
			if sh.Bounds.W != size.W || sh.Bounds.H != size.H {
				fail("Bounds 应为 %dx%d，实际 %v", size.W, size.H, sh.Bounds)
			}
			if sh.Header.Y != sh.Bounds.Y {
				fail("Header 应在最上：%v vs %v", sh.Header, sh.Bounds)
			}
			if sh.Header.Y1() != sh.Body.Y {
				fail("Header 与 Body 不相接：%d vs %d", sh.Header.Y1(), sh.Body.Y)
			}
			if sh.Body.Y1() != sh.Footer.Y {
				fail("Body 与 Footer 不相接：%d vs %d", sh.Body.Y1(), sh.Footer.Y)
			}
			if sh.Footer.Y1() != sh.Bounds.Y1() {
				fail("Footer 应到底：%v vs %v", sh.Footer, sh.Bounds)
			}
			if sum := sh.Header.H + sh.Body.H + sh.Footer.H; sum != size.H {
				fail("三段高度和 %d 应等于 %d", sum, size.H)
			}
			// 上栏下栏都必须落在画布内（不能被"撑出去"）。
			if sh.Header.H > size.H {
				fail("Header 高度 %d 超过总高 %d", sh.Header.H, size.H)
			}
			if sh.Header.H+sh.Footer.H > size.H {
				fail("上下栏高度和 %d 超过总高 %d", sh.Header.H+sh.Footer.H, size.H)
			}

			// 3) 三栏铺满 Body。
			if l, c, r := sh.Left.Rect, sh.Center.Rect, sh.Right.Rect; l.W+c.W+r.W != sh.Body.W {
				fail("三栏宽度和 %d 应等于 Body 宽 %d（%v %v %v）", l.W+c.W+r.W, sh.Body.W, l, c, r)
			}
			if sh.Left.Rect.X != sh.Body.X || sh.Center.Rect.X != sh.Left.Rect.X1() ||
				sh.Right.Rect.X != sh.Center.Rect.X1() || sh.Right.Rect.X1() != sh.Body.X1() {
				fail("三栏未首尾相接：%v %v %v（Body %v）", sh.Left.Rect, sh.Center.Rect, sh.Right.Rect, sh.Body)
			}
			for _, col := range []Column{
				sh.Left,
				{Rect: sh.Center.Rect, Slots: [2]geometry.Rect{sh.Center.Stage, sh.Center.Stage}},
				sh.Right,
			} {
				if col.Rect.H != sh.Body.H || col.Rect.Y != sh.Body.Y {
					fail("栏位应等高且与 Body 同起：%v vs %v", col.Rect, sh.Body)
				}
			}

			// 4) 槽位落在栏位内。
			//
			// 只对有面积的槽位断言：零尺寸的槽位在几何上"位置"没有意义
			// （v2.1.0 的教训之一就是"按宽度算出来的数据在骗自己"，
			// 因此这里宁可跳过一个无意义的断言，也不写一个会误报的断言）。
			for i := 0; i < 2; i++ {
				for _, slot := range []geometry.Rect{
					sh.Left.Slot(i), sh.Right.Slot(i), sh.Center.Slot(i),
				} {
					if slot.Empty() {
						continue
					}
					if !sh.Body.ContainsRect(slot) {
						fail("槽位 %d 越出 Body：%v vs %v", i, slot, sh.Body)
					}
				}
			}
			if !sh.Body.ContainsRect(sh.Center.Stage) {
				fail("Stage 应在 Body 内：%v vs %v", sh.Center.Stage, sh.Body)
			}

			// 5) Primary 落在 Body 内。
			if !sh.Body.ContainsRect(sh.Primary) {
				fail("Primary 应在 Body 内：%v vs %v", sh.Primary, sh.Body)
			}
		}
	}
}

// TestSingleTileIsFullBar 验证 req.md 的硬规则：单个磁贴渲染为长条。
func TestSingleTileIsFullBar(t *testing.T) {
	sh := Layout(geometry.Size{W: 120, H: 40}, func() LayoutConfig {
		c := DefaultConfig()
		c.LeftTiles, c.RightTiles = 1, 1
		return c
	}())
	if sh.Left.Slot(0) != sh.Left.Rect || sh.Left.Slot(1) != sh.Left.Rect {
		t.Errorf("单磁贴时两个槽位都应指向整栏：%v / %v vs %v",
			sh.Left.Slot(0), sh.Left.Slot(1), sh.Left.Rect)
	}
	if sh.Right.Slot(0) != sh.Right.Rect {
		t.Errorf("右栏同理：%v vs %v", sh.Right.Slot(0), sh.Right.Rect)
	}
	// 0 个磁贴时槽位也等于整栏（调用方不会画它，但几何上必须自洽）。
	empty := Layout(geometry.Size{W: 120, H: 40}, DefaultConfig())
	if empty.Left.Slot(0) != empty.Left.Rect {
		t.Errorf("0 磁贴时槽位应等于整栏：%v vs %v", empty.Left.Slot(0), empty.Left.Rect)
	}
}

// TestTwoTilesSplitEvenly 验证两个磁贴各占一半，且不重不漏。
//
// 注意"装不下"的情形：栏位太矮时两个槽位**都判空**，而不是重叠。
// 判空的语义是明确的（这个尺寸下摆不开两块），而重叠会让两块磁贴
// 互相覆盖边框——那在画布诊断里是一串"覆盖已有内容"。
func TestTwoTilesSplitEvenly(t *testing.T) {
	emptyPairs, splitPairs := 0, 0
	for h := 4; h <= 60; h++ {
		sh := Layout(geometry.Size{W: 120, H: h}, func() LayoutConfig {
			c := DefaultConfig()
			c.LeftTiles, c.RightTiles = 2, 2
			return c
		}())
		top, bottom := sh.Left.Slot(0), sh.Left.Slot(1)
		if top.Empty() && bottom.Empty() {
			// 装不下：两者必须**同时**为空。只空一个会让"用户只配了一块"
			// 与"这个尺寸摆不开两块"在界面上无法区分。
			emptyPairs++
			continue
		}
		if top.Empty() != bottom.Empty() {
			t.Fatalf("高 %d：两个槽位应当同为可用或同为空，实际 %v / %v", h, top, bottom)
		}
		splitPairs++
		if top.H+bottom.H != sh.Left.Rect.H {
			t.Fatalf("高 %d：两槽高度和 %d 应等于栏高 %d", h, top.H+bottom.H, sh.Left.Rect.H)
		}
		if top.Y1() != bottom.Y {
			t.Fatalf("高 %d：两槽不相接：%d vs %d", h, top.Y1(), bottom.Y)
		}
		if diff := top.H - bottom.H; diff > 1 || diff < 0 {
			t.Fatalf("高 %d：两槽高度差应为 0 或 1（上多一行），实际 %d（%d vs %d）",
				h, diff, top.H, bottom.H)
		}
		if top.H < MinTileH || bottom.H < MinTileH {
			t.Fatalf("高 %d：可用的槽位不该小于最小磁贴高度 %d（%d / %d）",
				h, MinTileH, top.H, bottom.H)
		}
	}
	if emptyPairs == 0 || splitPairs == 0 {
		t.Fatalf("这个用例应当同时覆盖「摆得开」与「摆不开」两种情形，实际 空=%d 均分=%d",
			emptyPairs, splitPairs)
	}
}

// TestDegenerateSlotsNeverOverlap 是"槽位重叠"那次事故的墓碑。
//
// 曾经 splitEven 在矩形太矮时让两个槽位都指向整块，注释里还写着
// "不会出现负数或重叠"——**恰恰就是重叠**，实测在 40×10 的终端下
// 画布报出 62 次"覆盖已有内容"。这条测试直接断言"要么都有面积且不相交、
// 要么都空"，把那种写法彻底堵死。
func TestDegenerateSlotsNeverOverlap(t *testing.T) {
	overlaps := 0
	for w := 1; w <= 200; w += 7 {
		for h := 1; h <= 40; h++ {
			for _, tiles := range []int{2, 3} {
				cfg := DefaultConfig()
				cfg.LeftTiles, cfg.RightTiles, cfg.DockTiles = tiles, tiles, tiles
				sh := Layout(geometry.Size{W: w, H: h}, cfg)
				pairs := [][2]geometry.Rect{
					{sh.Left.Slot(0), sh.Left.Slot(1)},
					{sh.Right.Slot(0), sh.Right.Slot(1)},
				}
				if sh.Center.DockVisible {
					pairs = append(pairs, [2]geometry.Rect{sh.Center.Slot(0), sh.Center.Slot(1)})
				}
				for _, p := range pairs {
					a, b := p[0], p[1]
					if a.Empty() && b.Empty() {
						continue
					}
					if a.Empty() != b.Empty() {
						t.Fatalf("%dx%d：两槽应同为可用或同为空：%v / %v", w, h, a, b)
					}
					// 不相交时 Intersect 返回零值矩形。
					if got := a.Intersect(b); got.W != 0 || got.H != 0 {
						t.Fatalf("%dx%d：两个槽位重叠了：%v 与 %v 交集 %v", w, h, a, b, got)
					}
					if a.Intersects(b) {
						t.Fatalf("%dx%d：Intersects 应报告不相交：%v 与 %v", w, h, a, b)
					}
					overlaps++
				}
			}
		}
	}
	if overlaps == 0 {
		t.Fatal("这个用例没有扫到任何可用槽位对，等于没验证")
	}
}

// TestDockBehavior 验证停靠区的三种状态，以及"隐藏时主控区独占中栏"。
func TestDockBehavior(t *testing.T) {
	base := geometry.Size{W: 120, H: 40}

	// ① 隐藏：主控区独占，停靠区为空。
	c := DefaultConfig()
	c.DockVisible = false
	c.DockTiles = 2
	sh := Layout(base, c)
	if sh.Center.Dock.H != 0 {
		t.Errorf("停靠区隐藏时应为空矩形，实际 %v", sh.Center.Dock)
	}
	if sh.Center.Stage != sh.Center.Rect {
		t.Errorf("停靠区隐藏时主控区应独占中栏：%v vs %v", sh.Center.Stage, sh.Center.Rect)
	}

	// ② 可见但没有磁贴：几何上等同隐藏（不留空条）。
	c2 := DefaultConfig()
	c2.DockTiles = 0
	sh2 := Layout(base, c2)
	if sh2.Center.Stage != sh2.Center.Rect {
		t.Errorf("停靠区无磁贴时主控区应独占中栏：%v vs %v", sh2.Center.Stage, sh2.Center.Rect)
	}

	// ③ 可见且有磁贴：主控区在上、停靠区在下，且停靠区不超过中栏一半。
	c3 := DefaultConfig()
	c3.DockTiles = 2
	sh3 := Layout(base, c3)
	if sh3.Center.Dock.Empty() {
		t.Fatal("停靠区有磁贴时应占位置")
	}
	if sh3.Center.Stage.Y1() != sh3.Center.Dock.Y {
		t.Errorf("主控区与停靠区应相接：%d vs %d", sh3.Center.Stage.Y1(), sh3.Center.Dock.Y)
	}
	if sh3.Center.Stage.H+sh3.Center.Dock.H != sh3.Center.Rect.H {
		t.Errorf("主控区+停靠区高度 %d 应等于中栏高 %d",
			sh3.Center.Stage.H+sh3.Center.Dock.H, sh3.Center.Rect.H)
	}
	if sh3.Center.Dock.H > sh3.Center.Rect.H/2+1 {
		t.Errorf("停靠区不应超过中栏一半，实际 %d / %d", sh3.Center.Dock.H, sh3.Center.Rect.H)
	}
	if sh3.Center.Stage.Empty() {
		t.Error("有停靠区时主控区也不能被挤没")
	}
}

// TestDockIsLowerHalfOfCenter 固化停靠区的规则（按用户实机反馈确定）。
//
//	0 个磁贴 → 不开停靠区，整个中栏都给看板
//	1 个磁贴 → 占**下半中栏整条**
//	2 个磁贴 → **左右各半**（下左 / 下右），而不是上下叠着
//
// 用户的反馈原话是「有 0 个磁贴那么全部都是中栏的看板，有 1 个磁贴那么
// 半个中栏下都是磁贴，有 2 个磁贴就是中栏的下左和下右（而不是叠着放）」。
func TestDockIsLowerHalfOfCenter(t *testing.T) {
	base := DefaultConfig()

	// 0 个磁贴：停靠区不可见，主控区独占中栏。
	zero := Layout(geometry.Size{W: 120, H: 40}, base)
	if zero.Center.DockVisible {
		t.Error("没有停靠磁贴时不该开停靠区")
	}
	if zero.Center.Stage.H != zero.Center.Rect.H {
		t.Errorf("没有停靠区时主控区应独占中栏：%d vs %d", zero.Center.Stage.H, zero.Center.Rect.H)
	}

	// 1 个磁贴：占下半中栏的整条。
	one := base
	one.DockTiles = 1
	sh1 := Layout(geometry.Size{W: 120, H: 40}, one)
	if !sh1.Center.DockVisible {
		t.Fatal("有停靠磁贴时应开停靠区")
	}
	if sh1.Center.Dock.W != sh1.Center.Rect.W {
		t.Errorf("单个停靠磁贴应占整条宽度：%d vs %d", sh1.Center.Dock.W, sh1.Center.Rect.W)
	}
	// 上下各半（奇数行时下半少一行，因为 want = H/2）。
	if diff := sh1.Center.Stage.H - sh1.Center.Dock.H; diff > 1 || diff < 0 {
		t.Errorf("停靠区应约占中栏一半：主控 %d 停靠 %d", sh1.Center.Stage.H, sh1.Center.Dock.H)
	}
	if sh1.Center.Stage.Y1() != sh1.Center.Dock.Y {
		t.Errorf("主控区与停靠区应上下相接：%d vs %d", sh1.Center.Stage.Y1(), sh1.Center.Dock.Y)
	}

	// 2 个磁贴：左右各半，高度相同。
	two := base
	two.DockTiles = 2
	sh2 := Layout(geometry.Size{W: 120, H: 40}, two)
	a, b := sh2.Center.Slot(0), sh2.Center.Slot(1)
	if a.Empty() || b.Empty() {
		t.Fatalf("停靠区两个槽位都应有面积：%v %v", a, b)
	}
	if a.W+b.W != sh2.Center.Dock.W {
		t.Errorf("停靠槽宽度和 %d 应等于停靠区宽 %d", a.W+b.W, sh2.Center.Dock.W)
	}
	if a.X1() != b.X {
		t.Errorf("停靠槽应左右相接：%d vs %d", a.X1(), b.X)
	}
	if a.H != b.H || a.Y != b.Y {
		t.Errorf("左右两个停靠槽应同高同起：%v 与 %v", a, b)
	}
	if diff := a.W - b.W; diff > 1 || diff < 0 {
		t.Errorf("停靠槽宽度差应为 0 或 1，实际 %d（%d vs %d）", diff, a.W, b.W)
	}
	if a.Intersects(b) {
		t.Errorf("停靠槽不该重叠：%v 与 %v", a, b)
	}
}

// TestWidthBreakpoints 固化三档宽度断点（沿用 v2.1.0 的实测值）。
//
// 注意：断点是**候选值**，不是最终值。中栏不足保底宽度时会从两侧借，
// 于是实际宽度可能小于断点值。这是 v2.1.0 的既有行为，也是对的——
// 中栏挤到放不下一行中文比侧栏窄几列严重得多。
// 因此这里分别在"不需要借"与"需要借"两种情形下断言。
func TestWidthBreakpoints(t *testing.T) {
	// ① 不需要借：三档断点值必须原样生效。
	noBorrow := []struct {
		totalW int
		wantL  int
		wantR  int
	}{
		{200, 34, 30},
		{160, 34, 30},
		{140, 34, 30},
	}
	for _, c := range noBorrow {
		sh := Layout(geometry.Size{W: c.totalW, H: 30}, DefaultConfig())
		if sh.Left.Rect.W != c.wantL || sh.Right.Rect.W != c.wantR {
			t.Errorf("%d 列：左右栏应为断点值 %d/%d，实际 %d/%d",
				c.totalW, c.wantL, c.wantR, sh.Left.Rect.W, sh.Right.Rect.W)
		}
	}

	// ② 需要借：中栏被顶到保底 48（90 列正好算得出来）。注意左右两侧
	// 借出的量不一定相等，因此只断言"两边都比断点值小"。
	sh := Layout(geometry.Size{W: 90, H: 30}, DefaultConfig())
	if sh.Center.Rect.W != 48 {
		t.Errorf("90 列时中栏应正好被顶到保底 48，实际 %d", sh.Center.Rect.W)
	}
	if sh.Left.Rect.W >= 28 || sh.Right.Rect.W >= 24 {
		t.Errorf("90 列时应从两侧借过宽度，实际 %d/%d", sh.Left.Rect.W, sh.Right.Rect.W)
	}

	// ③ 极窄：三档断点仍决定"谁先被压缩"的相对关系——
	// 到 90 列以下用更窄的一档，所以左栏不该比中档时更宽。
	wide := Layout(geometry.Size{W: 95, H: 30}, DefaultConfig())
	narrow := Layout(geometry.Size{W: 85, H: 30}, DefaultConfig())
	if narrow.Left.Rect.W > wide.Left.Rect.W {
		t.Errorf("更窄的终端不该给出更宽的左栏：%d（85 列）> %d（95 列）",
			narrow.Left.Rect.W, wide.Left.Rect.W)
	}
}

// TestCenterKeepsMinimum 验证中栏保底 48 列——不足时从两侧借。
//
// 这是"文字被折得七零八落"的防线：中栏太窄，帮助/设置/历史都会难看。
func TestCenterKeepsMinimum(t *testing.T) {
	for _, w := range []int{60, 62, 70, 80, 90, 100, 110} {
		sh := Layout(geometry.Size{W: w, H: 30}, DefaultConfig())
		// 借不动的时候（终端实在太窄）允许不足，但不允许比"能借到的"更少。
		if sh.Center.Rect.W < 48 && sh.Left.Rect.W > DefaultSideWidths().MinLeft {
			t.Errorf("%d 列：中栏只有 %d 列，但左栏还有 %d 列可借（保底 %d）",
				w, sh.Center.Rect.W, sh.Left.Rect.W, DefaultSideWidths().MinLeft)
		}
		if sh.Center.Rect.W < 0 {
			t.Fatalf("%d 列：中栏宽度为负", w)
		}
	}
	// 120 列时中栏应当宽裕。
	sh := Layout(geometry.Size{W: 120, H: 30}, DefaultConfig())
	if sh.Center.Rect.W < 48 {
		t.Errorf("120 列下中栏应 ≥48，实际 %d", sh.Center.Rect.W)
	}
}

// TestPrimarySwitchesToFullWidthWhenNarrow 验证窄终端下整页内容改用整行。
//
// 这是"窄终端里中文被折碎"的对策：与其挤在中栏，不如让左右栏暂时让位。
func TestPrimarySwitchesToFullWidthWhenNarrow(t *testing.T) {
	// 很窄：中栏必然小于最小正文宽度，Primary 应变成整行。
	narrow := Layout(geometry.Size{W: 60, H: 30}, DefaultConfig())
	if narrow.Center.Rect.W >= DefaultConfig().MinContentWidth {
		t.Skip("这个宽度下中栏已经够宽，用例前提不成立")
	}
	if narrow.Primary.W <= narrow.Center.Rect.W {
		t.Errorf("窄终端下 Primary 应比中栏更宽（改用整行），实际 %d vs %d",
			narrow.Primary.W, narrow.Center.Rect.W)
	}
	if narrow.Primary.W != narrow.Body.W {
		t.Errorf("窄终端下 Primary 应等于整行宽 %d，实际 %d", narrow.Body.W, narrow.Primary.W)
	}

	// 宽终端：Primary 就是中栏。
	wide := Layout(geometry.Size{W: 160, H: 44}, DefaultConfig())
	if wide.Primary != wide.Center.Rect {
		t.Errorf("宽终端下 Primary 应为中栏，实际 %v vs %v", wide.Primary, wide.Center.Rect)
	}
}

// TestLayoutIsPure 验证布局是纯函数：同样输入必得同样结果。
//
// 不纯的布局会让"同一个终端尺寸下界面偶尔不一样"，而用户无从解释。
func TestLayoutIsPure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LeftTiles, cfg.RightTiles, cfg.DockTiles = 2, 2, 2
	cfg.HeaderRows = 2
	size := geometry.Size{W: 133, H: 41}
	first := Layout(size, cfg)
	for i := 0; i < 20; i++ {
		again := Layout(size, cfg)
		if again != first {
			t.Fatalf("第 %d 次布局结果与首次不同：\n%+v\n%+v", i+2, first, again)
		}
	}
}

// TestZeroAndNegativeInputs 验证退化输入不 panic、不出负数。
func TestZeroAndNegativeInputs(t *testing.T) {
	sizes := []geometry.Size{
		{W: 0, H: 0}, {W: 0, H: 10}, {W: 10, H: 0},
		{W: -5, H: -5}, {W: 1, H: 1},
	}
	for _, s := range sizes {
		sh := Layout(s, DefaultConfig()) // 不 panic 即为合格
		for _, r := range rectsOf(sh) {
			if r.W < 0 || r.H < 0 {
				t.Errorf("%v：出现负宽高 %v", s, r)
			}
		}
		if sh.Bounds.W < 0 || sh.Bounds.H < 0 {
			t.Errorf("%v：Bounds 为负 %v", s, sh.Bounds)
		}
	}
}

// rectsOf 列出一次布局里的全部矩形，供统一断言。
func rectsOf(sh Shell) []geometry.Rect {
	out := []geometry.Rect{
		sh.Bounds, sh.Header, sh.Body, sh.Footer, sh.Primary,
		sh.Left.Rect, sh.Left.Slots[0], sh.Left.Slots[1],
		sh.Right.Rect, sh.Right.Slots[0], sh.Right.Slots[1],
		sh.Center.Rect, sh.Center.Stage, sh.Center.Dock,
		sh.Center.DockSlots[0], sh.Center.DockSlots[1],
	}
	return out
}
