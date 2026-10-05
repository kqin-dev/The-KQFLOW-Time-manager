// Package canvas 提供 KXFLOW 引擎的绘制基元：一块固定尺寸的字符-样式网格。
//
// 这是 v3.0.0 与 v2.1.0 在结构上最本质的差别。v2.1.0 的做法是"每个页面各自
// 渲染成字符串，再靠 clipBlock 兜底裁剪"，于是超宽 = 被截断 = 丢字，而丢字
// 恰恰是用户报过四次的症状。这里改成：**所有内容都画进画布，越界写不进去**。
//
// 四条结构性保证（对应 docs/kxflow-design.md §0.1 与 §3.1）：
//
//  1. **尺寸权威唯一**：画布尺寸就是权威，Render 的输出必然正好 H 行、每行 ≤ W 列。
//     不需要"渲染后实测宽度自校正"，也不需要 clipBlock。
//  2. **越界写不进去**：Set/Text 在写入前检查边界，写不进去就返回 false 并记账。
//  3. **跨栏串字不可能**：Clip 划定本次绘制的裁剪区；中栏渲染器拿不到左右栏的
//     矩形，它没有能力画过去。v2.1.0 靠的是"记得别那么画"。
//  4. **带样式的宽度不再是难题**：每个格子自带样式下标，Render 只对连续的同样式
//     片段上色。因此不需要 stripANSI 去反推宽度。
package canvas

import (
	"fmt"
	"strings"

	"github.com/kqin-dev/kxflow/geometry"
)

// StyleID 是样式表下标。0 表示"不施加样式"。
//
// 用下标而不是直接持有 lipgloss.Style，是为了让画布保持可比较、可测试，
// 也避免在热路径里反复构造样式对象（v2.1.0 的做法是每处 render 现用现渲染）。
type StyleID uint16

// DefaultStyle 是不施加任何样式的默认下标。
const DefaultStyle StyleID = 0

// MaxStyleID 是样式表容量的上限（含 DefaultStyle）。
const MaxStyleID = 1 << 16

// Cell 是画布上的一个格子。
type Cell struct {
	R rune
	// Cont 为真表示这是宽字符（东亚宽字）的右半格：渲染时跳过，但占一列宽度。
	Cont bool
	// StyleID 是该格的样式下标。
	StyleID StyleID
}

// blank 是空格子。
var blank = Cell{R: ' '}

// Diagnostics 汇总一次绘制过程中的异常。
//
// **开发期它必须全为 0**，测试直接断言这一点。把"画不下"变成可观测的数字，
// 而不是让终端悄悄折行或让代码悄悄截断，是这套设计最重要的工程价值：
// v2.1.0 的问题之所以修不完，正是因为"坏了"这件事没有出口。
type Diagnostics struct {
	// Overflow 是越界或超出裁剪区而被丢弃的写入次数。
	Overflow int
	// Collisions 是覆盖了非空格子的写入次数。
	//
	// 这一条专门抓 v2.1.0 最典型的事故：弹窗横跨三栏，把左右面板的边框切出断口，
	// 用户截图反馈"渲染坏了"。
	Collisions int
	// Clipped 是"内容被右侧截断"的次数（不折行时的正常截断也会计入）。
	Clipped int
	// Events 保留最近若干条细节，便于定位。
	Events []DiagEvent
}

// DiagEvent 是一条绘制诊断。
type DiagEvent struct {
	Kind DiagKind
	X, Y int
	Msg  string
}

// DiagKind 是诊断类别。
type DiagKind uint8

const (
	// DiagOverflow 表示写入越界或越过裁剪区。
	DiagOverflow DiagKind = iota
	// DiagCollision 表示覆盖了已有内容。
	DiagCollision
	// DiagClipped 表示内容被截断。
	DiagClipped
)

// diagEventLimit 限制保留的事件条数：诊断是给开发者的信号，不是日志系统，
// 不设上限的话一个渲染死循环能把内存吃光。
const diagEventLimit = 64

