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
// 注意它**只有 Drawing 相关的东西**：没有任何"去写盘""去播声音"的入口。
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
	// Palette 由画布使用；组件需要自己上色时从这里取。
	Palette canvas.Palette
	// Selection 是引擎维护的当前选中上下文（只读）。
	Selection Selection
	// Svc 只读能力（时钟、环境能力）。**不提供 Persist/Effect**：
	// 渲染期不允许产生副作用。
	Svc ReadOnlyServices
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
)

// Action 是插件返回的意图。
type Action struct {
	Kind    ActionKind
	Payload any
}

// None 是"什么都不做"的便捷构造函数。
func None() Action { return Action{Kind: ActionNone} }

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
