package tile

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// stub 是一个最小可用的组件。
type stub struct {
	title   string
	renders int
	// lastRect 记录它拿到内容区矩形，用于断言"内容区是算好的、合法的"。
	lastRect geometry.Rect
	lastCtx  plugin.RenderCtx
}

func (s *stub) Title() string { return s.title }
func (s *stub) Render(ctx plugin.RenderCtx) {
	s.renders++
	s.lastRect = ctx.Rect
	s.lastCtx = ctx
	// 故意写一行超长内容：它必须被裁剪区挡住，而不是画到边框上。
	ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y, strings.Repeat("X", 200), StyleMuted)
}
func (s *stub) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }

func newSlot(id string, a geometry.Anchor) (Slot, *stub) {
	s := &stub{title: id}
	return Slot{PluginID: id, PackID: "pack", Name: id, Anchor: a, Component: s}, s
}

// TestRegistryPlaceAndLookup 覆盖安置与两种查询。
func TestRegistryPlaceAndLookup(t *testing.T) {
	r := NewRegistry()
	slot, _ := newSlot("a", geometry.AnchorLeftTop)
	if _, hadOld := r.Place(slot); hadOld {
		t.Error("首次安置不应报告替换")
	}
	if got, ok := r.ByID("a"); !ok || got.PluginID != "a" {
		t.Errorf("ByID 应找到 a，实际 %+v ok=%v", got, ok)
	}
	if got, ok := r.At(geometry.AnchorLeftTop); !ok || got.PluginID != "a" {
		t.Error("At 应找到左上槽位的磁贴")
	}
	if r.Count() != 1 {
		t.Errorf("应计数 1，实际 %d", r.Count())
	}
	if got := r.CountIn("left"); got != 1 {
		t.Errorf("左栏应有 1 块，实际 %d", got)
	}
	if got := r.CountIn("dock"); got != 0 {
		t.Errorf("停靠区应有 0 块，实际 %d", got)
	}
}

// TestRegistryReplaceReportsOld 验证重复安置会**报告被替换者**。
//
// 静默覆盖会让"我的磁贴不见了"永远没有答案，因此必须有返回值让调用方报出来。
func TestRegistryReplaceReportsOld(t *testing.T) {
	r := NewRegistry()
	first, _ := newSlot("a", geometry.AnchorLeftTop)
	r.Place(first)

	second, _ := newSlot("b", geometry.AnchorLeftTop)
	old, hadOld := r.Place(second)
	if !hadOld || old.PluginID != "a" {
		t.Fatalf("应报告被替换的 a，实际 %+v hadOld=%v", old, hadOld)
	}
	// 被替换者的 ID 索引必须清掉，否则 ByID 会指向一个不在任何槽位上的磁贴。
	if _, ok := r.ByID("a"); ok {
		t.Error("被替换的磁贴不应还能通过 ID 查到")
	}
	if got, ok := r.ByID("b"); !ok || got.Anchor != geometry.AnchorLeftTop {
		t.Error("新磁贴应能被查到")
	}
	if r.Count() != 1 {
		t.Errorf("替换后仍应只有 1 块，实际 %d", r.Count())
	}
}

// TestRegistryRejectsUnsetAnchor 验证没有具体槽位的安置被拒绝。
func TestRegistryRejectsUnsetAnchor(t *testing.T) {
	r := NewRegistry()
	slot, _ := newSlot("a", geometry.AnchorUnset)
	if _, ok := r.Place(slot); ok {
		t.Error("AnchorUnset 不是具体槽位，安置应被拒绝")
	}
	if r.Count() != 0 {
		t.Errorf("被拒绝后不应有磁贴，实际 %d", r.Count())
	}
}

// TestRegistryAnchorsAreDeterministicAndDeduped 验证槽位列表可复现且不重复。
func TestRegistryAnchorsAreDeterministicAndDeduped(t *testing.T) {
	r := NewRegistry()
	a, _ := newSlot("a", geometry.AnchorLeftTop)
	b, _ := newSlot("b", geometry.AnchorLeftBottom)
	r.Place(a)
	r.Place(b)
	r.Place(a) // 重复安置同一槽位

	got := r.Anchors()
	if len(got) != 2 {
		t.Fatalf("槽位列表应去重为 2，实际 %d：%v", len(got), got)
	}
	if got[0] != geometry.AnchorLeftTop || got[1] != geometry.AnchorLeftBottom {
		t.Errorf("槽位顺序应为左上、左下，实际 %v", got)
	}
	// 多次调用结果必须一致。
	for i := 0; i < 5; i++ {
		again := r.Anchors()
		if len(again) != len(got) || again[0] != got[0] || again[1] != got[1] {
			t.Fatalf("第 %d 次调用结果不同：%v vs %v", i+2, again, got)
		}
	}
}

