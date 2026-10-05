package plugin

import (
	"fmt"
	"strings"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// Manifest 描述一个插件。**版本与冲突裁决只依据这里**，不看注册顺序。
type Manifest struct {
	// ID 全局唯一，建议 "kqflow.todo.fixed"（包 ID 加点后缀）。
	ID string
	// Name 是显示名（中文），会出现在装载报告与视图配置界面上。
	Name string
	Kind Kind
	// Version 是插件自身版本（接口层面的版本，引擎看这个）。
	Version semver.Version
	// EngineAPI 是本插件需要的 KXFLOW API 版本范围，如 ">=0.1 <0.2"。
	//
	// **必须显式声明**：零值（不限制）被视为不合法并由 Validate 拦下。
	// 理由见 SKILL pitfalls 的教训——校验器遇到"没声明"时默认应当拦住，
	// 而不是放行；否则将来引擎抬版本时会静默装上一个不兼容的插件。
	EngineAPI semver.Range
	// DataSchema 是它读写的数据结构版本（对应 KQFLOW 的 schema_version）。
	// 0 表示"不碰持久化数据"（纯界面插件）。
	DataSchema int
	// Slots 只有磁贴才有意义：期望放在哪里。
	Slots SlotPreference
}

// Validate 检查 Manifest 自身是否合法。返回 nil 表示可以参与装载。
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("插件缺少 ID")
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("插件 %s 缺少显示名", m.ID)
	}
	if m.EngineAPI.IsAny() {
		return fmt.Errorf("插件 %s 未声明 EngineAPI 版本范围（必须显式写，如 \">=0.1 <0.2\"）", m.ID)
	}
	if m.Kind == KindTile {
		if err := m.Slots.Validate(); err != nil {
			return fmt.Errorf("磁贴 %s：%w", m.ID, err)
		}
	}
	if m.Kind != KindTile && !m.Slots.IsZero() {
		return fmt.Errorf("插件 %s 是%s，不应声明磁贴槽位偏好", m.ID, m.Kind)
	}
	return nil
}

// SlotPreference 是磁贴期望的落位。
//
// 它只是**期望**：真正的安置由 ViewConfig（用户配置）优先决定，
// 未被用户显式安置的磁贴才按这里排序（design §6）。
type SlotPreference struct {
	// Anchor 是期望的锚点；AnchorUnset 表示"随便放，按优先级排"。
	Anchor geometry.Anchor
	// Priority 越大越优先占用槽位；同优先级按 ID 字典序，保证结果可复现。
	Priority int
}

// IsZero 报告没有声明任何偏好。
func (s SlotPreference) IsZero() bool {
	return s.Anchor == geometry.AnchorUnset && s.Priority == 0
}

// Validate 检查槽位偏好合法。
func (s SlotPreference) Validate() error {
	if !s.Anchor.Valid() {
		return fmt.Errorf("槽位锚点 %d 不是合法的物理位置", s.Anchor)
	}
	return nil
}

// Component 是"已经装配好、可以渲染与收事件"的组件。
//
// FocusReporter 是可选能力：组件在**获得焦点时**回报"我这儿默认选中的是谁"。
//
// 为什么必须有它（用户 2026-10-05 的反馈，一个真实的错位 bug）：
//
//	原话：我按下 TAB 按键后光标实际上指向了什么都不是，
//	但是内部其实还指在旧的位置（因为 TAB 不更新光标位置，
//	但是会影响渲染）这就导致明明我已经 TAB 到其他位置，
//	实际上光标还是选着之前最后一个选项，
//	并且 TAB 也不会自动选中磁贴中的第一个选项（但是观感和直觉是选中了）。
//
// 根因是"高亮"与"选中"是两套状态：高亮由焦点决定（渲染时算），
// 选中由磁贴的 j/k 上报（按键时算）。只按 tab 不改选中，
// 于是 l 会作用在**上一个磁贴的那一条**上——屏幕上完全看不出来。
//
// 因此焦点一变就必须重新问一次"你现在选中谁"：
// 列表类磁贴返回光标所指的那一条（因此"自动选中第一条"是自然结果），
// 没有可选项的磁贴返回零值 Selection（表示"这里没东西可选"）。
type FocusReporter interface {
	// FocusSelection 返回本组件在获得焦点时应上报的选中项。
	//
	// 实现必须是**廉价且纯**的：焦点每次变化都会调用它。
	FocusSelection(ctx RenderCtx) Selection
}

// FocusSelectionOf 取组件在获得焦点时的默认选中（未实现则返回零值）。
func FocusSelectionOf(c Component, ctx RenderCtx) (Selection, bool) {
	if r, ok := c.(FocusReporter); ok {
		return r.FocusSelection(ctx), true
	}
	return Selection{}, false
}