func (d *Diagnostics) record(kind DiagKind, x, y int, msg string) {
	switch kind {
	case DiagOverflow:
		d.Overflow++
	case DiagCollision:
		d.Collisions++
	case DiagClipped:
		d.Clipped++
	}
	if d == nil || len(d.Events) >= diagEventLimit {
		return
	}
	d.Events = append(d.Events, DiagEvent{Kind: kind, X: x, Y: y, Msg: msg})
}

// Clean 报告这次绘制没有任何异常。
func (d *Diagnostics) Clean() bool {
	return d == nil || (d.Overflow == 0 && d.Collisions == 0 && d.Clipped == 0)
}

// Reset 清空诊断，供画布复用。
func (d *Diagnostics) Reset() {
	if d == nil {
		return
	}
	d.Overflow, d.Collisions, d.Clipped = 0, 0, 0
	d.Events = d.Events[:0]
}

// Canvas 是一块固定尺寸的字符网格。
//
// 零值不可用，请用 New 构造。画布可以被复用（见 Reset）：动画帧每秒重绘数次，
// 每帧新建一块 160×60 的网格会白白制造垃圾。
type Canvas struct {
	W, H  int
	cells []Cell
	clip  geometry.Rect
	// Diag 记录异常。永不为 nil，调用方不必判空。
	Diag Diagnostics
	// styles 是样式表（下标 → 实际样式），由调用方通过 Render 传入。
}

// New 创建一块 w×h 的画布。负尺寸被夹紧为 0。
func New(w, h int) *Canvas {
	w, h = maxInt(w, 0), maxInt(h, 0)
	c := &Canvas{W: w, H: h, cells: make([]Cell, w*h)}
	c.clip = geometry.NewRect(0, 0, w, h)
	c.Clear()
	return c
}

// Size 返回画布尺寸。
func (c *Canvas) Size() geometry.Size { return geometry.Size{W: c.W, H: c.H} }

// Bounds 返回整块画布的矩形。
func (c *Canvas) Bounds() geometry.Rect { return geometry.NewRect(0, 0, c.W, c.H) }

// Clip 返回当前裁剪区。
func (c *Canvas) Clip() geometry.Rect { return c.clip }

// Clear 把整块画布恢复为空格，并清空诊断与裁剪区。
func (c *Canvas) Clear() {
	for i := range c.cells {
		c.cells[i] = blank
	}
	c.clip = c.Bounds()
	c.Diag.Reset()
}

// Reset 复用画布并切换到新尺寸。尺寸不变时不会重新分配。
func (c *Canvas) Reset(w, h int) {
	w, h = maxInt(w, 0), maxInt(h, 0)
	if w != c.W || h != c.H {
		c.W, c.H = w, h
		need := w * h
		if cap(c.cells) < need {
			c.cells = make([]Cell, need)
		} else {
			c.cells = c.cells[:need]
		}
	}
	c.Clear()
}

// PushClip 收窄裁剪区，返回恢复函数。必须成对使用，写法固定为：
//
//	restore := c.PushClip(inner)
//	... 绘制 ...
//	restore()
//
// 这是把"二级内容只占中栏"从约定变成机制的地方：中栏渲染器只拿到中栏的裁剪区。
func (c *Canvas) PushClip(r geometry.Rect) func() {
	prev := c.clip
	c.clip = r.Intersect(prev)
	// 裁剪区越出画布本体时，以画布为准——否则"越界"判断会出现两个标准。
	c.clip = c.clip.Intersect(c.Bounds())
	return func() { c.clip = prev }
}

// at 返回格子指针；越界返回 nil。
func (c *Canvas) at(x, y int) *Cell {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return nil
	}
	return &c.cells[y*c.W+x]
}

// CellAt 返回 (x,y) 处的格子副本；越界返回空格。
//
// 供测试与诊断使用（渲染路径不需要它）。
func (c *Canvas) CellAt(x, y int) Cell {
	if cell := c.at(x, y); cell != nil {
		return *cell
	}
	return blank
}

