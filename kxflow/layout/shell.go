// Package layout 是 KXFLOW 的布局权威：所有栏宽、槽位与行数在这里算一次。
//
// 为什么要有这么一层（design §0）：v2.1.0 的宽度有权威（columnLayout）而
// **高度没有**——renderCenterBox 用它的 bodyH，viewTooSmall 却另算一套。
// 两个来源并存，于是"面板多一行/少一行"这类现象只能靠逐个兜。
// 这里的约定是：**任何尺寸都只能从 Shell 拿**，不存在第二处计算。
//
// Layout 是纯函数：给定终端尺寸与配置，结果完全确定、可复现、可穷举测试。
// 它不依赖画布，也不认识业务——只认"这一栏放几个磁贴"。
package layout

import "github.com/kqin-dev/kxflow/geometry"

// shell.go 定义五层骨架里的"布局骨架"这一层：
//
//	+---------------------------------------------------+
//	| Header（上栏）                                     |
//	+---------+-------------------------+---------------+
//	| Left    | Center                  | Right         |
//	| Slot0   | +---------------------+ | Slot0         |
//	| Slot1   | | Stage（主控区/舞台） | | Slot1         |
//	|         | +---------------------+ |               |
//	|         | | Dock（停靠区 0~2）   | |               |
//	+---------+-------------------------+---------------+
//	| Footer（下栏）                                     |
//	+---------------------------------------------------+
//
// 名字与职责取自 req.md 与 KXflow.md 的词汇表，保持一致以免两处说法打架。

// Shell 是一次布局的完整结果。
type Shell struct {
	// Bounds 是整个终端区域（与请求的尺寸一致）。
	Bounds geometry.Rect
	// Header 是上栏（面包屑 / 状态 / 提示）。
	Header geometry.Rect
	// Body 是三栏所在的中间区域（Header 与 Footer 之间）。
	Body geometry.Rect
	// Footer 是下栏（进度条 / 按键提示）。
	Footer geometry.Rect

	// Left / Right 是左右侧栏，各含两个槽位。
	Left Column
	// Center 是中栏：主舞台 + 可选停靠区。
	Center Lane
	// Right 是右侧栏。
	Right Column

	// Primary 是"单栏布局时应当使用的那个矩形"。
	//
	// 它存在的理由是一个真实的取舍：帮助 / 设置 / 历史这类整页内容
	// 在宽终端下只占中栏（否则一行太长、读起来累），
	// 而在窄终端下如果坚持只占中栏，中间那点宽度会把中文折得七零八落。
	// 所以**由布局层告诉调用方该用哪一块**，调用方不要自己判断宽度。
	Primary geometry.Rect
}

// Column 是左右侧栏：一个矩形 + 最多两个槽位。
type Column struct {
	Rect geometry.Rect
	// Slots 是两个物理槽位（上、下）。装载 1 个磁贴时两者相同（长条）。
	Slots [2]geometry.Rect
	// Count 是调用方声明的磁贴数量（0、1 或 ≥2）。
	Count int
}

// Lane 是中栏：主舞台与可选停靠区。
type Lane struct {
	Rect geometry.Rect
	// Stage 是主控区（视图栈所在）。它**总是存在**。
	Stage geometry.Rect
	// Dock 是中栏下方停靠区；Visible 为假时它是空矩形。
	Dock        geometry.Rect
	DockVisible bool
	// DockSlots 是停靠区的两个槽位。
	DockSlots [2]geometry.Rect
	// DockCount 是停靠区声明的磁贴数量。
	DockCount int
}

// Slot 返回第 i 个槽位；i 越界或该栏只有一个磁贴时返回整栏。
//
// 这一条就是 req.md 说的"单个磁贴时渲染为长条，两个磁贴时各占一半"。
func (c Column) Slot(i int) geometry.Rect {
	if c.Count < 2 {
		return c.Rect
	}
	if i != 0 && i != 1 {
		return c.Rect
	}
	return c.Slots[i]
}

// Slot 返回停靠区第 i 个槽位；越界返回空矩形（停靠区可以没有磁贴）。
func (l Lane) Slot(i int) geometry.Rect {
	if i != 0 && i != 1 {
		return geometry.Rect{X: l.Dock.X, Y: l.Dock.Y}
	}
	return l.DockSlots[i]
}

