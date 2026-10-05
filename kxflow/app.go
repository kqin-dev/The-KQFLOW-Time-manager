// Package kxflow 是引擎门面：把布局、画布、舞台、磁贴与插件体系串成一帧。
//
// 它的职责边界很窄，值得写清楚：
//
//   - **它不认识业务**：不知道 TODO / GOAL / 计时是什么，只认识插件与动作；
//   - **它不碰副作用**：写盘、响铃、推送都经由 svc.Services，由宿主实现；
//   - **它的渲染是纯的**：View() 只读状态、只写画布，不改任何字段。
//
// 最后一条是硬性的。v2.1.0 把响铃挂在渲染路径上，导致"停在设置页就不响"；
// 也曾在渲染里写滚动偏移。这类错误在新架构里**没有地方可以犯**：
// 渲染函数拿到的 RenderCtx 里根本没有副作用入口。
package kxflow

import (
	"fmt"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/chrome"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/layout"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/stage"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/theme"
	"github.com/kqin-dev/kxflow/tile"
)

// Config 是创建引擎所需的全部输入。
type Config struct {
	// EngineAPI 是当前引擎版本（插件与包的兼容裁决依据）。
	EngineAPI semver.Version
	// DataSchema 是数据文件的实际结构版本（0 表示不做数据版本检查）。
	DataSchema int
	// Services 是宿主提供的副作用边界；为 nil 时退化为 Noop。
	Services svc.Services
	// Layout 是布局配置。
	Layout layout.LayoutConfig
	// View 是视图配置（磁贴摆放与显隐）。
	View plugin.ViewConfig
	// Packs 是要装载的全部整合包。
	Packs []plugin.Pack
	// Theme 是主题；为 nil 时用默认主题。
	Theme *theme.Theme
	// Frame 是磁贴外框样式；零值时退化为圆角。
	Frame canvas.Frame
}

// Model 是引擎的根模型。它实现 Bubble Tea 的 Model 接口，但**不依赖** bubbletea
// （Init 返回的 Cmd 用 any 表达），这样引擎可以被别的宿主复用。
type Model struct {
	svc       svc.Services
	manager   *plugin.Manager
	registry  *tile.Registry
	stage     *stage.Stage
	layoutCfg layout.LayoutConfig
	viewCfg   plugin.ViewConfig
	theme     *theme.Theme
	frame     canvas.Frame

	canvas *canvas.Canvas
	size   geometry.Size
	shell  layout.Shell

	header chrome.HeaderBar
	footer chrome.FooterBar

	// focus 是当前获得键盘焦点的槽位。
	focus geometry.Anchor
	// selection 是全局选中上下文（联动选项据此出现）。
	selection plugin.Selection

	toast     string
	toastKind toastKind
	toastTill time.Time

	// report 是装载报告，可供宿主展示（"为什么我的 DDL 没出现"）。
	report plugin.LoadReport
}

type toastKind uint8

const (
	toastInfo toastKind = iota
	toastWarn
	toastErr
)

// New 创建引擎：装载整合包、安置磁贴、准备画布。
//
// 装载失败**不阻断启动**：少一个包仍然可用，"起不来"才是事故。
// 失败原因全部在 Report() 里，宿主可以展示给用户。
func New(cfg Config) *Model {
	s := cfg.Services
	if s == nil {
		s = svc.Noop{}
	}
	th := cfg.Theme
	if th == nil {
		th = theme.Default()
	}
	frame := cfg.Frame
	if frame.Horizontal == 0 {
		frame = canvas.FrameRounded
	}
	lc := cfg.Layout
	m := &Model{
		svc:       s,
		manager:   plugin.NewManager(cfg.EngineAPI),
		registry:  tile.NewRegistry(),
		layoutCfg: lc,
		viewCfg:   cfg.View,
		theme:     th,
		frame:     frame,
		canvas:    canvas.New(0, 0),
	}
	m.report = m.manager.Load(cfg.DataSchema, cfg.Packs...)
	m.stage = stage.New(nil)
	if k := m.manager.Kernel(); k != nil {
		m.stage.SetBase(k.Dashboard())
		m.header.Crumbs = []string{"KXFLOW"}
		m.header.StatusStyle = tile.StyleStatus
		m.header.CrumbStyle = tile.StyleAccent
		// 下栏内容：左侧状态 + 右侧按键提示。
		//
		// 这些是**引擎级别**的默认值，因为任何基于 KXFLOW 的产品都需要
		// "怎么操作"这层提示。宿主可以覆盖（改 m.Footer 的字段或追加提示），
		// 但不该被要求必须自己写一份——空的下栏等于用户没有操作指引。
		m.footer.Text = k.PowerBy()
		m.footer.TextStyle = tile.StyleMuted
		m.footer.Hints = []chrome.KeymapHint{
			{Key: "tab", Desc: "切换栏位"},
			{Key: "enter", Desc: "借调/选中"},
			{Key: "esc", Desc: "退回"},
			{Key: "q", Desc: "退出"},
		}
		m.footer.KeyStyle = tile.StyleHintKey
		m.footer.HintStyle = tile.StyleHint
		m.footer.BarFilled = tile.StyleBarFilled
		m.footer.BarEmpty = tile.StyleBarEmpty
	}
	m.placeTiles()
	m.applyFocusDefaults()
	return m
}