// TestSetStatesReflectsFocusAndBorrow 验证状态由引擎统一计算。
//
// 状态决定"谁收按键"，因此不能由磁贴自己判断——
// v2.1.0 里"同一个动作写在两处就会按一下触发"是同一类问题的前车之鉴。
func TestSetStatesReflectsFocusAndBorrow(t *testing.T) {
	r := NewRegistry()
	a, _ := newSlot("a", geometry.AnchorLeftTop)
	b, _ := newSlot("b", geometry.AnchorLeftBottom)
	r.Place(a)
	r.Place(b)

	r.SetStates(geometry.AnchorLeftTop, false)
	if got, _ := r.At(geometry.AnchorLeftTop); got.State != plugin.StateFocused {
		t.Errorf("焦点槽位应为 Focused，实际 %v", got.State)
	}
	if got, _ := r.At(geometry.AnchorLeftBottom); got.State != plugin.StateIdle {
		t.Errorf("非焦点槽位应为 Idle，实际 %v", got.State)
	}

	// 有人借调舞台：**所有**磁贴都只渲染不收键。
	r.SetStates(geometry.AnchorLeftTop, true)
	for _, a := range r.Anchors() {
		if got, _ := r.At(a); got.State != plugin.StateOccupied {
			t.Errorf("借调期间 %v 应为 Occupied，实际 %v", a, got.State)
		}
	}
}

// TestDrawTileNeverSpillsOutsideFrame 验证磁贴内容画不进边框、也画不出自己的矩形。
//
// 这是"卡片框大小不定"（req.md 列出的痛点）的机制化断言：
// 外框尺寸来自布局，内容区由这里算，磁贴只能画内容区。
func TestDrawTileNeverSpillsOutsideFrame(t *testing.T) {
	c := canvas.New(40, 12)
	// 背景填成 '.'，这样"谁画到哪"一目了然。
	c.Fill(c.Bounds(), '.', 0)

	slot, comp := newSlot("t", geometry.AnchorLeftTop)
	outer := geometry.NewRect(5, 2, 20, 8)
	DrawTile(c, outer, &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)

	if comp.renders != 1 {
		t.Fatalf("组件应被渲染一次，实际 %d", comp.renders)
	}
	// 内容区必须落在外框之内，且已经去掉边框与标题行。
	if !outer.ContainsRect(comp.lastRect) {
		t.Errorf("内容区 %v 应在外框 %v 之内", comp.lastRect, outer)
	}
	if comp.lastRect.W != outer.W-2 {
		t.Errorf("内容区宽度应为外框减边框（%d），实际 %d", outer.W-2, comp.lastRect.W)
	}
	if comp.lastRect.Y <= outer.Y {
		t.Errorf("内容区应在标题行之下：内容 y=%d 外框 y=%d", comp.lastRect.Y, outer.Y)
	}
	// 外框之外一个 X 都不该有。
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			if c.CellAt(x, y).R == 'X' && !outer.Contains(x, y) {
				t.Fatalf("磁贴内容画到了外框之外 (%d,%d)", x, y)
			}
		}
	}
	if c.Diag.Overflow != 0 {
		t.Errorf("不应有越界写入，实际 %d", c.Diag.Overflow)
	}
}

// TestDrawTileClippedContentIsNotACollision 验证"内容写不完"不算布局缺陷。
//
// 内容超出内容区是正常的（磁贴内容由它自己决定），画布会按裁剪区裁掉；
// 只有**越界**与**覆盖**才是布局算错了。把两者混为一谈会让诊断失去意义。
func TestDrawTileClippedContentIsNotACollision(t *testing.T) {
	c := canvas.New(40, 12)
	slot, _ := newSlot("t", geometry.AnchorLeftTop)
	outer := geometry.NewRect(0, 0, 12, 4)
	DrawTile(c, outer, &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)

	if c.Diag.Collisions != 0 {
		t.Errorf("被裁剪不算覆盖，实际 collisions=%d", c.Diag.Collisions)
	}
	if c.Diag.Overflow != 0 {
		t.Errorf("被裁剪不算越界，实际 overflow=%d", c.Diag.Overflow)
	}
}