// Layout 计算整个骨架。**全项目唯一的尺寸权威。**
//
// 保证（由 layout_test.go 在尺寸网格上穷举）：
//   - 任何返回的矩形宽高都不为负；
//   - Header / Body / Footer 首尾相接且正好铺满 Bounds；
//   - Left / Center / Right 等高等宽拼满 Body，不重不漏；
//   - 即使终端只有 1×1，也返回合法矩形（不 panic、不出现负数）。
func Layout(size geometry.Size, cfg LayoutConfig) Shell {
	// 先夹紧尺寸：几何层承诺"不变量恒真"，而负尺寸可能来自
	// geometry.Size 字面量（不经 NewSize），因此这里必须自己夹一次，
	// 否则负宽高会一路传到画布。
	bounds := geometry.FromSize(geometry.NewSize(size.W, size.H))
	cfg = cfg.withDefaults()

	header, rest := bounds.CutTop(cfg.HeaderRows)
	body, footer := rest.CutBottom(cfg.FooterRows)

	leftW, _, rightW := cfg.SideWidths.resolve(bounds.W, cfg.MinCenterWidth)
	left, center, right := body.SplitVertical(leftW, rightW)

	sh := Shell{
		Bounds: bounds, Header: header, Body: body, Footer: footer,
		Left:  newColumn(left, cfg.LeftTiles),
		Right: newColumn(right, cfg.RightTiles),
	}
	sh.Center = newLane(center, cfg.DockVisible, cfg.DockTiles)
	sh.Primary = pickPrimary(bounds.W, left, center, right, cfg)
	return sh
}

// newColumn 把栏位矩形分给若干个槽位。
func newColumn(r geometry.Rect, count int) Column {
	c := Column{Rect: r, Count: count}
	if count < 2 {
		// 单磁贴 = 长条；0 个磁贴时槽位与整栏相同（调用方不会去画它）。
		c.Slots = [2]geometry.Rect{r, r}
		return c
	}
	c.Slots = splitEven(r)
	return c
}

// MinTileW / MinTileH 是一个磁贴能画出来的最小尺寸（含边框与标题行）。
//
// 小于它就别画了：一个 2 行高的框连标题都放不下，画出来只是噪声。
// 这个阈值是**布置槽位的依据**，不是"建议"——见 splitEven 的说明。
const (
	MinTileW = 6
	MinTileH = 3
)

// splitEven 把矩形上下均分给两个槽位；**装不下时就判空，绝不让它们重叠**。
//
// 这一条是踩出来的：早先版本在矩形太矮时让两个槽位都指向整块
// （注释里写的是"不会出现负数或重叠"），结果**恰恰就是重叠**——
// 两块磁贴画在同一片格子上，互相覆盖彼此的边框。
// 实测症状：40×10 的终端下画布报出 62 次"覆盖已有内容"。
//
// 正确的取舍是：每个磁贴至少要 MinTileH 行；分不出来就判空，
// 由调用方跳过渲染（少画一块磁贴，远好过画出两块互相盖住的）。
func splitEven(r geometry.Rect) [2]geometry.Rect {
	var empty [2]geometry.Rect
	if r.H < 2*MinTileH || r.W < MinTileW {
		// 两块的份都不够：**一个都不给**。
		//
		// 为什么不"给第一块、空第二块"：那样第一块会占满整栏，
		// 看起来像"用户只配了一块磁贴"，而真相是"这个尺寸下摆不开两块"——
		// 两种状态在界面上无法区分。判空则两块都不画，语义是明确的。
		return empty
	}
	topH := (r.H + 1) / 2
	top, bottom := r.CutTop(topH)
	if top.H < MinTileH || bottom.H < MinTileH {
		return empty
	}
	return [2]geometry.Rect{top, bottom}
}

// newLane 把中栏矩形分成主控区与（可选的）停靠区。
//
// 停靠区规则（按用户 2026-10-05 的实机反馈确定）：
//
//	0 个磁贴 → 不开停靠区，整个中栏都给看板；
//	1 个磁贴 → 占**下半中栏的整条**（长条，适合需要大空间的磁贴）；
//	2 个磁贴 → **左右各半**（下左 / 下右），而不是上下叠着放。
//
// 这样"中栏下半就是磁贴区"这件事一眼可见，高度也不再是
// "按需算出来的一个数"（原来是 dockCount*3+2，实测只有 5 行，
// 用户反馈"很小"）。现在固定取中栏一半，与上面对称。
func newLane(r geometry.Rect, dockVisible bool, dockCount int) Lane {
	l := Lane{Rect: r, DockVisible: dockVisible, DockCount: dockCount}
	if !dockVisible || dockCount == 0 {
		// 停靠区不可见：主控区独占整个中栏。
		// 这样调用方永远只需要关心 Stage，不必判断"到底有没有停靠区"。
		l.Stage = r
		l.Dock = geometry.Rect{X: r.X, Y: r.Y + r.H}
		l.DockVisible = false
		return l
	}
	// 停靠区取下半中栏。中栏太矮时不硬开：硬开只会把看板挤没，
	// 而一块 2 行高的磁贴也画不出内容。
	want := r.H / 2
	if want < MinTileH || r.W < MinTileW {
		l.Stage = r
		l.Dock = geometry.Rect{X: r.X, Y: r.Y + r.H}
		l.DockVisible = false
		return l
	}
	stage, dock := r.CutBottom(want)
	if stage.H < MinTileH {
		l.Stage = r
		l.Dock = geometry.Rect{X: r.X, Y: r.Y + r.H}
		l.DockVisible = false
		return l
	}
	l.Stage = stage
	l.Dock = dock
	l.DockSlots = splitEvenH(dock)
	return l
}