// Report 返回装载报告。
func (m *Model) Report() plugin.LoadReport { return m.report }

// Manager 暴露插件管理器（宿主要读能力标记、看板选项、联动选项时用）。
func (m *Model) Manager() *plugin.Manager { return m.manager }

// Stage 暴露舞台（宿主需要主动借调界面时用，例如内核打开设置页）。
func (m *Model) Stage() *stage.Stage { return m.stage }

// Registry 暴露磁贴注册表。
func (m *Model) Registry() *tile.Registry { return m.registry }

// Selection 返回当前选中上下文。
func (m *Model) Selection() plugin.Selection { return m.selection }

// SetSelection 设置当前选中上下文。
//
// 这是联动选项出现的唯一依据：磁贴通过 ActionSelect 上报，引擎归一化后
// 广播给所有联动选项（design §5.2）。磁贴之间因此不需要互相认识。
func (m *Model) SetSelection(s plugin.Selection) { m.selection = s }

// ContextOptions 返回当前选中下适用的联动选项。
//
// 这是**唯一**一处判定"哪个联动选项该出现"的入口：判定逻辑在
// plugin.Applies 里，各包不会各自实现一份。
func (m *Model) ContextOptions() []plugin.ContextOption {
	return m.manager.ContextOptions(m.selection)
}

// BoardOptions 返回全部看板选项（内核自带的 + 各包的）。
func (m *Model) BoardOptions() []plugin.BoardOption {
	return m.manager.BoardOptions()
}

// OptionKeys 返回"数字键 → 选项"的映射，供内核在界面上显示可用的快捷键。
//
// 编号规则（刻意与两处来源的顺序解耦）：
//   - 联动选项排在前面（1..N）：它们是**上下文相关**的，
//     用户刚选中一项时最可能想用的就是它们；
//   - 看板选项接在后面。
//
// 键位只用数字：字母与空格/回车在看板上有既有语义，
// 借走它们会导致"这个键在看板上本来是什么"的问题（v2.1.0 踩过）。
func (m *Model) OptionKeys() []OptionBinding {
	var out []OptionBinding
	// 联动选项：当期适用才给键位——不适用的选项连编号都不该出现。
	for i, o := range m.ContextOptions() {
		if len(out) >= 9 {
			break
		}
		out = append(out, OptionBinding{
			Key:   string(rune('1' + i)),
			Label: o.Label(m.selection),
			Ctx:   o,
		})
	}
	base := len(out)
	for i, o := range m.BoardOptions() {
		if base+i >= 9 {
			break
		}
		out = append(out, OptionBinding{
			Key:   string(rune('1' + base + i)),
			Label: o.Label(),
			Board: o,
		})
	}
	return out
}

// OptionBinding 是一条"按键 → 选项"的绑定。
type OptionBinding struct {
	// Key 是触发它的按键（当前只用数字键）。
	Key string
	// Label 是显示文本。
	Label string
	// Ctx 非空表示它来自联动选项（依赖当前选中）。
	Ctx plugin.ContextOption
	// Board 非空表示它来自看板选项。
	Board plugin.BoardOption
}

// IsContext 报告它是不是联动选项。
func (b OptionBinding) IsContext() bool { return b.Ctx != nil }

