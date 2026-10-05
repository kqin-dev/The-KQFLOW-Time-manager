// Package stage 实现中栏主控区与它的视图栈——本设计的核心交互机制。
//
// req.md 提出的直觉是：大多数功能都分成"磁贴"和"选项"两类，而它们需要调用
// 多级菜单时，就**向中栏借调界面**。这一层就是那个借调机制的实现。
//
// 它替换掉的是 v2.1.0 里 renderView() 那条 11 分支的优先级 if 链
// （stopAsk → ntfyHelp → pick → editor → custom → view 枚举）：
// 那条链子本质上就是"没有视图栈时的替代品"，而它有两个固有毛病——
// 优先级只能靠写在前后来表达，且每加一种二级内容就要再插一层判断。
//
// 现在：**栈的顺序就是优先级**，每个视图自带渲染与按键处理。
package stage

import (
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// Stage 是中栏主控区，它是一个视图栈。
//
// 不变量：
//   - 栈空时显示 base（默认底层视图，也就是 req.md 说的 Dashboard）；
//   - 栈非空时只有**栈顶**接收事件与渲染，其余层不参与；
//   - 每一层都记得自己"是谁借调的"（Origin），出栈时据此交还焦点。
type Stage struct {
	base  plugin.View
	stack []frame
}

// frame 是栈里的一层：视图 + 它的来源。
type frame struct {
	view   plugin.View
	origin plugin.Origin
}

// New 创建舞台，base 是栈空时显示的底层视图（可以为 nil，表示中栏留白）。
func New(base plugin.View) *Stage {
	return &Stage{base: base}
}

// SetBase 替换底层视图。若当前栈为空，界面会立刻切换到底层视图。
func (s *Stage) SetBase(v plugin.View) {
	s.base = v
	if s.base != nil {
		// 底层视图也需要一次 OnEnter 语义（它一直是"在场"的那一层）。
		s.base.OnEnter(plugin.Origin{Kind: plugin.OriginKernel})
	}
}

// Base 返回底层视图。
func (s *Stage) Base() plugin.View { return s.base }

// Push 借调一层视图（req.md 的 stage.push_view）。
//
// origin 说明是谁借调的；调用方（内核）负责补全它。
func (s *Stage) Push(v plugin.View, origin plugin.Origin) {
	if v == nil {
		return
	}
	s.stack = append(s.stack, frame{view: v, origin: origin})
	v.OnEnter(origin)
}

// Pop 弹出栈顶（req.md 的 stage.pop_view），返回被弹出的来源。
//
// 返回的 ok 为假表示栈本来就是空的——此时**不动 base**，
// 这样"多按了一次 esc"不会把界面关掉，与 v2.1.0 "esc 逐级退出"的手感一致。
func (s *Stage) Pop() (plugin.Origin, bool) {
	if len(s.stack) == 0 {
		return plugin.Origin{}, false
	}
	top := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	top.view.OnExit()
	return top.origin, true
}

// PopToBottom 一路退回底层（用于"退出到看板"这类动作）。
//
// 返回被弹出的全部来源，按出栈顺序；调用方可用最后一个来决定焦点回到哪。
func (s *Stage) PopToBottom() []plugin.Origin {
	var origins []plugin.Origin
	for len(s.stack) > 0 {
		o, _ := s.Pop()
		origins = append(origins, o)
	}
	return origins
}

// Replace 替换栈顶（向导的"下一步"用；不触发 OnEnter/OnExit 的语义差异
// 由调用方自己决定——这里只换内容，保持层位置不变）。
func (s *Stage) Replace(v plugin.View) {
	if v == nil || len(s.stack) == 0 {
		return
	}
	top := s.stack[len(s.stack)-1]
	top.view.OnExit()
	s.stack[len(s.stack)-1] = frame{view: v, origin: top.origin}
	v.OnEnter(top.origin)
}

// Top 返回当前接收事件与渲染的那一层：栈非空是栈顶，否则是 base。
func (s *Stage) Top() plugin.View {
	if len(s.stack) > 0 {
		return s.stack[len(s.stack)-1].view
	}
	return s.base
}

// TopOrigin 返回栈顶的来源；栈空返回零值。
func (s *Stage) TopOrigin() plugin.Origin {
	if len(s.stack) == 0 {
		return plugin.Origin{}
	}
	return s.stack[len(s.stack)-1].origin
}

// Depth 返回栈深（0 表示正在显示底层视图）。
//
// 磁贴需要它来判断"现在有没有别人在借调舞台"——有的话自己只渲染不收键。
func (s *Stage) Depth() int { return len(s.stack) }

// Borrowing 报告当前是否有视图正在借调主控区。
func (s *Stage) Borrowing() bool { return len(s.stack) > 0 }

// OccupiedBy 报告栈顶是不是由指定磁贴借调的。
func (s *Stage) OccupiedBy(tileID string) bool {
	if len(s.stack) == 0 || tileID == "" {
		return false
	}
	return s.stack[len(s.stack)-1].origin.TileID == tileID
}

// Render 渲染当前应该显示的那一层。
//
// 注意它**只渲染栈顶**：不做"叠在上面的浮层"。
// 这正是 v2.1.0 那条约定的机制化——"二级内容一律只占中间栏"，
// 而且不需要任何调用方自觉遵守：舞台只拿得到中栏的矩形。
func (s *Stage) Render(c *canvas.Canvas, rect geometry.Rect, ctx plugin.RenderCtx) {
	if s.Top() == nil || rect.Empty() {
		return
	}
	restore := c.PushClip(rect)
	defer restore()
	ctx.Canvas = c
	ctx.Rect = rect
	ctx.Frame = rect
	s.Top().Render(ctx)
}

// Clear 清空栈（不触发 OnExit —— 用于引擎销毁时避免在已死的视图上回调）。
func (s *Stage) Clear() { s.stack = nil }
