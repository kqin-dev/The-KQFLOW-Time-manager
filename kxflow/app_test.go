package kxflow

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/layout"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

var testEngine = semver.MustParse("0.1.0")

// ---------- 测试用的假内核与假磁贴 ----------

// fakeKernel 提供 LOGO 与一个可辨识的 Dashboard。
type fakeKernel struct{}

func (fakeKernel) Title() string                                      { return "内核" }
func (fakeKernel) Render(plugin.RenderCtx)                            {}
func (fakeKernel) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }
func (fakeKernel) PowerBy() string                                    { return "Power by KXFLOW" }
func (fakeKernel) BoardOptions() []plugin.BoardOption                 { return nil }

func (fakeKernel) Dashboard() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "看板",
		RenderFn: func(ctx plugin.RenderCtx) {
			ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y, canvas.Truncate("KQFLOW 看板", ctx.Rect.W), 1)
		},
	}
}

// fakeTile 是一个可观测的磁贴：记录渲染与按键次数，并可按 enter 借调一层。
type fakeTile struct {
	id      string
	name    string
	renders int
	keys    []string
	// anchor 是它声明的槽位偏好；AnchorUnset 表示"随便放，按优先级来"。
	anchor geometry.Anchor
	// priority 是槽位优先级。
	priority int
	// borrowOn 里的按键会触发借调。
	borrowOn string
	// selectOn 里的按键会上报选中上下文。
	selectOn string
}

func newFakeTile(id, name string) *fakeTile {
	return &fakeTile{id: id, name: name, anchor: geometry.AnchorUnset}
}

// newFakeTileAt 造一个**明确声明槽位**的磁贴（用于验证安置规则）。
func newFakeTileAt(id, name string, a geometry.Anchor) *fakeTile {
	t := newFakeTile(id, name)
	t.anchor = a
	return t
}

func (t *fakeTile) Title() string { return t.name }

func (t *fakeTile) Render(ctx plugin.RenderCtx) {
	t.renders++
	ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y, canvas.Truncate(t.name+" 内容", ctx.Rect.W), 2)
}

func (t *fakeTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	t.keys = append(t.keys, ev.Key)
	switch {
	case t.borrowOn != "" && ev.Key == t.borrowOn:
		// 借调一层。多级菜单（栈更深）由视图自己再借调来实现——
		// 那是视图的事（它知道"下一步是什么"），磁贴不需要知道。
		return plugin.Borrow(&plugin.TestView{
			ViewName: t.name + "视图",
			Lines:    []string{t.name + " 借调的界面"},
		})
	case t.selectOn != "" && ev.Key == t.selectOn:
		return plugin.Select(plugin.Selection{Kind: "fake", ID: t.id, Title: t.name})
	}
	return plugin.None()
}

// fakePack 装配若干假磁贴。
type fakePack struct {
	id       string
	members  []plugin.Plugin
	provides []string
	requires []string
	enabled  bool
	kernel   bool
	tiles    []*fakeTile
}

func newFakePack(id string, tiles ...*fakeTile) *fakePack {
	p := &fakePack{id: id, enabled: true, tiles: tiles}
	for _, t := range tiles {
		p.members = append(p.members, &tilePluginAdapter{t: t})
	}
	return p
}

func (p *fakePack) ID() string              { return p.id }
func (p *fakePack) Name() string            { return p.id }
func (p *fakePack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *fakePack) Enabled() bool           { return p.enabled }
func (p *fakePack) Provides() []string      { return p.provides }
func (p *fakePack) Requires() []string      { return p.requires }
func (p *fakePack) Conflicts() []string     { return nil }
func (p *fakePack) EngineAPI() semver.Range { return semver.MustRange(">=0.1 <0.2") }
func (p *fakePack) Members() []plugin.Plugin {
	return p.members
}
func (p *fakePack) Assemble(s svc.Services) (plugin.Assembled, error) {
	comps := make([]plugin.Component, 0, len(p.tiles))
	for _, t := range p.tiles {
		comps = append(comps, t)
	}
	a := &fakeAssembled{p: p, comps: comps}
	if p.kernel {
		a.kernel = fakeKernel{}
	}
	return a, nil
}

type tilePluginAdapter struct{ t *fakeTile }

