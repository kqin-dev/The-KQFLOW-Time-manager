package stage

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// newTestView 造一个可观测生命周期的视图。
func newTestView(name string) *plugin.TestView {
	return &plugin.TestView{ViewName: name, Lines: []string{name}}
}

// TestStackOrderAndTop 验证栈顶才是"在场"的那一层。
//
// 这是替换 v2.1.0 那条 11 分支优先级 if 链的核心：**栈的顺序就是优先级**。
func TestStackOrderAndTop(t *testing.T) {
	base := newTestView("看板")
	s := New(base)

	if s.Depth() != 0 || s.Borrowing() {
		t.Fatalf("初始应为栈空、未借调，实际 depth=%d borrowing=%v", s.Depth(), s.Borrowing())
	}
	if s.Top() != plugin.View(base) {
		t.Fatal("栈空时 Top 应返回底层视图")
	}

	v1 := newTestView("一级")
	s.Push(v1, plugin.Origin{TileID: "t1", Anchor: geometry.AnchorLeftTop})
	if s.Depth() != 1 || !s.Borrowing() {
		t.Fatalf("推一层后应 depth=1 且借调中，实际 depth=%d", s.Depth())
	}
	if s.Top() != plugin.View(v1) {
		t.Fatal("栈非空时 Top 应返回栈顶")
	}
	if !v1.Entered {
		t.Error("Push 应触发 OnEnter")
	}

	v2 := newTestView("二级")
	s.Push(v2, plugin.Origin{TileID: "t1", Anchor: geometry.AnchorLeftTop})
	if s.Depth() != 2 || s.Top() != plugin.View(v2) {
		t.Fatalf("再推一层后栈顶应为二级，实际 depth=%d", s.Depth())
	}

	// 逐级弹出，每层都要收到 OnExit。
	o, ok := s.Pop()
	if !ok || o.TileID != "t1" {
		t.Fatalf("弹出应返回来源，实际 %+v ok=%v", o, ok)
	}
	if !v2.Exited {
		t.Error("Pop 应触发 OnExit")
	}
	if s.Top() != plugin.View(v1) {
		t.Error("弹一层后栈顶应回到一级")
	}
	s.Pop()
	if s.Top() != plugin.View(base) || s.Borrowing() {
		t.Error("全部弹出后应回到看板且不再借调")
	}
}

// TestPopOnEmptyIsHarmless 验证"多按一次 esc"不会把界面弄坏。
//
// 这条直接对应手感：用户在看板上按 esc 不该发生任何事，
// 更不该把 base 也弹掉（那样中栏就永远空着了）。
func TestPopOnEmptyIsHarmless(t *testing.T) {
	base := newTestView("看板")
	s := New(base)
	if _, ok := s.Pop(); ok {
		t.Error("栈空时 Pop 应返回 ok=false")
	}
	if s.Top() != plugin.View(base) {
		t.Error("栈空时 Pop 不应影响底层视图")
	}
	if base.Exited {
		t.Error("底层视图不应被 Pop 触发 OnExit")
	}
}

// TestOriginIsRememberedPerLayer 验证每层各自记得"谁借调的"。
//
// 交还焦点靠的就是它：没有它，关掉二级菜单后焦点只能靠猜。
func TestOriginIsRememberedPerLayer(t *testing.T) {
	s := New(newTestView("看板"))
	s.Push(newTestView("甲"), plugin.Origin{TileID: "tileA", Anchor: geometry.AnchorLeftTop})
	s.Push(newTestView("乙"), plugin.Origin{TileID: "tileB", Anchor: geometry.AnchorRightBottom})

	if got := s.TopOrigin(); got.TileID != "tileB" {
		t.Errorf("栈顶来源应为 tileB，实际 %q", got.TileID)
	}
	if !s.OccupiedBy("tileB") {
		t.Error("OccupiedBy 应认得栈顶来源")
	}
	if s.OccupiedBy("tileA") {
		t.Error("OccupiedBy 不应把非栈顶来源算进来")
	}
	s.Pop()
	if got := s.TopOrigin(); got.TileID != "tileA" {
		t.Errorf("弹一层后栈顶来源应回到 tileA，实际 %q", got.TileID)
	}
}

// TestPopToBottom 验证"一路退回看板"，并返回按出栈顺序的来源。
func TestPopToBottom(t *testing.T) {
	s := New(newTestView("看板"))
	s.Push(newTestView("一"), plugin.Origin{TileID: "t1"})
	s.Push(newTestView("二"), plugin.Origin{TileID: "t2"})
	s.Push(newTestView("三"), plugin.Origin{TileID: "t3"})

	origins := s.PopToBottom()
	if len(origins) != 3 {
		t.Fatalf("应弹出 3 层，实际 %d", len(origins))
	}
	// 出栈顺序 = 后进先出。
	if origins[0].TileID != "t3" || origins[2].TileID != "t1" {
		t.Errorf("出栈顺序应为 t3,t2,t1，实际 %v", origins)
	}
	if s.Borrowing() || s.Depth() != 0 {
		t.Error("全部弹出后不应仍在借调")
	}
}

// TestPushNilIgnored 验证推 nil 不会制造一个"空层"。
func TestPushNilIgnored(t *testing.T) {
	s := New(newTestView("看板"))
	s.Push(nil, plugin.Origin{TileID: "x"})
	if s.Depth() != 0 {
		t.Errorf("推 nil 不应改变栈深，实际 %d", s.Depth())
	}
}

