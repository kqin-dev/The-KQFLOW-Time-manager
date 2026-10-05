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
	// quitPending 记录"退出确认层已经推出"，防止连按 q 叠出好几层。
	quitPending bool
	// quitRequested 记录"某处请求了退出"，由 Dispatch 读走并返回。
	quitRequested bool
	toastTill     time.Time

	// report 是装载报告，可供宿主展示（"为什么我的 DDL 没出现"）。
	report plugin.LoadReport

	// unplaced 是"装上了但没槽位可放"的磁贴；placementIssues 是视图配置
	// 与实际情况对不上的地方。两者都要能被宿主读到——
	// 否则用户看到的是"我明明有这个功能，界面上却没有"，且无从解释。
	unplaced        []plugin.Manifest
	placementIssues []plugin.PlacementIssue
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
		// 把"当前可用选项"的查询交给内核，让它在看板上列出来。
		// 这是选中条目后联动选项可见可用的唯一入口。
		k.SetOptionSource(m.optionBindingViews)
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
		m.footer.KeyStyle = tile.StyleHintKey
		m.footer.HintStyle = tile.StyleHint
		m.footer.BarFilled = tile.StyleBarFilled
		m.footer.BarEmpty = tile.StyleBarEmpty
	}
	// 下栏提示**跟着光标走**：光标停在哪儿，就提示那儿现在能按什么。
	// 它是每帧重算的（提示可能依赖"列表里有没有条目"这类实时状态）。
	m.refreshHints()
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

// Options 返回当前可用的全部选项：联动选项在前，看板选项在后。
//
// **没有"键位"这回事**（用户 2026-10-05 的反馈）：
//
//	原话：不应该保留数字作为快捷键：如果有 10 个选项怎么办呢？
//
// 数字键是错的：它把"选项数量"和"可用按键数量"绑在一起，
// 而且用户得先记住编号。正确的做法是**用方向键在菜单里选**
// （按 l 打开的那一刻才需要选，见 newOptionsMenu），
// 因此这里只给出有序列表，不给出任何按键绑定。
func (m *Model) Options() []OptionBinding {
	var out []OptionBinding
	for _, o := range m.ContextOptions() {
		out = append(out, OptionBinding{Label: o.Label(m.selection), Ctx: o})
	}
	for _, o := range m.BoardOptions() {
		out = append(out, OptionBinding{Label: o.Label(), Board: o})
	}
	return out
}

// OptionBinding 是一个可用选项（联动选项或看板选项）。
type OptionBinding struct {
	// Label 是显示文本。
	Label string
	// Ctx 非空表示它来自联动选项（依赖当前选中）。
	Ctx plugin.ContextOption
	// Board 非空表示它来自看板选项。
	Board plugin.BoardOption
}

// IsContext 报告它是不是联动选项。
func (b OptionBinding) IsContext() bool { return b.Ctx != nil }

// optionBindingViews 把当前选项转成内核可画的只读快照。
func (m *Model) optionBindingViews() []plugin.OptionBindingView {
	opts := m.Options()
	out := make([]plugin.OptionBindingView, 0, len(opts))
	for _, b := range opts {
		out = append(out, plugin.OptionBindingView{
			Label: b.Label, Context: b.IsContext(),
		})
	}
	return out
}

// Activate 打开这个选项对应的界面（借调舞台）。
//
// 必须把当前选中一并传进来：联动选项要据此决定"给谁设"。
// 宿主、内核与菜单都走这**同一条**路，谁都不开后门。
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
//
// 放不下的磁贴会进 unplaced 列表，并由这里汇总成一条**警告**：
// 槽位是有限资源（侧栏 2+2、停靠区 4），多出来的磁贴只能不显示。
// 不提示的话用户会以为"这个包没装上"——而它其实装上了，只是没地方放。
func (m *Model) placeTiles() {
	placed, unplaced, issues := m.manager.Placements(m.viewCfg)
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
	m.unplaced = unplaced
	m.placementIssues = issues
}