// inClip 报告 (x,y) 是否在裁剪区内。
func (c *Canvas) inClip(x, y int) bool { return c.clip.Contains(x, y) }

// Set 在第 (x,y) 格写入一个字符。
//
// 返回是否写入成功。以下情况返回 false 并计入诊断：坐标越界、超出裁剪区、
// 宽字符右侧放不下（**宁可整字不写，也不写半个汉字**）、控制字符。
func (c *Canvas) Set(x, y int, r rune, st StyleID) bool {
	if r == '\n' || r == '\t' || r == '\r' {
		r = ' '
	}
	if isControl(r) {
		c.Diag.record(DiagOverflow, x, y, "控制字符被拒绝")
		return false
	}
	w := RuneWidth(r)
	if w == 0 {
		// 组合记号等零宽字符：引擎不处理字形合成，直接忽略（计入诊断便于发现）。
		c.Diag.record(DiagOverflow, x, y, "零宽字符被忽略")
		return false
	}
	// 宽字符必须整体放得下：右半格也要在画布与裁剪区内。
	if x < 0 || y < 0 || x+w > c.W || y >= c.H || !c.inClip(x, y) || !c.inClip(x+w-1, y) {
		c.Diag.record(DiagOverflow, x, y, "写入越界或越过裁剪区")
		return false
	}
	if cell := c.at(x, y); cell != nil {
		if cell.R != ' ' && cell.R != r {
			c.Diag.record(DiagCollision, x, y, "覆盖已有内容 "+string(cell.R)+" → "+string(r))
		}
		// 覆盖宽字符的**右半格**时，把左半格一并抹掉（否则右边会留下孤立的半格）。
		if cell.Cont && x > 0 {
			if left := c.at(x-1, y); left != nil {
				if left.Cont {
					c.Diag.record(DiagCollision, x-1, y, "发现孤立的宽字符右半格标记")
				} else {
					left.R = ' '
					left.Cont = false
					left.StyleID = DefaultStyle
				}
			}
		}
		*cell = Cell{R: r, StyleID: st}
	}
	// 本格被改写后，(x+1) 上"原来属于本格"的右半格标记就失去主人了，必须清掉。
	// 清完再按新内容重新标记：新内容是宽字符时，下面的 w==2 分支会重新写上。
	if x+1 < c.W {
		if right := c.at(x+1, y); right != nil && right.Cont {
			right.Cont = false
			right.R = ' '
			right.StyleID = DefaultStyle
		}
	}
	if w == 2 {
		if cell := c.at(x+1, y); cell != nil {
			if cell.R != ' ' && !cell.Cont {
				c.Diag.record(DiagCollision, x+1, y, "宽字符右半格覆盖已有内容")
			}
			// 右半格必须**保留同一个字符**，只用 Cont 标记"输出时跳过"。
			// 曾经这里写成 Cell{Cont: true} 把字符丢掉，于是纯文本输出会多出一个
			// NUL：一行 7 个字符却报出 11 列宽。诊断数字因此一直在骗人——
			// 这正是 SKILL 里"量出来的数据在骗自己"那一类错误，必须避免。
			*cell = Cell{R: r, Cont: true, StyleID: st}
		}
	}
	c.normalizeRow(y)
	return true
}