// TestReplaceKeepsDepthAndOrigin 验证替换栈顶（向导下一步）不改变层位置。
func TestReplaceKeepsDepthAndOrigin(t *testing.T) {
	s := New(newTestView("看板"))
	first := newTestView("第一步")
	s.Push(first, plugin.Origin{TileID: "wizard", Anchor: geometry.AnchorLeftTop})

	second := newTestView("第二步")
	s.Replace(second)

	if s.Depth() != 1 {
		t.Errorf("替换不应改变栈深，实际 %d", s.Depth())
	}
	if s.Top() != plugin.View(second) {
		t.Error("替换后栈顶应为新视图")
	}
	if !first.Exited {
		t.Error("被替换掉的视图应收到 OnExit")
	}
	if !second.Entered {
		t.Error("新视图应收到 OnEnter")
	}
	if s.TopOrigin().TileID != "wizard" {
		t.Error("替换不应丢失来源（否则焦点交还会走错地方）")
	}
}

// TestRenderConfinesToRect 是"二级内容只占中栏"的机制化断言。
//
// 舞台只拿到一个矩形，画布裁剪区按它设置，因此视图**没有能力**
// 把内容画到矩形之外——这正是 v2.1.0"弹窗横跨三栏把边框切出断口"的根治。
func TestRenderConfinesToRect(t *testing.T) {
	c := canvas.New(40, 10)
	// 先把整块画布填成 '.'，这样"视图画到哪里"一目了然。
	c.Fill(c.Bounds(), '.', 1)

	// 一个企图画满全屏的"恶意"视图。
	evil := &plugin.ViewFunc{
		ViewName: "越界视图",
		RenderFn: func(ctx plugin.RenderCtx) {
			for y := 0; y < 10; y++ {
				ctx.Canvas.Text(0, y, "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", 0)
			}
			ctx.Canvas.Set(39, 9, 'Y', 0)
		},
	}
	s := New(nil)
	s.Push(evil, plugin.Origin{})

	rect := geometry.NewRect(10, 3, 12, 4)
	s.Render(c, rect, plugin.RenderCtx{Palette: canvas.PlainPalette{}})

	// 矩形之外必须一个 X 都没有。
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			got := c.CellAt(x, y).R
			if got == 'X' && !rect.Contains(x, y) {
				t.Fatalf("视图把内容画到了矩形之外 (%d,%d)", x, y)
			}
			if got == 'Y' && (x != 39 || y != 9) {
				t.Fatalf("意外的 Y 出现在 (%d,%d)", x, y)
			}
		}
	}
	// (39,9) 在矩形外，那次 Set 必须失败。
	if c.CellAt(39, 9).R == 'Y' {
		t.Error("矩形之外的 Set 竟然成功了")
	}
	if c.Diag.Overflow == 0 {
		t.Error("越界绘制必须计入诊断，否则「坏了」这件事没有出口")
	}
}

// TestRenderOnlyDrawsTopLayer 验证只渲染栈顶（不做叠加浮层）。
func TestRenderOnlyDrawsTopLayer(t *testing.T) {
	c := canvas.New(20, 3)
	s := New(nil)
	s.Push(&plugin.TestView{ViewName: "底层", Lines: []string{"底层内容"}}, plugin.Origin{})
	s.Push(&plugin.TestView{ViewName: "顶层", Lines: []string{"顶层内容"}}, plugin.Origin{})

	rect := geometry.NewRect(0, 0, 20, 3)
	s.Render(c, rect, plugin.RenderCtx{Palette: canvas.PlainPalette{}})

	got := c.String()
	if !strings.Contains(got, "顶层内容") {
		t.Errorf("应画出栈顶内容，实际 %q", got)
	}
	if strings.Contains(got, "底层内容") {
		t.Errorf("不应画出被盖住的层，实际 %q", got)
	}
}

// TestRenderEmptyRectIsNoop 验证矩形为空时不画任何东西（也不 panic）。
func TestRenderEmptyRectIsNoop(t *testing.T) {
	c := canvas.New(20, 3)
	c.Fill(c.Bounds(), '.', 0)
	s := New(&plugin.TestView{ViewName: "看板", Lines: []string{"内容"}})
	s.Render(c, geometry.Rect{X: 0, Y: 0}, plugin.RenderCtx{})
	if got := c.String(); strings.Contains(got, "内容") {
		t.Errorf("空矩形时不应画出内容，实际 %q", got)
	}
}

// TestClearDoesNotFireExit 验证 Clear 不触发 OnExit。
//
// 它用在引擎销毁路径上：那时视图可能已经依赖了被释放的资源，
// 再回调一次 OnExit 只会制造崩溃。
func TestClearDoesNotFireExit(t *testing.T) {
	v := newTestView("甲")
	s := New(nil)
	s.Push(v, plugin.Origin{})
	s.Clear()
	if v.Exited {
		t.Error("Clear 不应触发 OnExit")
	}
	if s.Depth() != 0 {
		t.Error("Clear 之后栈应为空")
	}
}

// TestSetBase 验证替换底层视图。
func TestSetBase(t *testing.T) {
	s := New(nil)
	a := newTestView("甲")
	s.SetBase(a)
	if s.Top() != plugin.View(a) {
		t.Error("设置底层后 Top 应返回它")
	}
	b := newTestView("乙")
	s.SetBase(b)
	if s.Top() != plugin.View(b) {
		t.Error("替换底层后 Top 应返回新的")
	}
}