// Placements 返回当前每个磁贴的落位（供宿主与测试查询）。
//
// 视图设置界面用它显示"这个磁贴现在在哪儿"；测试用它断言搬迁真的生效
// （只看渲染文本会被"标题撞车"骗过去）。
func (m *Model) Placements() []plugin.Placement {
	out := make([]plugin.Placement, 0, len(geometry.AllAnchors))
	for _, a := range geometry.AllAnchors {
		slot, ok := m.registry.At(a)
		if !ok || slot.PluginID == "" {
			continue
		}
		out = append(out, plugin.Placement{
			PluginID: slot.PluginID, PackID: slot.PackID, Anchor: a,
			// ByUser 问视图配置：这个槽位是不是用户明确指派的。
			ByUser: m.viewCfg.SlotOf(a) != "",
		})
	}
	return out
}

// Unplaced 返回没有槽位可放的磁贴（供宿主展示"为什么它没出现"）。
func (m *Model) Unplaced() []plugin.Manifest { return m.unplaced }

// PlacementIssues 返回视图配置与实际情况对不上的地方。
func (m *Model) PlacementIssues() []plugin.PlacementIssue { return m.placementIssues }

// Tick 是**周期性检查**：宿主每秒调一次，用来处理"与按键无关、
// 随时间发生"的事情（目前是计时走完要响铃）。
//
// 为什么不能把它塞进磁贴的 Update：那种做法只有在**该磁贴恰好获得焦点**
// 时才会被调用，于是"计时结束时用户正在看别的栏位"就不会响铃——
// 而那恰恰是最需要提醒的时候（用户在干别的）。
//
// 引擎在这里只负责"把机会交给每个组件"，具体要不要动作由组件自己判断
// （内核组件、统计磁贴都直接忽略）。
func (m *Model) Tick(now time.Time) {
	ctx := plugin.EventCtx{
		Now:        now,
		Selection:  m.selection,
		StageDepth: m.stage.Depth(),
	}
	ev := plugin.Event{Kind: plugin.EventTick}
	// **舞台顶层的视图**也算一份，不管它是借调来的还是常驻看板。
	//
	// ⚠️ 早期这里加了 `m.stage.Borrowing()` 的条件，于是"没有借调时"
	// 看板（作为 stage top）永远收不到 Tick——字条因此不轮换。
	// 而 Tick 的语义本来就是"把时间推进告诉当前在顶部的那一层"，
	// 加不加借调条件跟它没关系。测试 TestQuotesRotateOnTick 钉住了这一点。
	if top := m.stage.Top(); top != nil {
		if act, close := top.Update(ctx, ev); close {
			if origin, ok := m.stage.Pop(); ok {
				m.focusBack(origin)
			}
		} else {
			m.runAction(act)
		}
	}
	for _, a := range m.registry.Anchors() {
		slot, ok := m.registry.At(a)
		if !ok || slot.Component == nil {
			continue
		}
		m.runAction(slot.Component.Update(ctx, ev))
	}
}