// normalizeRow 修正第 y 行上的宽字符配对。
//
// 为什么需要它：一次 Set 只"拥有"它写入的那一格，而右半格的标记是**副作用**。
// 当两次写入落在相邻格子上时（例如先把整行填成 '─'，再从同一行中间写中文），
// 后一次写入无法知道前一格上那个标记是不是自己该管的，于是可能留下
// "两个连续的右半格标记"。那会让一行凭空少一个字符、宽度却按两列算。
//
// 与其在每次写入里穷举"谁该清谁"，不如在写入之后把这一行**修正到自洽**：
// 任何带右半格标记、但左邻并不是"同一个宽字符"的格子，一律退化成独立空格。
// 这样 CheckInvariants 成为一条恒真的不变量，而不是"但愿每次都记得清干净"。
func (c *Canvas) normalizeRow(y int) {
	if y < 0 || y >= c.H || c.W <= 0 {
		return
	}
	row := c.cells[y*c.W : (y+1)*c.W]
	for x := 1; x < len(row); x++ {
		cell := &row[x]
		if !cell.Cont {
			continue
		}
		left := &row[x-1]
		if left.Cont || left.R != cell.R || RuneWidth(left.R) != 2 {
			// 配对不成立：这格不是任何宽字符的右半格。
			*cell = blank
		}
	}
}

// Text 从 (x,y) 起写入一段文本，写到裁剪区右边界就停下。
//
// **不折行**：需要折行请先调用 Wrap 再逐行 Text。这是刻意的——v2.1.0 的教训是
// "超宽就靠 truncate 兜"，而截断等于丢字。分开之后，调用方要么显式折行，
// 要么显式接受截断（并通过返回的 clipped 知道发生了截断）。
//
// 返回实际写入的列数与是否发生截断。换行符会终止本次写入（不会换行继续写）。
func (c *Canvas) Text(x, y int, s string, st StyleID) (cols int, clipped bool) {
	col := x
	// 行内可用右边界：画布与裁剪区取小。
	limit := minInt(c.W, c.clip.X1())
	for _, r := range s {
		if r == '\n' {
			break
		}
		w := RuneWidth(r)
		if w == 0 {
			continue
		}
		if col+w > limit || !c.inClip(col, y) {
			clipped = true
			c.Diag.record(DiagClipped, col, y, "文本超出可用宽度")
			break
		}
		if !c.Set(col, y, r, st) {
			clipped = true
			break
		}
		col += w
	}
	return col - x, clipped
}

// Fill 用 r 填满矩形（与裁剪区求交）。返回实际填充的格子数。
func (c *Canvas) Fill(r geometry.Rect, ch rune, st StyleID) int {
	area := r.Intersect(c.clip)
	if area.Empty() {
		return 0
	}
	n := 0
	for y := area.Y; y < area.Y1(); y++ {
		for x := area.X; x < area.X1(); x++ {
			if c.Set(x, y, ch, st) {
				n++
			}
		}
	}
	return n
}

// ClearRect 把矩形区域恢复为空格。
func (c *Canvas) ClearRect(r geometry.Rect) int { return c.Fill(r, ' ', DefaultStyle) }

// DrawBox 用边框样式 f 画出正好占满 r 的边框。
//
// 边框**只画在 r 的四条边上**，内区留给内容。r 太小时退化为一条横线，
// 绝不 panic，也绝不画到 r 之外。
func (c *Canvas) DrawBox(r geometry.Rect, f Frame, st StyleID) {
	if r.Empty() {
		return
	}
	if r.W < 2 || r.H < 2 {
		c.Fill(r, f.Horizontal, st)
		return
	}
	x0, y0, x1, y1 := r.X, r.Y, r.X1()-1, r.Y1()-1
	c.Set(x0, y0, f.TopLeft, st)
	c.Set(x1, y0, f.TopRight, st)
	c.Set(x0, y1, f.BottomLeft, st)
	c.Set(x1, y1, f.BottomRight, st)
	for x := x0 + 1; x < x1; x++ {
		c.Set(x, y0, f.Horizontal, st)
		c.Set(x, y1, f.Horizontal, st)
	}
	for y := y0 + 1; y < y1; y++ {
		c.Set(x0, y, f.Vertical, st)
		c.Set(x1, y, f.Vertical, st)
	}
}