// Activate 打开这个选项对应的界面（借调舞台）。
//
// 必须把当前选中一并传进来：联动选项要据此决定"给谁设"。
// 宿主、内核与用户按数字键都走这**同一条**路，谁都不开后门。
func (b OptionBinding) Activate(s svc.Services, sel plugin.Selection) (plugin.View, error) {
	if b.Ctx != nil {
		return b.Ctx.Activate(sel, s)
	}
	if b.Board != nil {
		return b.Board.Activate(s)
	}
	return nil, nil
}

// Toast 显示一条临时提示（默认 3 秒后消失）。
func (m *Model) Toast(text string) {
	m.toast, m.toastKind = text, toastInfo
	m.toastTill = m.svc.Clock().Add(3 * time.Second)
}

// Resize 更新终端尺寸并重算布局。
func (m *Model) Resize(w, h int) {
	m.size = geometry.NewSize(w, h)
	m.canvas.Reset(w, h)
	m.relayout()
}

// relayout 重算布局骨架。
func (m *Model) relayout() {
	cfg := m.layoutCfg
	cfg.LeftTiles = m.registry.CountIn("left")
	cfg.RightTiles = m.registry.CountIn("right")
	cfg.DockTiles = m.registry.CountIn("dock")
	cfg.DockVisible = m.viewCfg.DockVisible
	m.shell = layout.Layout(m.size, cfg)
}

// placeTiles 依据视图配置把已装载的磁贴安置到槽位。
func (m *Model) placeTiles() {
	placed, _, _ := m.manager.Placements(m.viewCfg)
	for _, a := range geometry.AllAnchors {
		id := placed.IDAt(a)
		if id == "" {
			continue
		}
		ref, ok := m.manager.TileByID(id)
		if !ok {
			continue
		}
		if _, hadOld := m.registry.Place(tile.Slot{
			PluginID:  ref.Manifest.ID,
			PackID:    ref.PackID,
			Name:      ref.Manifest.Name,
			Anchor:    a,
			Component: ref.Tile,
		}); hadOld {
			// 重复安置不该发生（视图配置里一个磁贴只会出现一次），
			// 真发生了就留下证据，而不是静默覆盖。
			m.Toast("槽位冲突：" + a.String() + " 被重复占用")
		}
	}
}

// applyFocusDefaults 把焦点放到第一个可用槽位。
//
// 焦点必须有归宿：没有焦点时按键无处可去，用户会觉得"程序卡住了"。
func (m *Model) applyFocusDefaults() {
	if m.focus.IsSlot() {
		if _, ok := m.registry.At(m.focus); ok {
			return
		}
	}
	for _, a := range geometry.AllAnchors {
		if _, ok := m.registry.At(a); ok {
			m.focus = a
			return
		}
	}
	m.focus = geometry.AnchorUnset
}

// rectOf 按当前布局返回某个槽位的矩形。
//
// 这是"磁贴注册表"与"布局"的汇合点：注册表不认识布局，布局也不认识磁贴。
func (m *Model) rectOf(a geometry.Anchor) (geometry.Rect, bool) {
	switch a.Column() {
	case "left":
		return m.shell.Left.Slot(a.SlotIndex()), true
	case "right":
		return m.shell.Right.Slot(a.SlotIndex()), true
	case "dock":
		if !m.shell.Center.DockVisible {
			return geometry.Rect{}, false
		}
		return m.shell.Center.Slot(a.SlotIndex()), true
	}
	return geometry.Rect{}, false
}

// renderCtx 构造本帧的只读渲染上下文。
func (m *Model) renderCtx() plugin.RenderCtx {
	return plugin.RenderCtx{
		Palette:   m.theme.Styles,
		Selection: m.selection,
		Svc:       readOnly{m.svc},
	}
}

// readOnly 把 Services 收窄成渲染期可见的子集。
//
// 这一步是**类型层面**的保障：渲染上下文里没有 Persist / Effect，
// 于是"渲染时顺手写个盘"这种事在编译期就做不出来。
type readOnly struct{ s svc.Services }

func (r readOnly) Clock() time.Time             { return r.s.Clock() }
func (r readOnly) Capabilities() svc.Capability { return r.s.Capabilities() }