// Component 是"可渲染的东西"（磁贴、内核、视图内容都实现它）。
//
// Render 不返回值：内容只能画进 ctx.Canvas 的 ctx.Rect 里，
// 而画布的裁剪区保证它**画不出去**（design §0.1 的第 3 条保证）。
type Component interface {
	// Title 是磁贴标题栏文本（不含快捷键记号）。
	Title() string
	// Render 把内容画进自己的矩形。
	Render(ctx RenderCtx)
	// Update 处理落在本组件上的事件，返回意图（由内核解释）。
	Update(ctx EventCtx, ev Event) Action
}

// RenderCtx 是一次渲染所需的全部上下文。
//
// 注意它**只有绘制相关的东西**：没有任何"去写盘""去播声音"的入口。
// 那些只能通过 Update 返回 Action 来请求——这是"渲染是纯的"的机制保障。
type RenderCtx struct {
	Canvas *canvas.Canvas
	// Rect 是内容区（已去掉边框与内边距）——**已经保证合法**，
	// 调用方不需要再做边界判断。
	Rect geometry.Rect
	// Frame 是含边框的外框，供需要自己画边框的组件使用。
	Frame geometry.Rect
	// State 描述本组件当前的活跃状态。
	State ComponentState
	// Focus 是 State == StateFocused 的便捷判断。
	//
	// 同时提供两者是因为它们用途不同：State 用于"整体外观该怎样"，
	// Focus 用于"要不要画光标/高亮"。让调用方每次都写
	// `ctx.State == StateFocused` 容易写错（漏一个等号就是静默失效）。
	Focus bool
	// Palette 由画布使用；组件需要自己上色时从这里取。
	Palette canvas.Palette
	// Selection 是引擎维护的当前选中上下文（只读）。
	Selection Selection
	// Svc 只读能力（时钟、环境能力）。**不提供 Persist/Effect**：
	// 渲染期不允许产生副作用。
	Svc ReadOnlyServices
}

// Wrap 把一段文本按**当前可用宽度**折成多行。
//
// 磁贴这类小对象必须用它，而不是 Truncate：
//
//	tile 只有 14 列宽时，一句"（焦点在本磁贴时按 enter 会上报选中）"被截断后
//	只剩下"（焦点在本磁贴时按 enter 会上"——用户看到的是半句话，
//	会以为渲染坏了（这是**真实反馈**，不是假设）。
//
// 折行规则与全局一致（按显示宽度、宽字符不劈开），因为最终走的是
// canvas.Wrap——全项目唯一的折行实现。
//
// 用法（画一段可能超宽的文字）：
//
//	for i, line := range ctx.Wrap(text) {
//	    if i >= ctx.Rect.H { break }   // 行数也不够时才算真的放不下
//	    ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y+i, line, style)
//	}
//
// 需要"连行内剩余空间一起填满、续行顶到左边"的效果时用 WrapInset。
func (ctx RenderCtx) Wrap(text string) []string {
	return canvas.Wrap(text, maxInt(ctx.Rect.W, 1))
}

// WrapLines 对多段文本逐段折行，返回可直接逐行绘制的行序列。
//
// 它把"逐段折行 + 按高度截断"收在一处：这两件事分开写时，
// 每处渲染都要重复一遍，而漏掉高度检查就会出现"多画的行被裁掉"
// 或"画到别人地盘上"。
func (ctx RenderCtx) WrapLines(lines []string) []string {
	out := make([]string, 0, len(lines)+4)
	for _, l := range lines {
		out = append(out, canvas.Wrap(l, maxInt(ctx.Rect.W, 1))...)
	}
	if len(out) > ctx.Rect.H {
		out = out[:ctx.Rect.H]
	}
	return out
}