func (a *tilePluginAdapter) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: a.t.id, Name: a.t.name, Kind: plugin.KindTile,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     plugin.SlotPreference{Anchor: a.t.anchor, Priority: a.t.priority},
	}
}
func (a *tilePluginAdapter) New(svc.Services) (plugin.Component, error) { return a.t, nil }

type fakeAssembled struct {
	p      *fakePack
	comps  []plugin.Component
	kernel plugin.Kernel
}

func (a *fakeAssembled) Pack() plugin.Pack                  { return a.p }
func (a *fakeAssembled) Kernel() plugin.Kernel              { return a.kernel }
func (a *fakeAssembled) Tiles() []plugin.Component          { return a.comps }
func (a *fakeAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *fakeAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *fakeAssembled) Services() []plugin.Service { return nil }
func (a *fakeAssembled) Dispose()                   {}

// newTestModel 造一个装有两块磁贴的引擎。
func newTestModel(t *testing.T, tiles ...*fakeTile) (*Model, *fakePack) {
	t.Helper()
	if len(tiles) == 0 {
		tiles = []*fakeTile{newFakeTile("demo.a", "甲"), newFakeTile("demo.b", "乙")}
	}
	pack := newFakePack("demo.pack", tiles...)
	m := New(Config{
		EngineAPI: testEngine,
		Services:  svc.Noop{},
		Layout:    layout.DefaultConfig(),
		View:      plugin.NewViewConfig(),
		Packs:     []plugin.Pack{pack},
	})
	m.Resize(120, 40)
	return m, pack
}

// ---------- 布局与渲染 ----------

// TestRenderFitsTerminalExhaustive 在尺寸网格上断言引擎输出永不溢出。
//
// 这是 v2.1.0 那个"排版问题修不完"的根治性断言：不是"某个页面在某个宽度下
// 看着还行"，而是**任何尺寸下都不可能超宽超行**，而且画布诊断必须干净。
func TestRenderFitsTerminalExhaustive(t *testing.T) {
	sizes := []geometry.Size{
		{W: 1, H: 1}, {W: 2, H: 2}, {W: 10, H: 3}, {W: 20, H: 5},
		{W: 40, H: 10}, {W: 59, H: 15}, {W: 60, H: 16}, {W: 80, H: 24},
		{W: 100, H: 30}, {W: 120, H: 30}, {W: 160, H: 44}, {W: 200, H: 60},
	}
	for _, size := range sizes {
		m, _ := newTestModel(t)
		m.Resize(size.W, size.H)
		out := m.View()

		if size.W <= 0 || size.H <= 0 {
			continue
		}
		lines := strings.Split(out, "\n")
		if len(lines) != size.H {
			t.Errorf("%dx%d：输出 %d 行，应恰好 %d 行", size.W, size.H, len(lines), size.H)
		}
		for i, l := range lines {
			if got := canvas.StringWidth(l); got > size.W {
				t.Errorf("%dx%d：第 %d 行宽 %d 超过 %d：%q", size.W, size.H, i, got, size.W, l)
			}
		}
		// 画布诊断必须干净：正常渲染路径下不该有越界或覆盖。
		if !m.canvas.Diag.Clean() {
			t.Errorf("%dx%d：画布诊断不干净 overflow=%d collisions=%d clipped=%d",
				size.W, size.H, m.canvas.Diag.Overflow, m.canvas.Diag.Collisions, m.canvas.Diag.Clipped)
		}
		if bad := m.canvas.CheckInvariants(); bad != "" {
			t.Errorf("%dx%d：画布不变量被破坏：%s", size.W, size.H, bad)
		}
	}
}

// TestViewIsPure 验证渲染不改状态：连续渲染两帧必须完全一样。
//
// 这是"渲染是纯的"这条硬规则的测试化。v2.1.0 在渲染路径里写滚动偏移、
// 输出响铃，于是同一份状态渲染两次会得到不同结果，也没法安全地重绘。
func TestViewIsPure(t *testing.T) {
	m, _ := newTestModel(t)
	first := m.View()
	for i := 0; i < 5; i++ {
		again := m.View()
		if again != first {
			t.Fatalf("第 %d 次渲染与首次不同——渲染不纯", i+2)
		}
	}
	// 磁贴的渲染次数当然会增加（那是它自己的计数器），但界面输出必须一致。
}