// View 渲染一帧。**纯函数**：只读状态、只写画布。
func (m *Model) View() string {
	if m.size.W <= 0 || m.size.H <= 0 {
		return "正在启动 KXFLOW…"
	}
	m.canvas.Clear()
	m.registry.SetStates(m.focus, m.stage.Borrowing())

	// 1) 上下栏。
	m.header.Draw(m.canvas, m.shell.Header)
	m.footer.Draw(m.canvas, m.shell.Footer)

	// 2) 磁贴（含中栏停靠区）。
	m.registry.RenderAll(m.canvas, m.rectOf, m.renderCtx(), m.frame)

	// 3) 主舞台。
	//
	// **必须用 Center.Stage 而不是 Primary**：Primary 是"整页内容该用哪一块"
	// （窄终端下它等于整行，宽终端下等于中栏），而舞台永远只在主控区里。
	// 曾经这里传了 Primary，于是在 60×16 这类尺寸下，内核的看板视图
	// 拿到了 14 行高（含停靠区的 7 行），直接把文字画到了停靠区磁贴上面——
	// 画布诊断报出一串"覆盖已有内容"，而画面上看起来只是"字叠在一起了"。
	if stageRect := m.stageRect(); !stageRect.Empty() {
		m.stage.Render(m.canvas, stageRect, m.renderCtx())
	}

	// 4) 提示浮层叠在最上面（它永远在最上层，否则用户看不到自己刚触发的反馈）。
	if t := m.currentToast(); t != "" {
		m.drawToast(t)
	}
	return m.canvas.Render(m.theme.Styles)
}

// currentToast 返回当前应显示的提示（过期即消失）。
func (m *Model) currentToast() string {
	if m.toast == "" {
		return ""
	}
	if !m.toastTill.IsZero() && m.svc.Clock().After(m.toastTill) {
		m.toast = ""
		return ""
	}
	return m.toast
}

// drawToast 在上栏下方画一行提示。
//
// 它刻意**不用浮层**：v2.1.0 的教训是"铺满屏幕的居中浮层"会把左右面板
// 的边框切出断口，用户截图反馈"渲染坏了"。一行提示夹在上栏与主体之间最简单。
func (m *Model) drawToast(text string) {
	r := geometry.NewRect(m.shell.Header.X, m.shell.Header.Y, m.shell.Header.W, 1)
	if r.Empty() {
		return
	}
	style := tile.StyleMuted
	switch m.toastKind {
	case toastWarn:
		style = tile.StyleWarn
	case toastErr:
		style = tile.StyleError
	}
	inner := geometry.NewRect(r.X+1, r.Y, max(0, r.W-2), r.H)
	restore := m.canvas.PushClip(inner)
	defer restore()
	m.canvas.ClearRect(inner)
	m.canvas.Text(inner.X, inner.Y, canvas.Truncate(text, inner.W), style)
}

