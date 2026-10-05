// Package chrome 实现骨架的上下栏：上栏放面包屑与全局状态，下栏放按键提示与进度。
//
// 它们**不拥有状态**：调用方给出内容，这里只负责排版与绘制。
// 之所以强调这点：v2.1.0 的 renderHeader 会顺手读数据、算问候语、判断 toast，
// 于是"上栏显示什么"与"业务是什么"纠缠在一起，单测只能整块跑。
package chrome

import (
	"strings"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
)

// HeaderBar 是上栏：左侧标题/面包屑，右侧全局状态。
type HeaderBar struct {
	// Crumbs 是从左到右的面包屑（例如 "KQFLOW" → "设置"）。
	Crumbs []string
	// Status 是右对齐的全局状态（例如 "今日专注 2h13m"）。
	Status string
	// StatusStyle 是右侧状态的样式下标。
	StatusStyle canvas.StyleID
	// CrumbStyle 是面包屑的样式下标。
	CrumbStyle canvas.StyleID
	// Separator 是面包屑之间的分隔串，默认 " · "。
	Separator string

	// leftExtras / rightExtras 是各包通过 Decorate* 追加的内容。
	leftExtras  []string
	rightExtras []string
}

// AddLeft 在面包屑之前追加一段内容（内核或插件都可以用）。
func (h *HeaderBar) AddLeft(s string) {
	if s != "" {
		h.leftExtras = append(h.leftExtras, s)
	}
}

// AddRight 在状态之后追加一段内容。
func (h *HeaderBar) AddRight(s string) {
	if s != "" {
		h.rightExtras = append(h.rightExtras, s)
	}
}

// ResetExtras 清空追加内容（每帧重建时调用，避免越积越多）。
//
// 这一条是防"跨帧累积"的必要措施：上一版曾把"每帧往提示列表里加一项"
// 写成 append 到同一个切片，跑一会儿界面就被自己塞满了。
func (h *HeaderBar) ResetExtras() {
	h.leftExtras = h.leftExtras[:0]
	h.rightExtras = h.rightExtras[:0]
}

// LeftText 返回左半部分的纯文本（测试与宽度计算用）。
func (h *HeaderBar) LeftText() string {
	sep := h.Separator
	if sep == "" {
		sep = " · "
	}
	parts := make([]string, 0, len(h.leftExtras)+len(h.Crumbs))
	parts = append(parts, h.leftExtras...)
	parts = append(parts, h.Crumbs...)
	return strings.Join(parts, sep)
}

// RightText 返回右半部分的纯文本。
func (h *HeaderBar) RightText() string {
	parts := make([]string, 0, len(h.rightExtras)+1)
	if h.Status != "" {
		parts = append(parts, h.Status)
	}
	parts = append(parts, h.rightExtras...)
	return strings.Join(parts, "  ")
}

// Draw 把上栏画进 r（一行）。
//
// 空间不够时**先牺牲左侧**（面包屑可以缩短），右侧状态优先保留：
// 状态是用户最关心的信息，而"当前在哪个页面"通常也能从界面本身看出来。
// 这与 v2.1.0 renderHeader 的取舍一致（它也是先压缩问候语与日期）。
func (h *HeaderBar) Draw(c *canvas.Canvas, r geometry.Rect) {
	if r.Empty() || r.H < 1 {
		return
	}
	restore := c.PushClip(r)
	defer restore()

	left, right := h.LeftText(), h.RightText()
	leftW, rightW := canvas.StringWidth(left), canvas.StringWidth(right)

	// 右侧优先：它放不下时只截断它自己，不再去挤左侧。
	if rightW > r.W {
		right = canvas.TruncateEllipsis(right, r.W)
		rightW = canvas.StringWidth(right)
		left, leftW = "", 0
	} else if leftW+rightW+1 > r.W {
		// 左侧让位，留一列间隔。
		avail := r.W - rightW - 1
		if avail < 0 {
			avail = 0
		}
		left = canvas.TruncateEllipsis(left, avail)
		leftW = canvas.StringWidth(left)
	}

	if leftW > 0 {
		c.Text(r.X, r.Y, left, h.CrumbStyle)
	}
	if rightW > 0 {
		gap := r.W - leftW - rightW
		if gap < 1 {
			gap = 1
		}
		c.Text(r.X+leftW+gap, r.Y, right, h.StatusStyle)
	}
}

