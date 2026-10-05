// Package geometry 提供 KXFLOW 引擎的二维几何基元。
//
// 这一层刻意**不依赖任何东西**（连 lipgloss 都不依赖）：它是布局与画布的共同底座，
// 保持纯净才能在测试里穷举尺寸边界。
//
// 设计要点（见 docs/kxflow-design.md §3.1）：
//
//   - 分割方法名自带方向（CutTop / CutBottom / CutLeft / CutRight），
//     不用 SplitH/SplitV 这类"A 到底是上半还是下半"含糊的命名。
//     原型阶段真的因此踩过一次：底栏被撑成 22 行、主体只剩 1 行。
//   - **所有宽度/高度不变量是类型级承诺**：Rect 永远不出现负的 W/H，
//     越界输入一律夹紧。调用方不必层层判断负数。
package geometry

// Size 是终端或区域的尺寸（列 × 行）。
type Size struct {
	W, H int
}

// Point 是一个显示列坐标。
//
// X 是**显示列**（东亚宽字符占两列），Y 是行。全项目的坐标都按显示列算，
// 绝不按 rune 下标或字节下标——这是 KQFLOW 历史上出过三次事故的地方。
type Point struct {
	X, Y int
}

// NewSize 返回一个非负尺寸。
func NewSize(w, h int) Size {
	return Size{W: max(w, 0), H: max(h, 0)}
}

// Empty 报告尺寸是否为空（无法容纳任何内容）。
func (s Size) Empty() bool { return s.W <= 0 || s.H <= 0 }

// Rect 是一个矩形区域，坐标 (X, Y) 是左上角，W/H 是列数与行数。
//
// 不变量：W >= 0 且 H >= 0。所有构造与变换都必须维持它。
type Rect struct {
	X, Y, W, H int
}

// NewRect 构造一个矩形，负的宽高被夹紧为 0。
func NewRect(x, y, w, h int) Rect {
	return Rect{X: x, Y: y, W: max(w, 0), H: max(h, 0)}
}

// FromSize 返回一个从原点开始、大小为 s 的矩形。
func FromSize(s Size) Rect {
	return Rect{W: s.W, H: s.H}
}

// Empty 报告矩形是否没有面积。
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Size 返回矩形的尺寸。
func (r Rect) Size() Size { return Size{W: r.W, H: r.H} }

// Pos 返回矩形的左上角坐标。
func (r Rect) Pos() Point { return Point{X: r.X, Y: r.Y} }

// X1 返回右边界（不含）。空矩形时等于 X。
func (r Rect) X1() int { return r.X + r.W }

// Y1 返回下边界（不含）。空矩形时等于 Y。
func (r Rect) Y1() int { return r.Y + r.H }

// Right 是 X1 的别称，读起来更贴近"右边界在哪一列"。
func (r Rect) Right() int { return r.X1() }

// Bottom 是 Y1 的别称。
func (r Rect) Bottom() int { return r.Y1() }

// Contains 报告点 (x, y) 是否在矩形内（左闭右开）。
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X1() && y >= r.Y && y < r.Y1()
}

// ContainsPoint 是 Contains 的 Point 版本。
func (r Rect) ContainsPoint(p Point) bool { return r.Contains(p.X, p.Y) }

// ContainsRect 报告 o 是否完全落在 r 内（空矩形视为"在"）。
func (r Rect) ContainsRect(o Rect) bool {
	if o.Empty() {
		return true
	}
	if r.Empty() {
		return false
	}
	return o.X >= r.X && o.Y >= r.Y && o.X1() <= r.X1() && o.Y1() <= r.Y1()
}

// Intersect 返回两者的交集；不相交时返回空矩形。
//
// 结果可能是"位置合法但面积为 0"的矩形（例如 X 落在某个合法列上），
// 这也是为什么所有绘制入口都必须先看 Empty()。
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X1(), o.X1()), min(r.Y1(), o.Y1())
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Union 返回能包住两者的最小矩形。
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	x0, y0 := min(r.X, o.X), min(r.Y, o.Y)
	x1, y1 := max(r.X1(), o.X1()), max(r.Y1(), o.Y1())
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Translate 平移矩形。
func (r Rect) Translate(dx, dy int) Rect {
	return Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}
}

