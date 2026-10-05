package plugin

import (
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/svc"
)

// view.go 定义"可以在舞台上展示的东西"。
//
// 它放在 plugin 包而不是 stage 包，是为了**依赖方向干净**：
//
//	canvas / geometry / svc  ←  plugin  ←  stage / tile / chrome  ←  引擎门面
//
// 如果 View 定义在 stage，那么"磁贴返回一个借调请求"（plugin 的动作）
// 就要引用 stage，而 stage 又引用 plugin —— 立刻成环。
// 接口的归属应当由**谁用得更基础**决定，而不是由它概念上属于哪一层决定。

// View 是可以在 Stage 主控区展示的一屏内容。
//
// 它覆盖了 v2.1.0 里的全部"二级内容"：计时菜单、输入框、继承确认、
// 帮助、设置、历史、标签、DDL —— 那些原本是 renderView() 里一条
// 11 分支优先级 if 链，现在是栈里的一个个独立视图。
type View interface {
	// Name 是诊断与日志用的短名（不显示给用户）。
	Name() string
	// Render 把内容画进自己的矩形。
	Render(ctx RenderCtx)
	// Update 处理事件。requestClose 为真时引擎会自动 Pop 这一层，
	// 并按 Origin 把焦点交还给当初借调它的磁贴（req.md 的 Handover）。
	Update(ctx EventCtx, ev Event) (act Action, requestClose bool)
	// OnEnter 在视图入栈并成为栈顶时调用一次。
	// origin 说明"是谁借调了舞台"，视图可以在关闭时据此交还。
	OnEnter(origin Origin)
	// OnExit 在视图出栈时调用一次；实现必须是幂等的。
	OnExit()
}

// ViewFunc 让简单视图可以用函数字面量实现（只为减少样板，不改变语义）。
type ViewFunc struct {
	ViewName string
	// RenderFn / UpdateFn 为 nil 时对应行为是"什么都不做"。
	RenderFn func(ctx RenderCtx)
	UpdateFn func(ctx EventCtx, ev Event) (Action, bool)
	EnterFn  func(origin Origin)
	ExitFn   func()
}

// Name 返回视图名。
func (v *ViewFunc) Name() string {
	if v == nil || v.ViewName == "" {
		return "视图"
	}
	return v.ViewName
}

// Render 调用 RenderFn（若提供）。
func (v *ViewFunc) Render(ctx RenderCtx) {
	if v != nil && v.RenderFn != nil {
		v.RenderFn(ctx)
	}
}

// Update 调用 UpdateFn（若提供），否则什么都不做且不关闭。
func (v *ViewFunc) Update(ctx EventCtx, ev Event) (Action, bool) {
	if v != nil && v.UpdateFn != nil {
		return v.UpdateFn(ctx, ev)
	}
	return None(), false
}

// OnEnter 调用 EnterFn（若提供）。
func (v *ViewFunc) OnEnter(origin Origin) {
	if v != nil && v.EnterFn != nil {
		v.EnterFn(origin)
	}
}

// OnExit 调用 ExitFn（若提供）。
func (v *ViewFunc) OnExit() {
	if v != nil && v.ExitFn != nil {
		v.ExitFn()
	}
}

// Origin 记录"是谁借调了舞台"，用于视图关闭后把焦点交还回去。
//
// 它是 req.md 里 Handover / Yield 那一步的依据：没有它，
// 视图关闭后焦点就不知道该回到哪块磁贴。
type Origin struct {
	// TileID 是借调方的插件 ID；空表示"没有具体来源"（例如内核直接打开的页面）。
	TileID string
	// Anchor 是借调方所在的槽位（焦点交还时要重新聚焦它）。
	Anchor geometry.Anchor
	// Kind 区分来源类别，便于诊断与不同的关闭行为。
	Kind OriginKind
}

// OriginKind 是借调来源的类别。
type OriginKind uint8