// TestTilesArePlacedAndRendered 验证磁贴按视图配置安置并真的被画出来。
func TestTilesArePlacedAndRendered(t *testing.T) {
	a, b := newFakeTile("demo.a", "甲"), newFakeTile("demo.b", "乙")
	m, _ := newTestModel(t, a, b)

	if m.Registry().Count() != 2 {
		t.Fatalf("应安置 2 块磁贴，实际 %d", m.Registry().Count())
	}
	out := m.View()
	for _, want := range []string{"甲", "乙"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出里应含磁贴 %q", want)
		}
	}
	if a.renders == 0 || b.renders == 0 {
		t.Errorf("两块磁贴都应被渲染过（甲=%d 乙=%d）", a.renders, b.renders)
	}
}

// TestDockHidden 验证视图配置关掉停靠区后，停靠磁贴不再被画。
func TestDockHidden(t *testing.T) {
	a := newFakeTile("demo.a", "甲")
	// 把甲挪到停靠区，然后关掉停靠区。
	vc := plugin.NewViewConfig().
		WithSlot(geometry.AnchorCenterDockLeft, "demo.a")
	vc.DockVisible = false

	m2 := New(Config{
		EngineAPI: testEngine, Services: svc.Noop{},
		Layout: layout.DefaultConfig(), View: vc,
		Packs: []plugin.Pack{newFakePack("demo.pack", a)},
	})
	m2.Resize(120, 40)
	// 配置指向停靠区、而停靠区被关掉——这是**正常组合**（关掉停靠区就是
	// 想让它别占地方）。此时该磁贴既不占停靠槽位，也不该被塞到别的栏位：
	// 一个为停靠区设计的磁贴被塞进侧栏，它的排版假设全是错的。
	for _, anchored := range m2.Registry().Anchors() {
		if anchored.Column() == "dock" {
			t.Errorf("停靠区关闭时不该有磁贴占停靠槽位，实际 %v 被占", anchored)
		}
	}
	if slot, ok := m2.Registry().ByID("demo.a"); ok {
		t.Errorf("声明停靠区而停靠区被关闭时，磁贴不该被安置到 %v", slot.Anchor)
	}
	if got := m2.Layout().Center.Dock; !got.Empty() {
		t.Errorf("停靠区关闭时它应是空矩形，实际 %v", got)
	}
}

// ---------- 焦点与按键路由 ----------

// TestTabCyclesFocus 验证 tab 在已占用槽位间循环。
func TestTabCyclesFocus(t *testing.T) {
	a, b := newFakeTile("demo.a", "甲"), newFakeTile("demo.b", "乙")
	m, _ := newTestModel(t, a, b)

	first := m.Focus()
	if !first.IsSlot() {
		t.Fatal("初始应有焦点")
	}
	anchors := m.Registry().Anchors()
	if len(anchors) < 2 {
		t.Fatalf("这个用例需要至少两个槽位，实际 %d", len(anchors))
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "tab"})
	second := m.Focus()
	if second == first {
		t.Fatalf("tab 后焦点应移动，实际仍是 %v", second)
	}
	// 再按 n-1 次，总共 n 次，应当回到起点。
	for i := 1; i < len(anchors); i++ {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "tab"})
	}
	if m.Focus() != first {
		t.Errorf("按满一圈（%d 次 tab）后应回到 %v，实际 %v", len(anchors), first, m.Focus())
	}
	// 反向也要能循环（shift+tab）。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "shift+tab"})
	if m.Focus() != second {
		t.Errorf("shift+tab 一次应退到 %v，实际 %v", second, m.Focus())
	}
}

// TestEventGoesToFocusedTile 验证按键只送到焦点磁贴。
func TestEventGoesToFocusedTile(t *testing.T) {
	a, b := newFakeTile("demo.a", "甲"), newFakeTile("demo.b", "乙")
	m, _ := newTestModel(t, a, b)

	// 把焦点定到甲，发一个普通按键。
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "x"})

	if len(a.keys) != 1 || a.keys[0] != "x" {
		t.Errorf("焦点磁贴应收到按键，实际 %v", a.keys)
	}
	if len(b.keys) != 0 {
		t.Errorf("非焦点磁贴不应收到按键，实际 %v", b.keys)
	}
}

