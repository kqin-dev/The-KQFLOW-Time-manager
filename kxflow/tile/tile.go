// Package tile 实现磁贴的注册与渲染外壳。
//
// 这里只做两件事：**管住"哪些磁贴在哪里"**，以及**给磁贴套上统一的边框与标题**。
// 业务内容一律由磁贴自己画——引擎不认识 TODO、GOAL、计时这些概念。
package tile

import (
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// Slot 是一个"已安置的磁贴"：组件 + 它的身份 + 它占的槽位。
type Slot struct {
	// PluginID 是这个磁贴的插件 ID（来自 Manifest，用于视图配置与焦点交还）。
	PluginID string
	// PackID 是它所属的整合包（用于诊断"这块磁贴是谁提供的"）。
	PackID string
	// Name 是显示名。
	Name string
	// Anchor 是它所在的物理槽位。
	Anchor geometry.Anchor
	// Component 是已经装配好的组件。
	Component plugin.Component
	// State 是它当前的活跃状态（由引擎每帧设置）。
	State plugin.ComponentState
}

// Registry 是已安置磁贴的集合。
//
// 它**不负责装载**（那是 plugin.Manager 的事），只负责"按槽位取磁贴"
// 与"按 ID 找磁贴"这两种查询。分开的理由：装载是启动期一次性行为，
// 而查询在每帧、每次按键都会发生，二者的失效模式与测试方式完全不同。
type Registry struct {
	slots map[geometry.Anchor]Slot
	// byID 是 ID → 槽位锚点的索引，用于焦点交还与视图配置。
	byID map[string]geometry.Anchor
	// order 保留安置顺序，便于诊断输出可复现。
	order []geometry.Anchor
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{slots: map[geometry.Anchor]Slot{}, byID: map[string]geometry.Anchor{}}
}

// Place 把一个磁贴放到槽位上。
//
// 同一个槽位只能有一个磁贴：重复放置会**替换**并返回被替换者，
// 由调用方决定要不要报出来（静默覆盖会让"我的磁贴不见了"无法解释）。
//
// 返回值的拷贝语义：返回的是值而不是指针，避免调用方拿着一个
// 指向 map 内部存储的指针，在后续 Place 之后突然指向了别的东西。
func (r *Registry) Place(s Slot) (replaced Slot, hadOld bool) {
	if !s.Anchor.IsSlot() {
		return Slot{}, false
	}
	if old, exists := r.slots[s.Anchor]; exists {
		delete(r.byID, old.PluginID)
		replaced, hadOld = old, true
	}
	r.slots[s.Anchor] = s
	if s.PluginID != "" {
		r.byID[s.PluginID] = s.Anchor
	}
	r.order = append(r.order, s.Anchor)
	return replaced, hadOld
}

// At 返回槽位上的磁贴。
func (r *Registry) At(a geometry.Anchor) (Slot, bool) {
	s, ok := r.slots[a]
	return s, ok
}

// ByID 按插件 ID 找磁贴。
func (r *Registry) ByID(id string) (Slot, bool) {
	a, ok := r.byID[id]
	if !ok {
		return Slot{}, false
	}
	s, ok := r.slots[a]
	return s, ok
}

// Anchors 返回已占用槽位的顺序列表（可复现：按安置先后）。
func (r *Registry) Anchors() []geometry.Anchor {
	out := make([]geometry.Anchor, 0, len(r.order))
	seen := map[geometry.Anchor]bool{}
	for _, a := range r.order {
		if seen[a] {
			continue
		}
		if _, ok := r.slots[a]; !ok {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// Count 返回已安置的磁贴数。
func (r *Registry) Count() int { return len(r.slots) }

// CountIn 返回某个栏位的磁贴数（left / right / dock）。
//
// 布局需要这个数字来决定槽位是"长条"还是"上下各半"。
func (r *Registry) CountIn(column string) int {
	n := 0
	for a := range r.slots {
		if a.Column() == column {
			n++
		}
	}
	return n
}

// SetStates 按当前焦点刷新每个磁贴的状态。
//
// 状态决定"谁收按键、谁只渲染"，因此它由引擎统一算，磁贴不自己判断——
// v2.1.0 里"同一个动作写在两处就会按一下触发"的教训属于同一类问题。
func (r *Registry) SetStates(focused geometry.Anchor, stageBorrowed bool) {
	for a, s := range r.slots {
		switch {
		case stageBorrowed:
			// 有人借调了主控区：所有磁贴只渲染、不收键。
			s.State = plugin.StateOccupied
		case a == focused:
			s.State = plugin.StateFocused
		default:
			s.State = plugin.StateIdle
		}
		r.slots[a] = s
	}
}

// RenderAll 把所有磁贴画进各自的矩形。
//
// 边框与标题由这里统一画，磁贴只画内容区。这样"卡片框大小不定"
// （req.md 列出的痛点之一）就不可能出现：外框尺寸来自布局，不是磁贴自己算的。
//
// rectOf 由调用方按当前布局提供（"这个槽位现在的矩形是什么"）——
// 磁贴注册表不认识布局，布局也不认识磁贴，两者在这里汇合。
func (r *Registry) RenderAll(c *canvas.Canvas, rectOf func(geometry.Anchor) (geometry.Rect, bool), ctx plugin.RenderCtx, frame canvas.Frame) {
	for _, a := range r.Anchors() {
		s, ok := r.slots[a]
		if !ok {
			continue
		}
		outer, ok := rectOf(a)
		if !ok || outer.Empty() {
			continue
		}
		DrawTile(c, outer, &s, ctx, frame)
	}
}

// DrawTile 画一块磁贴：外框 + 标题 + 内容。
//
// 标题行占用框内第一行；内容从第二行开始。框太小的时候退化为"只画内容"，
// 宁可少一个标题也不留下画不出的边框。
func DrawTile(c *canvas.Canvas, outer geometry.Rect, s *Slot, ctx plugin.RenderCtx, frame canvas.Frame) {
	if outer.Empty() || s == nil || s.Component == nil {
		return
	}

	borderStyle, titleStyle := tileStyles(s.State, ctx)
	c.DrawBox(outer, frame, borderStyle)

	inner := outer.Inset(1, 1)
	if inner.Empty() {
		return
	}
	// 标题：有空间才画，且只占一行。
	contentTop := inner.Y
	if inner.H >= 2 {
		title := s.Component.Title()
		if title != "" {
			c.Text(inner.X, inner.Y, canvas.Truncate(title, inner.W), titleStyle)
		}
		contentTop = inner.Y + 1
	}
	content := geometry.NewRect(inner.X, contentTop, inner.W, inner.Y1()-contentTop)
	if content.Empty() {
		return
	}

	restore := c.PushClip(content)
	defer restore()
	ctx.Canvas = c
	ctx.Rect = content
	ctx.Frame = outer
	ctx.State = s.State
	ctx.Focus = s.State == plugin.StateFocused
	// 磁贴只有渲染能力，没有副作用入口——这一点由 RenderCtx 的类型保证。
	s.Component.Render(ctx)
}

// tileStyles 按状态给出边框与标题的样式下标。
//
// 这些下标是**约定**：主题包按同样的下标布置样式（0 默认、1 强调、2 次要、3 边框）。
// 集中在这里而不是各处硬编码数字，是为了改主题时只需要改一处。
func tileStyles(state plugin.ComponentState, ctx plugin.RenderCtx) (border, title canvas.StyleID) {
	switch state {
	case plugin.StateFocused:
		return StyleBorderFocused, StyleTitleFocused
	case plugin.StateOccupied:
		return StyleBorderDim, StyleTitleDim
	default:
		return StyleBorder, StyleTitle
	}
}

// 主题约定的样式下标。主题包必须按这些下标布置对应样式。
const (
	// StyleDefault 是不施加样式。
	StyleDefault canvas.StyleID = 0
	// StyleAccent 是强调（标题、选中）。
	StyleAccent canvas.StyleID = 1
	// StyleMuted 是次要文字。
	StyleMuted canvas.StyleID = 2
	// StyleBorder 是普通边框。
	StyleBorder canvas.StyleID = 3
	// StyleBorderFocused 是聚焦边框（焦点所在磁贴）。
	StyleBorderFocused canvas.StyleID = 4
	// StyleBorderDim 是被占用/变暗的边框。
	StyleBorderDim canvas.StyleID = 5
	// StyleTitle 是普通标题。
	StyleTitle canvas.StyleID = 6
	// StyleTitleFocused 是聚焦标题。
	StyleTitleFocused canvas.StyleID = 7
	// StyleTitleDim 是变暗标题。
	StyleTitleDim canvas.StyleID = 8
	// StyleStatus 是全局状态文本。
	StyleStatus canvas.StyleID = 9
	// StyleHintKey 是按键提示里的按键。
	StyleHintKey canvas.StyleID = 10
	// StyleHint 是按键提示里的说明。
	StyleHint canvas.StyleID = 11
	// StyleBarFilled 是进度条已填部分。
	StyleBarFilled canvas.StyleID = 12
	// StyleBarEmpty 是进度条未填部分。
	StyleBarEmpty canvas.StyleID = 13
	// StyleError 是错误文本。
	StyleError canvas.StyleID = 14
	// StyleWarn 是警告文本。
	StyleWarn canvas.StyleID = 15
	// StyleText 是正文文本。
	StyleText canvas.StyleID = 16
	// StyleCount 是样式表应有的长度（下标从 0 到 StyleCount-1）。
	//
	// 它让"主题少给了一个样式"变成可断言的事：两边下标错位是那种
	// "颜色不对但程序不报错"的问题，必须靠测试守住。
	StyleCount = 17
)