// stageRect 返回舞台当前应当绘制的矩形。
//
// 规则只有一句：**舞台与停靠区共享中栏，但绝不重叠**。
//
//   - 停靠区可见时，舞台用 Center.Stage（停靠区之下那部分不属于它）；
//   - 停靠区不可见时，舞台可以用整个中栏高度；
//   - 栈空时（在看板）还允许横向借用到整行——窄终端下多看板一屏能多放内容；
//     栈非空（借调中）则不借：借调视图属于主控区，横跨出去会盖住侧栏磁贴。
//
// 曾经这里写成 `Primary ∩ Center.Rect`，而 Center.Rect 是**整个中栏**
// （含停靠区的 14 行），于是看板拿到了本该属于停靠区的行，
// 把文字直接画到停靠区磁贴上——画布诊断报出"覆盖已有内容"。
func (m *Model) stageRect() geometry.Rect {
	// 垂直方向：有停靠区就用它的上方。
	vertical := m.shell.Center.Rect
	if m.shell.Center.DockVisible && !m.shell.Center.Dock.Empty() {
		vertical = m.shell.Center.Stage
	}
	if vertical.Empty() {
		return vertical
	}
	if m.stage.Borrowing() {
		return vertical
	}
	// 看板可以借用左右栏的横向空间，但仍以 vertical 的高度为界。
	wide := m.shell.Primary
	wide.W = minInt(wide.W, m.shell.Body.W)
	wide.X = m.shell.Body.X
	wide.H = minInt(wide.H, vertical.H)
	wide.Y = vertical.Y
	return wide.Intersect(vertical)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CanvasClean 报告最近一帧没有越界、没有覆盖。
//
// 供宿主在自检与开发期断言使用（测试里尤其有用：它让"画坏了"这件事
// 有一个可查询的出口，而不是只能靠眼睛看）。
func (m *Model) CanvasClean() bool {
	return m.canvas.Diag.Overflow == 0 && m.canvas.Diag.Collisions == 0
}

// Diagnostics 返回最近一帧的诊断摘要，便于失败信息里带上线索。
func (m *Model) Diagnostics() string {
	return fmt.Sprintf("overflow=%d collisions=%d clipped=%d",
		m.canvas.Diag.Overflow, m.canvas.Diag.Collisions, m.canvas.Diag.Clipped)
}

// DiagEvents 返回最近一帧的诊断明细（坐标、原因、可选调用点）。
//
// 把"画坏了"的现场交出去，而不是只给一个计数：本项目历史上多次出现
// "量出来的数据在骗自己"，因此宁可多给一条线索。
func (m *Model) DiagEvents() []canvas.DiagEvent {
	return m.canvas.Diag.Events
}

// Layout 返回最近一次算好的骨架（供宿主查询各区域矩形）。
func (m *Model) Layout() layout.Shell { return m.shell }

// Size 返回当前终端尺寸。
func (m *Model) Size() geometry.Size { return m.size }

// Header 暴露上栏，供宿主追加面包屑或全局状态。
//
// 每帧渲染前宿主应当先调用 ResetExtras 清掉上一帧追加的内容——
// 否则追加项会逐帧累积，最后把上栏塞满（这一条在本项目的旧版本里真实发生过）。
func (m *Model) Header() *chrome.HeaderBar { return &m.header }

// Footer 暴露下栏，供宿主改状态文本或追加按键提示。
func (m *Model) Footer() *chrome.FooterBar { return &m.footer }

// Focus 返回当前获得焦点的槽位。
func (m *Model) Focus() geometry.Anchor { return m.focus }

// SetFocus 设置焦点（不可用的槽位会被忽略）。
func (m *Model) SetFocus(a geometry.Anchor) {
	if !a.IsSlot() {
		return
	}
	if _, ok := m.registry.At(a); !ok {
		return
	}
	m.focus = a
}

// FocusNext 把焦点移到下一个已占用槽位（tab 的行为）。
func (m *Model) FocusNext(delta int) {
	anchors := m.registry.Anchors()
	if len(anchors) == 0 {
		m.focus = geometry.AnchorUnset
		return
	}
	cur := -1
	for i, a := range anchors {
		if a == m.focus {
			cur = i
			break
		}
	}
	next := (cur + delta) % len(anchors)
	if next < 0 {
		next += len(anchors)
	}
	m.focus = anchors[next]
}

// Dispatch 把一个事件交给当前该处理它的那一层，并执行返回的动作。
//
// 路由顺序（**必须只有这一处**）：
//
//  1. 舞台栈顶（有人借调主控区时，它优先——否则侧栏按键会穿透到下面的磁贴）；
//  2. 焦点磁贴。
//
// 对照 v2.1.0：那里是一条 11 分支的 if 链写在 handleKey 里，每加一种
// 二级内容就要再插一层判断。现在"优先级"就是栈的顺序，不需要再维护。
//
// 返回 requestQuit 表示宿主应当退出。
func (m *Model) Dispatch(ev plugin.Event) (requestQuit bool) {
	ctx := plugin.EventCtx{
		Now:        m.svc.Clock(),
		Selection:  m.selection,
		StageDepth: m.stage.Depth(),
	}

	// 提示是可以被任意按键清掉的：它是临时信息，不该拦着用户干活。
	if ev.Kind == plugin.EventKey && m.currentToast() != "" {
		m.toast = ""
	}

	// 全局按键：先处理"谁能拿到焦点"这类与具体磁贴无关的动作。
	switch ev.Key {
	case "ctrl+c":
		return true
	case "tab":
		m.FocusNext(1)
		return false
	case "shift+tab":
		m.FocusNext(-1)
		return false
	case "esc":
		// esc 的语义：先退出借调，退出不了才算"没处可去"。
		if origin, ok := m.stage.Pop(); ok {
			m.focusBack(origin)
			return false
		}
	}

	if top := m.stage.Top(); top != nil && m.stage.Borrowing() {
		act, close := top.Update(ctx, ev)
		m.runAction(act)
		if close {
			if origin, ok := m.stage.Pop(); ok {
				m.focusBack(origin)
			}
		}
		return false
	}

	// 选项（联动 + 看板）用数字键触发。
	//
	// 排在磁贴**之前**：数字键在看板上没有别的语义，因此借走它们不会
	// 造成"这个键本来是什么"的问题（字母键就会——v2.1.0 踩过）。
	// 而磁贴自己的按键是 j/k/space/enter 这些，不与数字冲突。
	if n := digitIndex(ev.Key); n >= 0 {
		if m.activateOption(n) {
			return false
		}
	}

	slot, ok := m.registry.At(m.focus)
	if !ok || slot.Component == nil {
		return false
	}
	ctx.Focused = true
	act := slot.Component.Update(ctx, ev)
	m.runAction(act)
	return false
}

// digitIndex 把 "1".."9" 解析成 0..8；其它按键返回 -1。
func digitIndex(key string) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return -1
	}
	return int(key[0] - '1')
}