// splitEvenH 把矩形**左右**均分给两个槽位（停靠区用）。
//
// 与 splitEven（上下均分）分开是因为两者的语义不同：
// 侧栏是"上下两格"，停靠区是"下左 / 下右"。装不下时同样判空，理由见 splitEven。
func splitEvenH(r geometry.Rect) [2]geometry.Rect {
	var empty [2]geometry.Rect
	if r.W < 2*MinTileW || r.H < MinTileH {
		return empty
	}
	leftW := (r.W + 1) / 2
	left, right := r.CutLeft(leftW)
	if left.W < MinTileW || right.W < MinTileW {
		return empty
	}
	return [2]geometry.Rect{left, right}
}

// pickPrimary 选出"整页内容应当使用的那一块"。
//
// 规则（design §3.2）：宽终端下用中栏（一行不至于太长）；
// 窄到中栏装不下最小正文宽度时改用整行（左右栏让位），
// 免得把中文折得七零八落——v2.1.0 的说明页就在窄终端上吃这个亏。
func pickPrimary(totalW int, left, center, right geometry.Rect, cfg LayoutConfig) geometry.Rect {
	if center.W >= cfg.MinContentWidth {
		return center
	}
	full := left.Union(center).Union(right)
	// 整行也放不下时，仍然选"更宽的那一个"，让调用方拿到尽可能好的矩形。
	if full.W > center.W {
		return full
	}
	return center
}

// resolve 把期望的左右栏宽度解析成一组**保证合法**的实际宽度。
//
// 这是 v2.1.0 columnLayout 的等价实现（断点值与兜底阈值沿用它的实测值，
// 那些值已被 60~160 列的测试覆盖过，不擅自改）。规则：
//
//  1. 按宽度断点取一对候选值；
//  2. 中栏不足 MinCenterWidth 时从左右两侧各借一半；
//  3. 左右栏分别不低于 MinSideWidth 与右侧下限；
//  4. 最后兜底：总宽实在太窄时从右往左让位，中栏可降到 0，
//     但**任何一段都不出现负数**。
func (s SideWidths) resolve(totalW, minCenter int) (leftW, centerW, rightW int) {
	leftW, rightW = s.pick(totalW)
	if leftW+rightW > totalW {
		// 断点值本身就把宽度吃光了（终端极窄）：先按比例压到放得下。
		rightW = max(0, min(rightW, totalW/3))
		leftW = max(0, min(leftW, totalW-rightW))
	}

	if centerW = totalW - leftW - rightW; centerW < minCenter {
		shrink := minCenter - centerW
		takeLeft := shrink / 2
		takeRight := shrink - takeLeft

		// 左栏保底。
		if leftW-takeLeft < s.MinLeft {
			takeLeft = max(0, leftW-s.MinLeft)
			takeRight = shrink - takeLeft
		}
		// 右栏保底（用剩下的量再算一次）。
		if rightW-takeRight < s.MinRight {
			takeRight = max(0, rightW-s.MinRight)
			takeLeft = shrink - takeRight
		}
		// 两侧都到保底还不够：从右往左继续让，中栏优先。
		if takeLeft > leftW {
			takeLeft = leftW
		}
		if takeRight > rightW {
			takeRight = rightW
		}
		leftW -= takeLeft
		rightW -= takeRight
		centerW = totalW - leftW - rightW
	}

	// 最终兜底：任何一段都不得为负，且三者之和不超过总宽。
	if leftW < 0 {
		leftW = 0
	}
	if rightW < 0 {
		rightW = 0
	}
	if centerW < 0 {
		centerW = 0
	}
	if leftW+rightW > totalW {
		rightW = max(0, totalW-leftW)
	}
	centerW = totalW - leftW - rightW
	if centerW < 0 {
		centerW = max(0, totalW-rightW)
		leftW = totalW - rightW - centerW
	}
	return leftW, centerW, rightW
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