// FooterBar 是下栏：左侧进度/状态，右侧按键提示。
type FooterBar struct {
	// Text 是左对齐的状态文本（例如"今日已过 68%"）。
	Text string
	// TextStyle 是它的样式下标。
	TextStyle canvas.StyleID
	// Hints 是按键提示（从左到右）。
	Hints []KeymapHint
	// HintStyle / KeyStyle 分别是说明与按键的样式下标。
	HintStyle canvas.StyleID
	KeyStyle  canvas.StyleID
	// Progress 是可选的进度条（0~1）；当 HasProgress 为假时不画。
	Progress    float64
	HasProgress bool
	// BarFilled / BarEmpty 是进度条两侧的样式下标。
	BarFilled canvas.StyleID
	BarEmpty  canvas.StyleID
}

// KeymapHint 是一条按键提示。
//
// 它由**组件自己**提供（见 plugin.KeyHinter），而不是引擎写死一套：
// 光标停在不同东西上时能按的键本来就不一样——停在有待办的列表上是
// j/k + 勾选，停在计时磁贴上是暂停，停在空列表上则什么也做不了。
//
// 用户的原话：
//
//	如果光标在无动作时不会触发什么东西，那么下栏的操作提示就需要
//	跟着光标的操作提示改变，显然这种提示需要插件包提供。
type KeymapHint struct {
	Key  string // 如 "tab" / "space"
	Desc string // 如 "切换栏位"
}

// HintsText 返回提示的纯文本，形如 "tab:切换栏位  space:勾选"。
func (f *FooterBar) HintsText() string {
	parts := make([]string, 0, len(f.Hints))
	for _, h := range f.Hints {
		if h.Key == "" && h.Desc == "" {
			continue
		}
		parts = append(parts, h.Key+":"+h.Desc)
	}
	return strings.Join(parts, "  ")
}

// Draw 把下栏画进 r（一行）。
//
// 分配规则（三段互不重叠，从左到右依次定死）：
//
//	[ 状态文本 ][ 进度条 ][ 空隙 ][ 按键提示 ]
//
// 优先级从高到低是：**按键提示 > 进度条 > 状态文本**。
// 提示是"我现在能做什么"的唯一入口；进度条只在**放得下时**才占位；
// 状态文本最后拿剩下的空间（往往只剩几列，那就少显示几个字）。
//
// 这里踩过两个坑，都靠诊断抓出来（画布会记下越界写入的坐标与发起函数）：
//   - 先在固定位置画状态文本、再把进度条摆到"预算位置"，两者就会压在一起——
//     因为状态文本占的是 [X, X+leftAvail)，而进度条也想从 X 起画。
//     正确做法是**把进度条算进左侧预算里**，而不是让它与状态文本抢地盘。
//   - 提示文本的显示宽度要按**显示宽度**算：中文与全角标点各占两列，
//     一条 30 个字的提示实际要 54 列。按字数估算会让整个分配错位。
func (f *FooterBar) Draw(c *canvas.Canvas, r geometry.Rect) {
	if r.Empty() || r.H < 1 {
		return
	}
	restore := c.PushClip(r)
	defer restore()

	hints := f.HintsText()
	// 用 Truncate（不加省略号）而不是 TruncateEllipsis：
	// drawHints 是**逐段**重画的，省略号会多占一列，
	// 让"预量宽度"与"实际画出宽度"对不上。
	if canvas.StringWidth(hints) > r.W {
		hints = canvas.Truncate(hints, r.W)
	}
	hintsW := canvas.StringWidth(hints)
	hintsX := r.X + r.W - hintsW

	// 左侧预算：从行首到提示之前（至少留一列空隙）。
	const gap = 1
	leftBudget := hintsX - r.X - gap
	if leftBudget < 0 {
		leftBudget = 0
	}

	// 进度条先占位（它比状态文本重要）。放不下就不画——
	// 宁可少一个进度条，也不能让它压掉别的文字。
	barW := 0
	if f.HasProgress && leftBudget >= 8 {
		barW = 12
		if barW > r.W/3 {
			barW = r.W / 3
		}
		if barW > leftBudget-2 { // 至少给状态文本留一列
			barW = leftBudget - 2
		}
		if barW < 4 {
			barW = 0
		}
	}
	statusAvail := leftBudget - barW
	if barW > 0 {
		statusAvail-- // 状态文本与进度条之间留一列
	}
	if statusAvail < 0 {
		statusAvail = 0
	}

	if statusAvail > 0 && f.Text != "" {
		text := f.Text
		if canvas.StringWidth(text) > statusAvail {
			text = canvas.TruncateEllipsis(text, statusAvail)
		}
		c.Text(r.X, r.Y, text, f.TextStyle)
	}
	if barW > 0 {
		// 进度条紧贴左侧预算的末端，因此它右边必然还剩 gap 列空隙。
		f.drawProgress(c, r.X+leftBudget-barW, r.Y, barW)
	}
	if hints != "" {
		f.drawHints(c, hintsX, r.Y, hintsW)
	}
}