// TestDrawTileHandlesDegenerateRects 验证退化输入不 panic 也不画坏。
func TestDrawTileHandlesDegenerateRects(t *testing.T) {
	c := canvas.New(20, 5)
	slot, _ := newSlot("t", geometry.AnchorLeftTop)

	cases := []geometry.Rect{
		{},                          // 空
		{X: 0, Y: 0, W: 1, H: 1},    // 1×1
		{X: 0, Y: 0, W: 2, H: 2},    // 刚好装下边框
		{X: 0, Y: 0, W: 3, H: 1},    // 只有一行
		{X: 18, Y: 4, W: 10, H: 10}, // 越出画布
		{X: -3, Y: -2, W: 8, H: 4},  // 起点为负
	}
	for _, outer := range cases {
		DrawTile(c, outer, &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	}
	// 没有 nil 组件也应安全。
	DrawTile(c, geometry.NewRect(0, 0, 10, 3), nil, plugin.RenderCtx{}, canvas.FrameRounded)
	// 组件为 nil 的槽位也应安全。
	DrawTile(c, geometry.NewRect(0, 0, 10, 3), &Slot{}, plugin.RenderCtx{}, canvas.FrameRounded)
}

// TestRenderAllSkipsUnknownAnchors 验证 rectOf 说不出的槽位被跳过。
func TestRenderAllSkipsUnknownAnchors(t *testing.T) {
	c := canvas.New(40, 10)
	r := NewRegistry()
	slot, comp := newSlot("a", geometry.AnchorLeftTop)
	r.Place(slot)

	// rectOf 只认得左上；其它槽位一律返回 false。
	rectOf := func(a geometry.Anchor) (geometry.Rect, bool) {
		if a == geometry.AnchorLeftTop {
			return geometry.NewRect(0, 0, 20, 5), true
		}
		return geometry.Rect{}, false
	}
	r.RenderAll(c, rectOf, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	if comp.renders != 1 {
		t.Errorf("已知槽位应被渲染，实际 %d 次", comp.renders)
	}

	// rectOf 全返回 false：不应渲染也不应 panic。
	comp.renders = 0
	r.RenderAll(c, func(geometry.Anchor) (geometry.Rect, bool) {
		return geometry.Rect{}, false
	}, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	if comp.renders != 0 {
		t.Errorf("槽位不可用时不应渲染，实际 %d 次", comp.renders)
	}
}

// TestDrawTilePassesFocusAndState 验证状态被传给组件（组件据此画高亮）。
func TestDrawTilePassesFocusAndState(t *testing.T) {
	c := canvas.New(20, 6)
	slot, comp := newSlot("t", geometry.AnchorLeftTop)

	slot.State = plugin.StateFocused
	DrawTile(c, geometry.NewRect(0, 0, 12, 4), &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	if !comp.lastCtx.Focus || comp.lastCtx.State != plugin.StateFocused {
		t.Errorf("聚焦状态应传给组件，实际 focus=%v state=%v", comp.lastCtx.Focus, comp.lastCtx.State)
	}

	slot.State = plugin.StateOccupied
	DrawTile(c, geometry.NewRect(0, 0, 12, 4), &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	if comp.lastCtx.Focus || comp.lastCtx.State != plugin.StateOccupied {
		t.Errorf("被占用状态应传给组件，实际 focus=%v state=%v", comp.lastCtx.Focus, comp.lastCtx.State)
	}
}

// TestTitleIsTruncatedNotOverflowing 验证长标题被裁到内容宽度内。
func TestTitleIsTruncatedNotOverflowing(t *testing.T) {
	c := canvas.New(30, 6)
	slot, _ := newSlot("一个非常非常非常长的磁贴标题名字用来试探边框", geometry.AnchorLeftTop)
	outer := geometry.NewRect(0, 0, 14, 4)
	DrawTile(c, outer, &slot, plugin.RenderCtx{Palette: canvas.PlainPalette{}}, canvas.FrameRounded)
	// 边框必须完整（标题若撑破边框，右边界字符会被覆盖）。
	row := c.RowString(0)
	if got := canvas.StringWidth(row); got > 30 {
		t.Fatalf("行宽 %d 超过画布", got)
	}
	if c.Diag.Collisions != 0 {
		t.Errorf("标题不应覆盖边框，实际 collisions=%d（%q）", c.Diag.Collisions, row)
	}
}

// TestStyleIDsAreContiguous 验证样式下标是连续的、从 0 到 StyleCount-1。
//
// 主题包按这些下标布置样式；一旦中间有空洞，两边就会悄悄错位
// （颜色不对但程序不报错，属于最难发现的一类问题）。
func TestStyleIDsAreContiguous(t *testing.T) {
	ids := []canvas.StyleID{
		StyleDefault, StyleAccent, StyleMuted, StyleBorder, StyleBorderFocused,
		StyleBorderDim, StyleTitle, StyleTitleFocused, StyleTitleDim, StyleStatus,
		StyleHintKey, StyleHint, StyleBarFilled, StyleBarEmpty, StyleError,
		StyleWarn, StyleText,
	}
	if len(ids) != StyleCount {
		t.Fatalf("声明的下标数 %d 应等于 StyleCount %d", len(ids), StyleCount)
	}
	seen := map[canvas.StyleID]bool{}
	for i, id := range ids {
		if int(id) != i {
			t.Errorf("第 %d 个下标应为 %d，实际 %d（下标必须连续且从 0 开始）", i, i, id)
		}
		if seen[id] {
			t.Errorf("下标 %d 重复", id)
		}
		seen[id] = true
	}
}