// Render 输出最终字符串。
//
// 保证：行数恰好等于 H（H>0 时），每行显示宽度不超过 W。
// 相邻的同一样式格子合并成一段上色，带样式的文本因此**不需要**再按显示宽度
// 反推——这是删掉 v2.1.0 里 stripANSI/displayRange 那一整套绕行代码的前提。
func (c *Canvas) Render(pal Palette) string {
	if c.W <= 0 || c.H <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow((c.W + 16) * c.H)
	for y := 0; y < c.H; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		c.renderRow(&b, y, pal)
	}
	return b.String()
}

// renderRow 渲染第 y 行，把连续同样式的格子合并成一次上色。
func (c *Canvas) renderRow(b *strings.Builder, y int, pal Palette) {
	row := c.cells[y*c.W : (y+1)*c.W]
	var seg strings.Builder
	segStyle := row[0].StyleID
	flush := func(st StyleID) {
		if seg.Len() == 0 {
			return
		}
		b.WriteString(pal.Render(st, seg.String()))
		seg.Reset()
	}
	for i := 0; i < c.W; i++ {
		cell := row[i]
		if cell.Cont {
			// 宽字符的右半格不输出字符，但它的样式与左半格一致，
			// 因此这里不需要打断当前分段。
			continue
		}
		if cell.StyleID != segStyle {
			flush(segStyle)
			segStyle = cell.StyleID
		}
		seg.WriteRune(cell.R)
	}
	flush(segStyle)
}

// CheckInvariants 校验画布内部不变量，返回第一个违规的描述（无违规则返回空串）。
//
// 不变量：宽字符必须成对出现——某格标了 Cont，它左边一格必须是同一个字符且自身
// 不标 Cont。这条不变量一旦破了，纯文本输出与显示宽度就会分家，
// 而**分家之后所有基于宽度的诊断都会开始骗人**（KQFLOW 历史上正是被这个坑过）。
// 因此把它做成可调用的自检，测试与开发期都能随时验证。
func (c *Canvas) CheckInvariants() string {
	if c.W <= 0 || c.H <= 0 {
		return ""
	}
	if len(c.cells) != c.W*c.H {
		return fmt.Sprintf("格子数 %d 与画布尺寸 %dx%d 不一致", len(c.cells), c.W, c.H)
	}
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			cell := c.cells[y*c.W+x]
			if cell.Cont {
				if x == 0 {
					return fmt.Sprintf("第 %d 行第 0 列带右半格标记，但左边没有格子", y)
				}
				left := c.cells[y*c.W+x-1]
				if left.Cont {
					return fmt.Sprintf("第 %d 行第 %d 列被重复标为右半格", y, x)
				}
				if left.R != cell.R {
					return fmt.Sprintf("第 %d 行第 %d 列的右半格(%s)与左邻(%s)不是同一个字",
						y, x, string(cell.R), string(left.R))
				}
				if RuneWidth(left.R) != 2 {
					return fmt.Sprintf("第 %d 行第 %d 列的左邻(%s)不是宽字符，配对不成立",
						y, x, string(left.R))
				}
				continue
			}
			if RuneWidth(cell.R) == 2 && x+1 >= c.W {
				return fmt.Sprintf("第 %d 行末尾的宽字符 %s 缺右半格", y, string(cell.R))
			}
		}
	}
	return ""
}

// RowString 返回第 y 行的纯文本（不含样式，行尾空格保留）。
//
// 与 String 一样只用于调试与诊断；渲染请用 Render。
// 它的显示宽度必须恰好等于画布宽度 W —— 这是画布自洽性的断言点。
func (c *Canvas) RowString(y int) string {
	if y < 0 || y >= c.H || c.W <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(c.W)
	for x := 0; x < c.W; x++ {
		cell := c.cells[y*c.W+x]
		if cell.Cont {
			continue
		}
		b.WriteRune(cell.R)
	}
	return b.String()
}

// String 返回不含样式的纯文本（调试用）。
func (c *Canvas) String() string {
	if c.W <= 0 || c.H <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow((c.W + 1) * c.H)
	for y := 0; y < c.H; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.RowString(y))
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