// Inset 向内部收缩：内容区在去掉边框与内边距后的矩形。
//
// 收缩量大于尺寸时结果为空矩形，不会出现负宽高。
func (r Rect) Inset(dx, dy int) Rect {
	dx, dy = max(dx, 0), max(dy, 0)
	return Rect{
		X: r.X + dx,
		Y: r.Y + dy,
		W: max(r.W-2*dx, 0),
		H: max(r.H-2*dy, 0),
	}
}

// CutTop 从顶部切下 h 行，返回（顶部, 剩余）。
//
// 这是"底栏/上栏"的标准写法：`header, rest := full.CutTop(1)`。
func (r Rect) CutTop(h int) (top, rest Rect) {
	h = clamp(h, 0, r.H)
	return Rect{X: r.X, Y: r.Y, W: r.W, H: h},
		Rect{X: r.X, Y: r.Y + h, W: r.W, H: r.H - h}
}

// CutBottom 从底部切下 h 行，返回（剩余, 底部）。
//
// 注意返回值顺序与 CutTop 一致：**先剩下的，后被切走的**语义由名字给出，
// 但两行写在一起时请看名字而不要靠记忆：
// `body, footer := full.CutBottom(1)`。
func (r Rect) CutBottom(h int) (rest, bottom Rect) {
	h = clamp(h, 0, r.H)
	return Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - h},
		Rect{X: r.X, Y: r.Y + r.H - h, W: r.W, H: h}
}

// CutLeft 从左侧切下 w 列，返回（左侧, 剩余）。
func (r Rect) CutLeft(w int) (left, rest Rect) {
	w = clamp(w, 0, r.W)
	return Rect{X: r.X, Y: r.Y, W: w, H: r.H},
		Rect{X: r.X + w, Y: r.Y, W: r.W - w, H: r.H}
}

// CutRight 从右侧切下 w 列，返回（剩余, 右侧）。
func (r Rect) CutRight(w int) (rest, right Rect) {
	w = clamp(w, 0, r.W)
	return Rect{X: r.X, Y: r.Y, W: r.W - w, H: r.H},
		Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}
}

// SplitVertical 把矩形按「左栏 / 中栏 / 右栏」三段切开。
//
// 这是三栏骨架的专用入口：避免手写两次 CutLeft 时把剩余宽度算错
// （v2.1.0 的三栏宽度就是靠一条 `a.width - leftW - rightW` 维持的）。
//
// 宽度总和不足时，从右往左依次让位，任何一段都不会变成负数。
func (r Rect) SplitVertical(leftW, rightW int) (left, center, right Rect) {
	leftW, rightW = max(leftW, 0), max(rightW, 0)
	if leftW+rightW > r.W {
		// 先保左栏，再按剩余给右栏；中栏可以为 0（极小终端时的诚实结果）。
		if leftW > r.W {
			leftW = r.W
		}
		rightW = max(r.W-leftW, 0)
	}
	centerW := r.W - leftW - rightW
	left = Rect{X: r.X, Y: r.Y, W: leftW, H: r.H}
	center = Rect{X: r.X + leftW, Y: r.Y, W: centerW, H: r.H}
	right = Rect{X: r.X + leftW + centerW, Y: r.Y, W: rightW, H: r.H}
	return left, center, right
}

// Row 返回第 y 行的整条横向矩形（用于按行裁剪时的调试与测试）。
func (r Rect) Row(y int) Rect {
	if y < r.Y || y >= r.Y1() {
		return Rect{X: r.X, Y: y, W: 0, H: 0}
	}
	return Rect{X: r.X, Y: y, W: r.W, H: 1}
}

// String 便于测试失败信息可读，形如 "(0,1 80x22)"。
func (r Rect) String() string {
	return "(" + itoa(r.X) + "," + itoa(r.Y) + " " + itoa(r.W) + "x" + itoa(r.H) + ")"
}

// ---------- 内部小工具 ----------

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// itoa 与 strconv.Itoa 等价，但不引入 strconv（几何层刻意保持零外部依赖，
// 连标准库的格式化包都不牵进来，保证这一层可以被任何宿主复用）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	if v < 0 {
		return "-" + itoa(-v)
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