// WrapInset 在折行时为每行预留缩进，续行也顶到同一缩进。
//
// 用于"前缀 + 长文本"的排版：例如"  1 ✔ 星星"。不这样做的话，
// 折行后的续行会从最左边开始，看起来像是另起一段。
func (ctx RenderCtx) WrapInset(prefix, text string, indent int) []string {
	indent = maxInt(indent, 0)
	avail := maxInt(ctx.Rect.W-indent, 1)
	head := canvas.Wrap(prefix+text, avail)
	for i := range head {
		head[i] = strings.Repeat(" ", indent) + head[i]
	}
	return head
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ReadOnlyServices 是渲染期可见的能力子集。
type ReadOnlyServices interface {
	Clock() time.Time
	Capabilities() svc.Capability
}

// EventCtx 是一次事件处理所需的上下文。
type EventCtx struct {
	// Now 是本次事件的时间戳（引擎统一注入，便于测试确定性）。
	Now time.Time
	// Selection 是引擎维护的当前选中上下文。
	Selection Selection
	// Focused 报告本组件当前是否持有键盘焦点。
	Focused bool
	// StageDepth 报告舞台视图栈深度（>0 表示有视图正在借调主控区）。
	StageDepth int
}

// ComponentState 描述组件当前的活跃状态。
type ComponentState uint8

const (
	// StateIdle 空闲：只渲染，不接收按键。
	StateIdle ComponentState = iota
	// StateFocused 持有焦点：渲染高亮并接收按键。
	StateFocused
	// StateCollapsed 被用户折叠：只渲染标题。
	StateCollapsed
	// StateOccupied 被舞台借调占用：只渲染，不接收按键。
	// 它对应"磁贴把主控区借出去了，焦点在舞台上"。
	StateOccupied
)

// KeyHint 是组件向下栏申报的一条"我现在能按什么键"。
type KeyHint struct {
	// Key 是按键名（如 "space" / "enter" / "l"）。
	Key string
	// Desc 是它的含义（如 "勾选" / "借调"）。
	Desc string
}

// KeyHinter 是可选能力：组件据此申报**当前**可用的按键。
//
// 为什么要它（用户的原话）：
//
//	如果光标在无动作时不会触发什么东西，那么下栏的操作提示就需要
//	跟着光标的操作提示改变，显然这种提示需要插件包提供。
//
// 也就是说：光标只负责**悬停**，它一移动下栏提示就该跟着变；
// 而"这个位置能按什么"只有组件自己知道（它知道自己有没有条目、
// 有没有正在跑的东西）。引擎不该去猜。
//
// 用可选接口而不是往 Component 里加方法：不申报提示的组件
// （内核、纯展示磁贴）不该被迫实现一个恒返回空的方法。
// 引擎在组件没实现它时会退回到一组通用提示（tab/esc/q）。
type KeyHinter interface {
	// KeyHints 返回当前上下文的按键提示；返回空表示"这里没什么可做的"。
	//
	// 实现必须是**廉价且纯**的：它每帧都会被调用，
	// 因此不该在里面读盘或做重活。
	KeyHints(ctx RenderCtx) []KeyHint
}

// KeyHintsOf 取一个组件的按键提示（未实现该接口则返回 nil）。
func KeyHintsOf(c Component, ctx RenderCtx) []KeyHint {
	if h, ok := c.(KeyHinter); ok {
		return h.KeyHints(ctx)
	}
	return nil
}

// FooterStatus 是组件向下栏申报的"进行中任务"信息（进度条）。
//
// 为什么要有它（用户反馈："专注还没有进度条"）：进度条是下栏的通用能力，
// 但"现在进行到哪一步"只有**正在跑那件事的组件**知道——
// 计时磁贴知道专注走到几分之几，引擎不该去理解"什么是计时"。
//
// 实现必须是廉价且纯的（每帧都会问）。返回 ok 为假表示"我这会儿没什么
// 在跑的"，引擎会去问下一个组件。
type FooterStatus interface {
	// FooterProgress 返回进度（0~1）与要显示的文字。
	//
	// 进度会被夹到 [0,1]；文字可以为空（只画进度条）。
	FooterProgress(ctx RenderCtx) (progress float64, text string, ok bool)
}

// FooterProgressOf 取组件的下栏进度（未实现或无进度时 ok 为假）。
func FooterProgressOf(c Component, ctx RenderCtx) (float64, string, bool) {
	if s, ok := c.(FooterStatus); ok {
		return s.FooterProgress(ctx)
	}
	return 0, "", false
}

// HeaderContent 是内核向下栏/上栏申报的文本内容。
type HeaderContent struct {
	// Left 是上栏左侧文本（日期、问候、当天概况）。
	Left string
	// Right 是上栏右侧文本（时间、状态）。留空表示不画。
	Right string
}

// HeaderProvider 是可选能力：内核据此提供**上栏内容**。
//
// 为什么放在内核而不是引擎：上栏要显示的东西（今天是哪天、完成了多少、
// 现在几点）都是**业务概念**，引擎只该负责"把一段文本画在上栏"。
// 用户反馈"上栏和下栏用得不多"，根因就是这里没人提供内容。
type HeaderProvider interface {
	// HeaderContent 返回本帧的上栏内容。
	HeaderContent(ctx RenderCtx) HeaderContent
}

// HeaderContentOf 取内核提供的上栏内容（未实现则返回零值）。
func HeaderContentOf(k Kernel, ctx RenderCtx) (HeaderContent, bool) {
	if h, ok := k.(HeaderProvider); ok {
		return h.HeaderContent(ctx), true
	}
	return HeaderContent{}, false
}

// Event 是交给组件处理的事件。
//
// 定义成引擎自己的类型而不是直接用 bubbletea 的 KeyMsg：
// 引擎的组件接口不应绑死某个 TUI 框架，宿主才能换实现（也更好测试）。
type Event struct {
	// Key 是规范化后的按键名（"enter" / "esc" / "ctrl+s" / "j" …）。
	Key string
	// Runes 是本次输入的可见字符（粘贴或输入法上屏时可能多于一个）。
	Runes []rune
	// Paste 为真表示这些字符来自粘贴。
	Paste bool
	// Kind 区分类别，方便组件只关心自己在意的事件。
	Kind EventKind
}

// EventKind 是事件类别。
type EventKind uint8

const (
	// EventKey 键盘输入（普通按键）。
	EventKey EventKind = iota
	// EventPaste 粘贴。
	EventPaste
	// EventTick 定时推进（动画或秒级刷新）。
	EventTick
	// EventResize 终端尺寸变化。
	EventResize
)

// ActionKind 是插件返回的"意图"类别。
//
// 插件**不能**直接落盘或播音：它只能说"我想做什么"，由内核解释。
// 这就是"磁贴不知道存储、不知道庆祝动画存在"的机制。
type ActionKind uint8

const (
	// ActionNone 什么都不做。
	ActionNone ActionKind = iota
	// ActionPersist 请求持久化（Payload 由宿主约定）。
	ActionPersist
	// ActionEffect 请求播一次副作用（响铃/流光/推送/提示）。
	ActionEffect
	// ActionBorrow 请求借调舞台显示一个视图（Payload 是 BorrowRequest）。
	ActionBorrow
	// ActionSelect 上报当前选中上下文（Payload 是 Selection）。
	ActionSelect
	// ActionToast 请求一条界面内提示。
	ActionToast
	// ActionClosePack 请求卸载自己所在的包（由用户确认后由内核执行）。
	ActionClosePack
	// ActionQuit 请求退出整个程序。
	//
	// 与 ctrl+c 的区别：ctrl+c 是"立刻走"（终端级中断，不给任何机会），
	// ActionQuit 是"经过确认、可以体面收尾"的退出（保存、打印小结）。
	// 插件一般不该直接返回它——退出确认由引擎负责（见 kxflow.openQuitConfirm）。
	ActionQuit
)

// Action 是插件返回的意图。
type Action struct {
	Kind    ActionKind
	Payload any
}

// None 是"什么都不做"的便捷构造函数。
func None() Action { return Action{Kind: ActionNone} }

// Quit 构造一个"退出程序"意图。
func Quit() Action { return Action{Kind: ActionQuit} }

// Persist 构造一个持久化意图。
func Persist(pluginID, dataKind string, payload any) Action {
	return Action{Kind: ActionPersist, Payload: svc.PersistRequest{
		PluginID: pluginID, Kind: dataKind, Payload: payload,
	}}
}

// Effect 构造一个副作用意图。
func EffectOf(e svc.Effect) Action {
	return Action{Kind: ActionEffect, Payload: e}
}

// Select 构造一个"我选中了某项"的上报。
func Select(s Selection) Action {
	return Action{Kind: ActionSelect, Payload: s}
}

// Toast 构造一条界面内提示。
func Toast(text string) Action {
	return Action{Kind: ActionToast, Payload: text}
}

// Selection 是引擎维护的"当前选中上下文"。
//
// **它由引擎持有，而不是某个磁贴私有**——这是联动选项能不认识任何磁贴的前提。
// 谁被选中由磁贴在 Update 里用 ActionSelect 上报，引擎归一化后广播给
// 所有联动选项，它们据此决定自己是否出现（design §4.2、§5.2）。
type Selection struct {
	// Kind 是选中对象的类别，如 "todo" / "goal" / "session" / ""（无选中）。
	Kind string
	// ID 是选中对象的稳定标识（空表示没有选中）。
	ID string
	// Title 供选项显示，例如"给「写架构文档」设截止时间"。
	Title string
	// Can 描述该选中对象支持哪些能力标记，联动选项据此判断是否适用。
	Can svc.Capability
}

// Empty 报告没有任何选中。
func (s Selection) Empty() bool { return s.Kind == "" || s.ID == "" }

// String 便于测试与装载报告可读。
func (s Selection) String() string {
	if s.Empty() {
		return "无选中"
	}
	return s.Kind + ":" + s.ID
}