// activateOption 打开第 idx 个选项（按 OptionKeys 的顺序）；没有则返回 false。
//
// 返回 false 时调用方继续把按键交给磁贴——这样"按了没有对应选项的数字"
// 不会把按键吞掉，用户不会觉得"这个键没反应"。
func (m *Model) activateOption(idx int) bool {
	bindings := m.OptionKeys()
	if idx < 0 || idx >= len(bindings) {
		return false
	}
	b := bindings[idx]
	view, err := b.Activate(m.svc, m.selection)
	if err != nil {
		m.toast, m.toastKind = "打开失败："+err.Error(), toastErr
		return true
	}
	if view == nil {
		return false
	}
	kind := plugin.OriginBoardOption
	if b.IsContext() {
		kind = plugin.OriginContextOption
	}
	m.stage.Push(view, plugin.Origin{Kind: kind})
	return true
}

// focusBack 在退出借调之后把焦点交还给借调方（req.md 的 Handover）。
//
// origin 必须是**被弹掉的那一层**的来源，由调用方从 Pop 拿到后传进来。
// 这里刻意不接受"自己去栈里查"：Pop 之后栈顶已经是上一层了，
// 再去读 TopOrigin 会读到父层的来源——那正是本函数最初写错的地方，
// 表现为"退回看板时焦点跑到了另一个磁贴"（最外层弹完后更是什么都读不到）。
func (m *Model) focusBack(origin plugin.Origin) {
	if origin.TileID != "" {
		if slot, ok := m.registry.ByID(origin.TileID); ok {
			m.focus = slot.Anchor
			return
		}
	}
	m.applyFocusDefaults()
}

// runAction 解释插件返回的意图。
//
// 插件**不能**直接落盘或播音：它只能说"我想做什么"。这就是
// "磁贴不知道存储、不知道庆祝动画存在"的机制（design §5.2）。
func (m *Model) runAction(a plugin.Action) {
	switch a.Kind {
	case plugin.ActionNone:
		return
	case plugin.ActionBorrow:
		req, ok := plugin.AsBorrow(a)
		if !ok {
			return
		}
		origin := req.Origin
		if origin.TileID == "" {
			// 磁贴通常不知道自己的锚点，也不需要知道；内核替它补全。
			if slot, ok := m.registry.At(m.focus); ok {
				origin.TileID = slot.PluginID
				origin.Anchor = slot.Anchor
			}
			if origin.Kind != plugin.OriginKernel {
				origin.Kind = plugin.OriginTile
			}
		}
		m.stage.Push(req.View, origin)
	case plugin.ActionSelect:
		if s, ok := plugin.AsSelection(a); ok {
			m.selection = s
		}
	case plugin.ActionEffect:
		if e, ok := plugin.AsEffect(a); ok {
			m.svc.Effect(e)
		}
	case plugin.ActionPersist:
		if req, ok := a.Payload.(svc.PersistRequest); ok {
			if err := m.svc.Persist(req); err != nil {
				m.toast, m.toastKind = "保存失败："+err.Error(), toastErr
			}
		}
	case plugin.ActionToast:
		if s, ok := plugin.AsToast(a); ok {
			m.Toast(s)
		}
	}
}

// Borrow 让宿主主动借调一层界面（内核打开设置页等）。
//
// 宿主也走**同一个**借调通道，不给自己开后门：这样"二级内容只占中栏"
// 对内核与对插件是同一条规则。
func (m *Model) Borrow(v plugin.View) {
	m.stage.Push(v, plugin.Origin{Kind: plugin.OriginKernel})
}

// CloseAll 退回底层视图。
func (m *Model) CloseAll() { m.stage.PopToBottom() }

// Dispose 释放引擎持有的资源（幂等）。
func (m *Model) Dispose() { m.manager.Unload() }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