// TestBorrowAndHandover 是本次架构最核心的行为测试。
//
// 它一次断言四件事：
//  1. 磁贴按 enter 能借调中栏舞台（二级内容出现在舞台上）；
//  2. 借调期间**所有磁贴都不再收键**（否则侧栏按键会穿透到下面）；
//  3. esc 逐级退回，栈深正确变化；
//  4. 退干净之后**焦点交还给当初借调的磁贴**（req.md 的 Handover）。
func TestBorrowAndHandover(t *testing.T) {
	// 两块磁贴各自声明槽位，避免它们争同一个位置（那是另一个用例的事）。
	a := newFakeTileAt("demo.a", "甲", geometry.AnchorLeftTop)
	b := newFakeTileAt("demo.b", "乙", geometry.AnchorLeftBottom)
	a.borrowOn = "enter"

	m, _ := newTestModel(t, a, b)
	if got := m.Registry().Count(); got != 2 {
		t.Fatalf("这个用例需要两块磁贴都安置好，实际 %d", got)
	}
	m.SetFocus(geometry.AnchorLeftTop)

	// ① 借调一层；再手工推一层，凑出"多级菜单 = 栈更深"。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if m.Stage().Depth() != 1 {
		t.Fatalf("应借调 1 层，实际 %d", m.Stage().Depth())
	}
	if got := m.Stage().TopOrigin().TileID; got != "demo.a" {
		t.Fatalf("栈顶来源应是 demo.a，实际 %q", got)
	}
	m.Stage().Push(&plugin.TestView{ViewName: "二级", Lines: []string{"二级菜单"}},
		plugin.Origin{TileID: "demo.a", Anchor: geometry.AnchorLeftTop})
	if m.Stage().Depth() != 2 {
		t.Fatalf("手工再推一层后应为 2 层，实际 %d", m.Stage().Depth())
	}
	if out := m.View(); !strings.Contains(out, "二级菜单") {
		t.Errorf("舞台应显示栈顶内容，实际输出：\n%s", out)
	}

	// ② 借调期间按键不应送进磁贴。
	beforeA, beforeB := len(a.keys), len(b.keys)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "x"})
	if len(a.keys) != beforeA || len(b.keys) != beforeB {
		t.Errorf("借调期间磁贴不应收到按键（甲 %d→%d，乙 %d→%d）",
			beforeA, len(a.keys), beforeB, len(b.keys))
	}

	// ③ esc 逐级退回。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Depth() != 1 {
		t.Fatalf("esc 一次应退到 1 层，实际 %d", m.Stage().Depth())
	}
	if m.Focus() != geometry.AnchorLeftTop {
		t.Errorf("每退一层都应交还焦点给借调方，实际 %v", m.Focus())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Fatalf("再 esc 一次应退回看板，实际栈深 %d", m.Stage().Depth())
	}

	// ④ 交还焦点：借调期间焦点若被别的东西动过，退出时仍要回到借调方。
	//    （这里直接改内部字段来模拟"被别的东西动过"，因为真实场景里
	//     借调期间磁贴根本不收键，焦点只可能被引擎自己或宿主改动。）
	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 焦点在乙，不借调
	if m.Stage().Borrowing() {
		t.Fatal("焦点在乙时按 enter 不应借调（这个用例假定只有甲会借调）")
	}
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 甲借调
	if !m.Stage().Borrowing() {
		t.Fatal("第二次借调应当发生")
	}
	m.focus = geometry.AnchorLeftBottom // 借调期间焦点被改动
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Focus() != geometry.AnchorLeftTop {
		t.Errorf("退出借调后焦点应回到甲（左上），实际 %v（栈深 %d，来源 %v）",
			m.Focus(), m.Stage().Depth(), m.Stage().TopOrigin())
	}
}

// TestEscOnBoardIsHarmless 验证在看板上按 esc 不会做任何事。
//
// 这条对应手感：不能用 esc 把界面关掉，更不能让中栏变空。
func TestEscOnBoardIsHarmless(t *testing.T) {
	m, _ := newTestModel(t)
	before := m.View()
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Error("看板上按 esc 不应产生借调")
	}
	if after := m.View(); after != before {
		t.Error("看板上按 esc 不应改变界面")
	}
}

// TestCtrlCRequestsQuit 验证 ctrl+c 请求退出。
func TestCtrlCRequestsQuit(t *testing.T) {
	m, _ := newTestModel(t)
	if !m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+c"}) {
		t.Error("ctrl+c 应请求退出")
	}
}