// drawProgress 画一条宽度固定的进度条。
//
// 起画点必须**夹进行区域**：只用宽度判断"放得下放不下"是不够的，
// 左边界可能已经越过行首（barW 仍然 ≥ 4，但 x < r.X）。
// 实测症状是"越界写入发生在 x=-1..-5，而画布上什么也看不见"——
// 幸好画布把发起写入的函数名记在了诊断里，否则这种问题只能靠猜。
func (f *FooterBar) drawProgress(c *canvas.Canvas, x, y, w int) {
	area := c.Clip()
	if x < area.X {
		w -= area.X - x
		x = area.X
	}
	if x+w > area.X1() {
		w = area.X1() - x
	}
	if w <= 0 {
		return
	}
	filled := int(float64(w) * clamp01(f.Progress))
	for i := 0; i < w; i++ {
		if i < filled {
			c.Set(x+i, y, '█', f.BarFilled)
			continue
		}
		c.Set(x+i, y, '░', f.BarEmpty)
	}
}

// drawHints 把按键提示从 x 起画出（按键与说明用不同样式）。
//
// width 是调用方预算好的可用宽度，本函数**绝不画到 x+width 之外**。
//
// 这个上界不能省：hints 是全量文本的拷贝，而这里是逐段重画的，
// 一旦提示本身被裁过（hints 只放下前几段），循环若还按 f.Hints 全跑，
// 画出的总宽就会超过预算——表现为文字被挤出边界。
// 教训：**"预量宽度"与"逐段重画"必须共用同一个上界**。
func (f *FooterBar) drawHints(c *canvas.Canvas, x, y, width int) {
	if width <= 0 {
		return
	}
	end := x + width
	cur := x
	for i, h := range f.Hints {
		if h.Key == "" && h.Desc == "" {
			continue
		}
		if cur >= end {
			return
		}
		if i > 0 {
			n, _ := c.Text(cur, y, "  ", f.HintStyle)
			cur += n
		}
		if cur >= end {
			return
		}
		// 按键名**永远完整显示**：它是用户唯一能"照着按"的信息。
		// 曾经这里对 Key+":" 也做 Truncate，于是窄终端下会出现
		// "ta"、"en" 这种没有意义的片段。
		if w := canvas.StringWidth(h.Key + ":"); w > end-cur {
			return // 连按键都放不下了，后面的更放不下
		}
		n, _ := c.Text(cur, y, h.Key+":", f.KeyStyle)
		cur += n
		if cur >= end {
			return
		}
		// 说明文字允许省略，但**必须带省略号**：
		// 半个词看起来像渲染坏了，带省略号才是"这里还有内容被省略"。
		rest := end - cur
		desc := h.Desc
		if canvas.StringWidth(desc) > rest {
			desc = canvas.TruncateEllipsis(desc, rest)
		}
		n, _ = c.Text(cur, y, desc, f.HintStyle)
		cur += n
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