const (
	// OriginKernel 内核直接打开（设置 / 帮助 / 退出确认）。
	OriginKernel OriginKind = iota
	// OriginTile 侧栏或停靠区的磁贴打开。
	OriginTile
	// OriginBoardOption 看板选项打开。
	OriginBoardOption
	// OriginContextOption 联动选项打开（作用于"当前选中"）。
	OriginContextOption
)

// String 便于诊断输出可读。
func (o Origin) String() string {
	if o.TileID == "" {
		return "无来源"
	}
	return o.TileID + "@" + o.Anchor.String()
}

// BorrowRequest 是磁贴请求借调舞台时携带的信息。
//
// 磁贴**不直接调用**舞台（那会让所有磁贴都依赖舞台的实现）；
// 它只返回一个 Action，由内核把它翻译成 stage.Push。
type BorrowRequest struct {
	View View
	// Origin 由内核补全（磁贴通常不知道自己的 Anchon，也不需要知道）。
	Origin Origin
}

// Borrow 构造一个"借调舞台"的意图。
//
// 注意：这是**唯一**让磁贴获得界面的途径（design §3.4）。
// 这样"二级内容一律只占中间栏"不再是一条需要自觉遵守的约定，
// 而是磁贴根本没有能力去做别的事。
func Borrow(v View) Action {
	return Action{Kind: ActionBorrow, Payload: BorrowRequest{View: v}}
}

// AsBorrow 从 Action 里取出借调请求。
func AsBorrow(a Action) (BorrowRequest, bool) {
	if a.Kind != ActionBorrow {
		return BorrowRequest{}, false
	}
	req, ok := a.Payload.(BorrowRequest)
	return req, ok
}

// AsSelection 从 Action 里取出选中上下文。
func AsSelection(a Action) (Selection, bool) {
	if a.Kind != ActionSelect {
		return Selection{}, false
	}
	s, ok := a.Payload.(Selection)
	return s, ok
}

// AsEffect 从 Action 里取出副作用。
func AsEffect(a Action) (svc.Effect, bool) {
	if a.Kind != ActionEffect {
		return svc.Effect{}, false
	}
	e, ok := a.Payload.(svc.Effect)
	return e, ok
}

// AsToast 从 Action 里取出一条提示文本。
func AsToast(a Action) (string, bool) {
	if a.Kind != ActionToast {
		return "", false
	}
	s, ok := a.Payload.(string)
	return s, ok
}

// TestView 是一个供引擎自测与演示用的最小视图：显示一行文本、esc 关闭。
//
// 它刻意放在**非测试文件**里：demo 命令与宿主文档都会用到它，
// 而"给别人抄的示例"应该是能被编译进来的。
type TestView struct {
	ViewName string
	Lines    []string
	// Style 是内容使用的样式下标。
	Style canvas.StyleID
	// Entered / Exited 记录生命周期是否被调用（供测试断言 Handover 真的发生）。
	Entered bool
	Exited  bool
	// LastOrigin 记录入栈时的来源，用于断言"焦点交还给谁"。
	LastOrigin Origin
}

// Name 返回视图名。
func (v *TestView) Name() string { return v.ViewName }

// Render 逐行画出内容。
func (v *TestView) Render(ctx RenderCtx) {
	y := ctx.Rect.Y
	for _, line := range v.Lines {
		if y >= ctx.Rect.Y1() {
			break
		}
		ctx.Canvas.Text(ctx.Rect.X, y, canvas.Truncate(line, ctx.Rect.W), v.Style)
		y++
	}
}

// Update 在 esc 或 enter 时请求关闭。
func (v *TestView) Update(ctx EventCtx, ev Event) (Action, bool) {
	switch ev.Key {
	case "esc", "enter", "q":
		return None(), true
	}
	return None(), false
}

// OnEnter 记录入栈。
func (v *TestView) OnEnter(origin Origin) {
	v.Entered = true
	v.LastOrigin = origin
}

// OnExit 记录出栈。
func (v *TestView) OnExit() { v.Exited = true }