// TestSelectBroadcastsSelection 验证选中上报会更新全局选中上下文。
//
// 这是"联动选项不认识任何磁贴"的前提：选中由引擎统一持有。
func TestSelectBroadcastsSelection(t *testing.T) {
	a := newFakeTile("demo.a", "甲")
	a.selectOn = "enter"
	m, _ := newTestModel(t, a)
	m.SetFocus(geometry.AnchorLeftTop)

	if !m.Selection().Empty() {
		t.Fatal("初始不应有选中")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if got := m.Selection(); got.ID != "demo.a" {
		t.Errorf("选中应被上报为 demo.a，实际 %q", got.ID)
	}
}

// TestToastIsTemporaryAndClearedByKey 验证提示会自动过期且被按键清掉。
//
// 提示必须能被清掉：它是临时信息，拦着用户干活是最恼人的一种设计。
func TestToastIsTemporaryAndClearedByKey(t *testing.T) {
	m, _ := newTestModel(t)
	m.Toast("测试提示")
	if !strings.Contains(m.View(), "测试提示") {
		t.Fatal("提示应被画出来")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "x"})
	if strings.Contains(m.View(), "测试提示") {
		t.Error("按任意键后提示应消失")
	}
}

// TestLoadReportIsExposed 验证装载报告能被宿主读到（"为什么它没出现"要有答案）。
func TestLoadReportIsExposed(t *testing.T) {
	// 缺依赖的包：要有一个成员，否则它会先因为"空包"被拦下，
	// 测的就不是缺能力这条路径了。
	broken := newFakePack("demo.broken", newFakeTile("demo.broken.tile", "孤儿磁贴"))
	broken.requires = []string{"cap.nobody"}

	m := New(Config{
		EngineAPI: testEngine, Services: svc.Noop{},
		Layout: layout.DefaultConfig(), View: plugin.NewViewConfig(),
		Packs: []plugin.Pack{broken},
	})
	m.Resize(80, 24)

	rep := m.Report()
	if len(rep.Loaded) != 0 {
		t.Fatalf("缺依赖的包不应装载，实际 %v", rep.Loaded)
	}
	text := rep.Explain()
	if !strings.Contains(text, "cap.nobody") {
		t.Errorf("装载报告应点出缺失的能力标记，实际：%s", text)
	}
}

// TestMissingKernelStillRenders 验证没有内核时引擎仍然可用。
//
// "少一个包"必须仍然可用；一个装配期的问题不该让程序起不来。
func TestMissingKernelStillRenders(t *testing.T) {
	m, _ := newTestModel(t) // 假包里没有内核
	if m.Manager().Kernel() != nil {
		t.Fatal("这个用例假定没有内核")
	}
	out := m.View()
	if out == "" {
		t.Error("没有内核时也应渲染出东西")
	}
	if m.Stage().Base() != nil {
		t.Error("没有内核时底层视图应为空")
	}
}

// TestBorrowFromHostUsesSameChannel 验证宿主借调走的是**同一个**通道。
//
// 内核给自己开后门（直接改 view 枚举）正是 v2.1.0 的老路，
// 因此这里断言宿主借调之后，其行为与磁贴借调完全一致。
func TestBorrowFromHostUsesSameChannel(t *testing.T) {
	m, _ := newTestModel(t)
	v := &plugin.TestView{ViewName: "宿主页面", Lines: []string{"宿主打开的页面"}}

	m.Borrow(v)
	if m.Stage().Depth() != 1 {
		t.Fatalf("宿主借调后栈深应为 1，实际 %d", m.Stage().Depth())
	}
	if !strings.Contains(m.View(), "宿主打开的页面") {
		t.Error("宿主借调的页面应显示在舞台上")
	}
	// 宿主借调期间，磁贴同样不收键。
	a, _ := m.Registry().ByID("demo.a")
	before := len(a.Component.(*fakeTile).keys)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "x"})
	if len(a.Component.(*fakeTile).keys) != before {
		t.Error("宿主借调期间磁贴不应收到按键")
	}
	m.CloseAll()
	if m.Stage().Borrowing() {
		t.Error("CloseAll 之后不应仍在借调")
	}
}