// applyFocusDefaults 把焦点放到第一个可用槽位。
//
// 焦点必须有归宿：没有焦点时按键无处可去，用户会觉得"程序卡住了"。
//
// 舞台（geometry.AnchorStage）也参与这个归属判断：它是伪锚点，
// 代表"中栏那块被借调出去的地方"。为什么它要能拿焦点，见
// geometry.AnchorStage 的说明（用户要求"可以通过 TAB 回到舞台按方向键"）。
func (m *Model) applyFocusDefaults() {
	// 舞台上有借调内容时，焦点优先落在舞台：那是用户刚打开的东西。
	if m.stage.Borrowing() {
		m.focus = geometry.AnchorStage
		return
	}
	if m.focus == geometry.AnchorStage {
		// 舞台已经空了：焦点必须离开它，否则按键会落在一个不存在的东西上。
		m.focus = geometry.AnchorUnset
	}
	if m.focus.IsSlot() {
		if _, ok := m.registry.At(m.focus); ok {
			return
		}
	}
	for _, a := range geometry.AllAnchors {
		if _, ok := m.registry.At(a); ok {
			m.focus = a
			// 初次落焦点同样要把选中对齐：否则开局就存在
			// "高亮在 A、选中为空"的错位，用户按 l 会得到"没有操作"。
			m.syncSelectionToFocus()
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
	// 每帧重算下栏提示与上下栏内容：它们跟着光标与当前状态变。
	m.refreshChrome()
	m.refreshHints()

	// 1) 上下栏。
	m.header.Draw(m.canvas, m.shell.Header)
	m.footer.Draw(m.canvas, m.shell.Footer)

	// 2) 主舞台（先画）。
	//
	// 顺序很关键：栈空时舞台画的是**看板底色**，它是背景；
	// 磁贴是前景，必须后画、画在上面。曾经顺序是反的（先磁贴后舞台），
	// 那时看板很窄所以看不出问题；一旦看板拿到完整宽度，
	// 它就变成一块大底板，把先画的磁贴整片盖掉——
	// 画面上表现为"侧栏的框被擦掉了、只剩几根线"。
	//
	// 舞台拿到的是"整行横向跨度 + 主控区纵向跨度"（见 stageRect），
	// 因此它绝不会盖住停靠区；而侧栏由磁贴后画覆盖，也不会被擦掉。
	if stageRect := m.stageRect(); !stageRect.Empty() {
		sctx := m.renderCtx()
		// 借调内容在舞台上，因此"舞台获得焦点"就等于"这一层获得焦点"。
		sctx.Focus = m.focus == geometry.AnchorStage && m.stage.Borrowing()
		sctx.State = plugin.StateIdle
		if sctx.Focus {
			sctx.State = plugin.StateFocused
		}
		m.stage.Render(m.canvas, stageRect, sctx)
	}

	// 3) 磁贴（含中栏停靠区）——前景，画在舞台之上。
	m.registry.RenderAll(m.canvas, m.rectOf, m.renderCtx(), m.frame)

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

// drawToast 在上栏位置上画一行提示（**有意覆盖**上栏内容）。
//
// 它刻意不用浮层：v2.1.0 的教训是"铺满屏幕的居中浮层"会把左右面板
// 的边框切出断口，用户截图反馈"渲染坏了"。一行提示就地替换上栏最简单。
//
// 因为是有意覆盖，这里必须显式声明 overlay：否则画布会把这次覆盖记成
// "覆盖已有内容"，而那条不变量是全项目最重要的报警器——
// 不能为了一个提示条把它整体关掉（实测这条用例就是这样发现问题的）。
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
	restoreClip := m.canvas.PushClip(inner)
	defer restoreClip()
	restoreOverlay := m.canvas.BeginOverlay()
	defer restoreOverlay()
	m.canvas.ClearRect(inner)
	m.canvas.Text(inner.X, inner.Y, canvas.Truncate(text, inner.W), style)
}

// stageRect 返回舞台当前应当绘制的矩形。
//
// 规则只有一句：**舞台与停靠区共享中栏，但绝不重叠**。
//
//	停靠区可见 → 舞台 = Center.Stage：它与停靠区上下分中栏，
//	             横向也**不外借**——外借会盖到左右栏磁贴上
//	             （实测表现为看板文字横穿侧栏、侧栏的框被擦出缺口）。
//	停靠区不可见 → 舞台可用整个中栏（含其整高），窄终端下多放一屏内容。
//	借调中       → 永远只在主控区里，横向纵向都不越界。
//
// ⚠️ **不要把横向外借写成 `wide.Intersect(vertical)`**：
// wide 的 X 是整行起点（0），vertical 的 X 是中栏起点（34），
// 求交会拿 wide.X1()（整行中段的 56）当右边界，宽度被砍成 22 而不是 56
// ——实机表现为"终端明明很宽，看板里的文字却被折成很短几行"。
// 要的是"整行横向跨度 + 主控区纵向跨度"的**组合**，不是交集。
func (m *Model) stageRect() geometry.Rect {
	dockOn := m.shell.Center.DockVisible && !m.shell.Center.Dock.Empty()
	if dockOn || m.stage.Borrowing() {
		return m.shell.Center.Stage
	}
	if r := m.shell.Center.Rect; !r.Empty() {
		return r
	}
	return m.shell.Body
}

// StageRect 返回舞台当前实际绘制的矩形（供宿主与测试查询）。
//
// 它是内部规则的只读出口：测试据此断言"底色不越界"，
// 宿主据此在需要时对齐自己的浮层。
func (m *Model) StageRect() geometry.Rect { return m.stageRect() }

// refreshHints 重算下栏的按键提示。
//
// 提示的来源有三层，从具体到通用：
//
//  1. 借调中的视图自己申报（它有完全不同的按键集，如编辑器的 ctrl+s）；
//  2. 当前获得焦点的磁贴申报（"这里现在能按什么"只有它自己知道）；
//  3. 引擎兜底（tab/esc/q，任何地方都能走）。
//
// 用户的原话点明了这件事的必要性：
//
//	如果光标在无动作时不会触发什么东西，那么下栏的操作提示就需要
//	跟着光标的操作提示改变，显然这种提示需要插件包提供。
func (m *Model) refreshHints() {
	m.footer.Hints = m.computeHints()
}

// computeHints 计算**当前**应有的按键提示。
//
// 顺序上从具体到通用：借调视图 → 焦点磁贴 → 引擎兜底（tab/q）。
//
// 它与 refreshHints 分开，是因为**查询与渲染必须同源**：
// 曾经 Hints() 直接读 footer 里的缓存，而缓存只在 View() 时更新，
// 于是"按了键之后立刻问提示"拿到的是上一帧的旧值——
// 测试会看到提示不跟着变（那正是这个功能要保证的事）。
func (m *Model) computeHints() []chrome.KeymapHint {
	var hints []chrome.KeymapHint

	if top := m.stage.Top(); top != nil && m.stage.Borrowing() {
		// 借调视图：它自己说了算（实现 KeyHinter 时）。
		if h, ok := top.(plugin.KeyHinter); ok {
			for _, k := range h.KeyHints(m.renderCtx()) {
				hints = append(hints, chrome.KeymapHint{Key: k.Key, Desc: k.Desc})
			}
		}
	} else if slot, ok := m.registry.At(m.focus); ok && slot.Component != nil {
		ctx := m.renderCtx()
		ctx.Focus = true
		for _, k := range plugin.KeyHintsOf(slot.Component, ctx) {
			hints = append(hints, chrome.KeymapHint{Key: k.Key, Desc: k.Desc})
		}
	}

	// 换栏位与退出在任何位置都有意义，因此永远附在末尾。
	return append(hints,
		chrome.KeymapHint{Key: "tab", Desc: "换栏位"},
		chrome.KeymapHint{Key: "q", Desc: "退出"},
	)
}

// Hints 返回**当前**下栏应有的按键提示（供宿主与测试查询）。
//
// 它每次现算，不读渲染缓存：缓存只在 View() 时更新，
// 而"按了键之后立刻问提示"必须拿到新的（那正是这个功能要保证的事）。
func (m *Model) Hints() []chrome.KeymapHint { return m.computeHints() }

// refreshChrome 刷新上下栏的内容。
//
// 两件事都是"引擎把通用位置让给插件去填"（用户反馈：上下栏用得不多）：
//
//	上栏 → 内核提供（今天是哪天、完成多少、现在几点：都是业务概念）
//	下栏进度条 → **正在跑那件事的组件**提供（计时磁贴知道专注走到几分之几）
//
// 引擎仍然不认识"计时""待办"这些词：它只问接口、画文本、画进度条。
func (m *Model) refreshChrome() {
	// 上栏。
	if k := m.manager.Kernel(); k != nil {
		if hc, ok := plugin.HeaderContentOf(k, m.renderCtx()); ok {
			m.header.Left = hc.Left
			m.header.Right = hc.Right
		}
	}

	// 下栏进度条：问所有已安置的组件，用第一个申报的。
	//
	// 只取第一个而不是画多条：下栏只有一行，多条进度条会互相挤掉；
	// 而"同时有两件事在跑"本身是罕见情况（目前只有计时）。
	if progress, text, ok := m.footerStatus(); ok {
		m.footer.HasProgress = true
		m.footer.Progress = progress
		if text != "" {
			m.footer.Text = text
		}
	} else {
		m.footer.HasProgress = false
		m.footer.Progress = 0
		// 没有进行中的事：回到默认状态文本（"Power by …"）。
		if k := m.manager.Kernel(); k != nil {
			m.footer.Text = k.PowerBy()
		}
	}
}

// clamp01 把比值夹到 [0,1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// FooterText 返回下栏左侧的当前文本（供测试与宿主查询）。
//
// 它与 Hints 一样是"现算"的出口：缓存只在 View() 时更新，
// 而"改完状态立刻问界面"必须拿到新的。
func (m *Model) FooterText() string {
	// 复用 refreshChrome 的判定：有进度条用它的文字，否则用默认状态文本。
	if _, text, ok := m.footerStatus(); ok {
		return text
	}
	if k := m.manager.Kernel(); k != nil {
		return k.PowerBy()
	}
	return m.footer.Text
}

// footerStatus 返回当前应当显示的进度条信息（没有则 ok 为假）。
func (m *Model) footerStatus() (float64, string, bool) {
	for _, a := range m.registry.Anchors() {
		slot, ok := m.registry.At(a)
		if !ok || slot.Component == nil {
			continue
		}
		if progress, text, ok := plugin.FooterProgressOf(slot.Component, m.renderCtx()); ok {
			return clamp01(progress), text, true
		}
	}
	return 0, "", false
}

// AllTiles 返回全部**已装载**的磁贴（含未安置、被隐藏的）。
//
// 视图设置界面需要它：用户要能看到"有哪些磁贴可以摆"，
// 而不仅仅是"现在摆出来的那几个"。因此这里从管理器问，
// 而不是从注册表（注册表只有已安置的）。
func (m *Model) AllTiles() []plugin.TileRef { return m.manager.AllTiles() }

// ViewConfig 返回当前的视图配置（供宿主保存与界面展示）。
func (m *Model) ViewConfig() plugin.ViewConfig { return m.viewCfg }

// ApplyViewConfig 换一份视图配置并立即重新安置。
//
// 视图设置改完就是这样生效的：改配置 → 重新安置 → 重绘。
// 不做"增量搬动"是因为那需要处理一大堆中间冲突（两边互占、连锁让位），
// 而重新安置的规则已经被测试穷举过（design §4.7）。
func (m *Model) ApplyViewConfig(vc plugin.ViewConfig) {
	m.viewCfg = vc
	m.registry = tile.NewRegistry()
	m.placeTiles()
	m.relayout()
	m.applyFocusDefaults()
}

// openQuitConfirm 推出"确定退出吗"的确认层。
//
// 默认选项是**取消**（光标停在第一项）：退出确认的目的是拦住误按，
// 而不是让用户多按一次回车。
func (m *Model) openQuitConfirm() {
	if m.quitPending {
		return
	}
	m.quitPending = true
	choices := []string{"取消，继续使用", "退出 KQFLOW"}
	cursor := 0
	view := &plugin.ViewFunc{
		ViewName:    "确认退出",
		FocusLockFn: func() bool { return true },
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "选择"},
				{Key: "enter", Desc: "确认"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			// 只画单行、超宽就截断：确认框不需要折行逻辑，
			// 而且它在极小窗口下也必须能画出来（否则用户会卡在
			// 一个看不见的确认层里——那比不能退出更糟）。
			line := func(y int, s string, style canvas.StyleID) {
				if y < ctx.Rect.Y || y >= ctx.Rect.Y1() {
					return
				}
				m.canvas.Text(ctx.Rect.X, y, canvas.Truncate(s, ctx.Rect.W), style)
			}
			y := ctx.Rect.Y
			line(y, "确定要退出 KQFLOW 吗？", tile.StyleTitle)
			y += 2
			for i, c := range choices {
				mark := "  "
				style := tile.StyleMuted
				if i == cursor {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				line(y, mark+c, style)
				y++
			}
			line(y+1, "未保存的改动会随退出丢失", tile.StyleMuted)
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "j", "down":
				cursor = (cursor + 1) % len(choices)
				return plugin.None(), false
			case "k", "up":
				cursor = (cursor - 1 + len(choices)) % len(choices)
				return plugin.None(), false
			case "enter", " ":
				if cursor == 0 {
					return plugin.None(), true
				}
				return plugin.Quit(), true
			}
			return plugin.None(), false
		},
	}
	// origin 记下进来之前的焦点，取消后能回到原处。
	m.stage.Push(view, plugin.Origin{})
	m.focus = geometry.AnchorStage
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
//
// 焦点一变就**必须重算选中**：否则"高亮在 A、选中还是 B"，
// 而 l 会作用在 B 上——屏幕上完全看不出来（用户实测到的错位 bug）。
func (m *Model) SetFocus(a geometry.Anchor) {
	// 舞台是合法的焦点目标，但只在**有人借调**时可聚焦：
	// 栈空时的看板是底色，不是"一种磁贴"，不该抢焦点。
	if a == geometry.AnchorStage {
		if m.stage.Borrowing() {
			m.focus = a
		}
		return
	}
	if !a.IsSlot() {
		return
	}
	if _, ok := m.registry.At(a); !ok {
		return
	}
	m.focus = a
	m.syncSelectionToFocus()
}

// syncSelectionToFocus 让"选中"跟着焦点走。
//
// 这是用户第 3 条反馈的落点：tab 之后观感上已经选中了新磁贴的第一项，
// 内部状态就必须同意这件事。做法是把决定权交给**磁贴自己**
// （它知道自己光标在哪、有没有条目），而不是引擎去猜。
func (m *Model) syncSelectionToFocus() {
	if m.focus == geometry.AnchorStage {
		return // 舞台上的视图不需要"选中条目"这个概念
	}
	slot, ok := m.registry.At(m.focus)
	if !ok || slot.Component == nil {
		return
	}
	ctx := m.renderCtx()
	ctx.Focus = true
	if sel, ok := plugin.FocusSelectionOf(slot.Component, ctx); ok {
		m.selection = sel
	}
}

// FocusNext 把焦点移到下一个目标（tab 的行为）。
//
// 焦点环 = 已占用槽位 + （有人借调时）舞台本身。
// 舞台排在最后：它是"中栏"，放在磁贴之后符合"从外往里"的直觉。
func (m *Model) FocusNext(delta int) {
	targets := m.focusTargets()
	if len(targets) == 0 {
		m.focus = geometry.AnchorUnset
		return
	}
	cur := -1
	for i, a := range targets {
		if a == m.focus {
			cur = i
			break
		}
	}
	next := (cur + delta) % len(targets)
	if next < 0 {
		next += len(targets)
	}
	m.focus = targets[next]
	// 焦点换了，选中必须跟着换（见 syncSelectionToFocus）。
	m.syncSelectionToFocus()
}

// focusTargets 返回当前的焦点环。
func (m *Model) focusTargets() []geometry.Anchor {
	targets := append([]geometry.Anchor{}, m.registry.Anchors()...)
	if m.stage.Borrowing() {
		targets = append(targets, geometry.AnchorStage)
	}
	return targets
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
	// 每轮开始先清掉上一轮的退出请求标记。
	//
	// 它必须在这里清（而不是在读取处）：万一某条分支提前 return，
	// 标记会留到下一次调用，于是"上一轮的退出"在下一轮突然生效——
	// 这种延迟触发的行为极难排查。
	m.quitRequested = false
	defer func() {
		if m.quitRequested {
			requestQuit = true
		}
	}()

	ctx := plugin.EventCtx{
		Now:        m.svc.Clock(),
		Selection:  m.selection,
		StageDepth: m.stage.Depth(),
	}

	// 提示是可以被任意按键清掉的：它是临时信息，不该拦着用户干活。
	if ev.Kind == plugin.EventKey && m.currentToast() != "" {
		m.toast = ""
	}

	// 焦点被"未决事务"锁住时，tab 不再切换焦点。
	// 这一条是实机反馈的直接落点：菜单打开着、用户按 tab 走开，
	// 事务就退化成"一个恰好画在中栏的东西"，用户再也回不来。
	locked := plugin.IsFocusLocked(m.stage.Top())

	// 退出确认层自己处理按键（它需要收 q，而不是再叠一层）。
	if m.quitPending {
		if top := m.stage.Top(); top != nil {
			act, close := top.Update(ctx, ev)
			m.runAction(act)
			if close {
				m.stage.Pop()
				m.quitPending = false
				if !m.quitRequested {
					m.focusBack(plugin.Origin{})
				}
			}
			return m.quitRequested
		}
		// 层不见了（不该发生）：把标记清掉，免得谁也退不出去。
		m.quitPending = false
	}

	// 全局按键：先处理"谁能拿到焦点"这类与具体磁贴无关的动作。
	switch ev.Key {
	case "ctrl+c":
		return true
	case "q":
		// q 不直接退出，而是**先问一句**。
		//
		// 与 2.1.0 对齐（它的 askQuit 也是"取消"在第一位、默认选中它）。
		// 理由不只是"怕误触"：本程序的数据都在内存里改、按需落盘，
		// 而 q 就在下栏提示里写着，误按一次就丢一屏状态（例如正写着的
		// 随手记、正跑着的计时）。让默认选项是"继续使用"，
		// 才能让随手按 q 的人什么都不丢。
		//
		// ⚠️ 这条以前是**完全缺失**的：下栏一直显示"q:退出"，
		// 但引擎压根没有处理 q 的分支——按了没反应。功能没做出来
		// 比做错了更隐蔽，因为没有任何报错。
		m.openQuitConfirm()
		return false
	case "tab":
		if locked {
			m.Toast("请先处理当前操作（esc 取消）")
			return false
		}
		m.FocusNext(1)
		return false
	case "shift+tab":
		if locked {
			m.Toast("请先处理当前操作（esc 取消）")
			return false
		}
		m.FocusNext(-1)
		return false
	case "esc":
		// 当前焦点磁贴如果是**模态**（占用 esc），esc 归它
		//（子任务模式就是这种：esc 是"从子任务退回条目"，
		//  不是"退出上一层界面"）。
		//
		// 必须在全局 pop 之前问：否则用户在子任务里按 esc 会直接把
		// 借调层关掉，而子任务模式还开着——两层状态就此错位。
		if slot, ok := m.registry.At(m.focus); ok && slot.Component != nil &&
			plugin.OwnsEsc(slot.Component) {
			break // 落到下面的磁贴分派去
		}
		// 借调视图也能占 esc（编辑器要自己决定"有未保存改动时怎么办"）。
		// 同理必须在 pop 之前问。
		if top := m.stage.Top(); m.stage.Borrowing() && top != nil {
			if owner, ok := top.(plugin.ModalOwner); ok && owner.OwnsEsc() {
				break // 落到下面的舞台分派去
			}
		}
		// esc 的语义：先退出借调，退出不了才算"没处可去"。
		if origin, ok := m.stage.Pop(); ok {
			m.focusBack(origin)
			return false
		}
	}

	// 舞台层：**没有借调时**它作为"默认看板"存在，但仍可被 tab 聚焦。
	//
	// 焦点在舞台上时，按键交给舞台层处理（而不是穿透到磁贴）——
	// 这正是用户要求的"可以通过 TAB 回到舞台按方向键"。
	// tab/shift+tab/esc 已在上面处理过，因此这里只可能是别的按键。
	if top := m.stage.Top(); top != nil {
		if m.stage.Borrowing() || m.focus == geometry.AnchorStage {
			act, close := top.Update(ctx, ev)
			m.runAction(act)
			if close {
				if origin, ok := m.stage.Pop(); ok {
					m.focusBack(origin)
				}
			}
			return false
		}
	}

	// 打开"未决事务"：把当前光标所在项的操作菜单推上舞台。
	//
	// 用**按键**触发而不是"光标一移到就自动弹"：后者会让选项跟着光标
	// 变来变去，用户一走神就分不清"我现在到底在操作谁"。
	// 这也是 v2.1.0 的语义（按 L 打开选中项的操作菜单）。
	//
	// 光标本身**只负责悬停**：它移动时唯一的副作用是下栏提示跟着变
	// （见 refreshHints），中栏看板不因此改变。
	if ev.Key == "l" && !m.stage.Borrowing() {
		if m.openSelectionOptions() {
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

// openSelectionOptions 打开"未决事务"：操作菜单。
//
// 菜单里有两段（顺序即优先级）：
//
//	当前条目：联动选项——作用在光标所指的那一条上（可能为空）
//	通用    ：看板选项——永远可用（开始专注、帮助、关于…）
//
// 为什么把两段合到一个菜单里（用户 2026-10-05 的反馈）：
//
//	原话：不应该保留数字作为快捷键：如果有 10 个选项怎么办呢？
//
// 去掉数字键之后，"常驻选项"就失去了触发方式。与其再发明一套按键，
// 不如让 **l 成为唯一的入口**：菜单里既有针对当前条目的事，
// 也有到处都能做的事。数量不受限（方向键滚动），也不会与字母键打架。
//
// 返回 false 表示连通用选项都没有（此时按键继续传给磁贴）。
func (m *Model) openSelectionOptions() bool {
	opts := m.Options()
	if len(opts) == 0 {
		m.Toast("当前没有可执行的操作")
		return true
	}
	// 记下"是谁打开的"：关闭事务时焦点要交还给当时获得焦点的那个磁贴。
	// 不记的话 focusBack 找不到目标，只能退回第一个磁贴——
	// 表现为"esc 之后焦点莫名跳到了左上角"。
	origin := plugin.Origin{Kind: plugin.OriginContextOption}
	if slot, ok := m.registry.At(m.focus); ok {
		origin.TileID = slot.PluginID
		origin.Anchor = slot.Anchor
	}
	m.stage.Push(m.newOptionsMenu(opts, origin), origin)
	m.focus = geometry.AnchorStage
	return true
}

// newOptionsMenu 构造操作菜单视图。
//
// 它是一个**未决事务**：独占焦点（FocusLock），玩家必须选一项或按 esc 取消，
// 期间 tab 无效。菜单自己按方向键选择、回车确认。
func (m *Model) newOptionsMenu(opts []OptionBinding, menuOrigin plugin.Origin) plugin.View {
	cursor := 0
	var status string
	return &plugin.ViewFunc{
		ViewName:    "操作",
		FocusLockFn: func() bool { return true },
		// 菜单自己申报按键：它有一套与看板完全不同的操作。
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "选择"},
				{Key: "enter", Desc: "执行"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			putLine := func(y int, s string, style canvas.StyleID) {
				if y < ctx.Rect.Y1() {
					ctx.Canvas.Text(ctx.Rect.X, y, canvas.Truncate(s, ctx.Rect.W), style)
				}
			}
			y := ctx.Rect.Y
			putLine(y, "可用操作", tile.StyleTitle)
			// 提示选中了谁：菜单里的操作都作用在这一条上。
			if m.selection.Title != "" {
				putLine(y+1, "作用于："+m.selection.Title, tile.StyleStatus)
			}
			y += 3
			for i, b := range opts {
				if y >= ctx.Rect.Y1() {
					break
				}
				mark := "  "
				style := tile.StyleMuted
				if i == cursor {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				putLine(y, mark+b.Label, style)
				y++
			}
			y++
			putLine(y, "↑↓ 选择 · enter 执行 · esc 取消", tile.StyleHint)
			if status != "" && y+1 < ctx.Rect.Y1() {
				putLine(y+1, status, tile.StyleMuted)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc", "q":
				return plugin.None(), true
			case "j", "down":
				cursor = (cursor + 1) % len(opts)
				return plugin.None(), false
			case "k", "up":
				cursor = (cursor - 1 + len(opts)) % len(opts)
				return plugin.None(), false
			case "enter", " ":
				b := opts[cursor]
				view, err := b.Activate(m.svc, m.selection)
				if err != nil {
					status = "打开失败：" + err.Error()
					return plugin.None(), false
				}
				if view == nil {
					status = "这个操作没有界面"
					return plugin.None(), false
				}
				// 把真正的编辑界面推在菜单之上：esc 先从编辑界面退回菜单，
				// 再 esc 才关闭事务。这样"填错了"不必重开菜单。
				//
				// 但 origin 要记**进入菜单之前**的那个磁贴，而不是"菜单"。
				// 否则关闭整条事务时 focusBack 找不到目标，只能退回第一个磁贴——
				// 表现为"从设置页出来焦点莫名跳到了左上角"。
				return plugin.Action{
					Kind:    plugin.ActionBorrow,
					Payload: plugin.BorrowRequest{View: view, Origin: menuOrigin},
				}, false
			}
			return plugin.None(), false
		},
	}
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
	// 没有可交还的磁贴：如果栈里还有一层（例如菜单之上打开了编辑页），
	// 焦点应当留在舞台；否则回到第一个磁贴，别把焦点丢给不存在的舞台。
	if m.stage.Borrowing() && origin.Kind == plugin.OriginContextOption {
		m.focus = geometry.AnchorStage
		return
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
	case plugin.ActionQuit:
		// 记下来，由 Dispatch 统一返回。
		//
		// 为什么不当场返回：runAction 没有返回值，而"是否退出"必须
		// 沿着 Dispatch → 宿主 这条唯一的路走上去（宿主才知道怎么收尾：
		// 打印小结、恢复终端）。中间加一条隐蔽的旁路，就等于
		// "退出"有两条路径了——而它们迟早会不一致。
		m.quitRequested = true
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
