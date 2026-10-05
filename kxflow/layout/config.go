package layout

// config.go 定义布局的输入。所有"魔法数字"集中在这里，且都带出处说明——
// 布局是那种"数字散落各处就再也说不清为什么是这个值"的地方。

// SideWidths 是左右侧栏的宽度策略。
//
// 断点与阈值全部沿用 v2.1.0 columnLayout 的实测值：那些值已被 60~160 列的
// 界面测试覆盖过，且用户对当前观感没有意见，因此**不擅自改**。
// （design §3.2：这是"把 v2.1.0 隐式做对的东西写成显式约束"。）
type SideWidths struct {
	// Wide 是宽终端（≥ WideMin）下的 (左, 右) 宽度。
	WideLeft, WideRight int
	// Medium / Narrow 是更窄两档的宽度。
	MediumLeft, MediumRight int
	NarrowLeft, NarrowRight int
	// 三档的分界（列数）。
	WideMin, MediumMin int

	// MinLeft / MinRight 是收缩时的保底宽度。
	//
	// 为什么要有保底：中栏宽度不足时要从两侧借，但借光了侧栏就没法显示
	// TODO/GOAL 这类列表，用户会觉得"功能消失了"。v2.1.0 用的就是 18/16，
	// 与它的断点配套，一起保留。
	MinLeft, MinRight int
}

// DefaultSideWidths 返回 KQFLOW 正在用的那套宽度策略。
func DefaultSideWidths() SideWidths {
	return SideWidths{
		WideLeft: 34, WideRight: 30,
		MediumLeft: 28, MediumRight: 24,
		NarrowLeft: 24, NarrowRight: 22,
		WideMin: 110, MediumMin: 90,
		MinLeft: 18, MinRight: 16,
	}
}

// pick 按总宽度选出一对候选宽度。
func (s SideWidths) pick(totalW int) (left, right int) {
	switch {
	case totalW >= s.WideMin:
		return s.WideLeft, s.WideRight
	case totalW >= s.MediumMin:
		return s.MediumLeft, s.MediumRight
	default:
		return s.NarrowLeft, s.NarrowRight
	}
}

// LayoutConfig 是一次布局的全部输入。
type LayoutConfig struct {
	// HeaderRows / FooterRows 是上下栏占用行数。默认各 1。
	//
	// 上栏在有临时提示（toast）时会变成 2 行——这一点由调用方决定，
	// 布局层不去猜内容高度（那是 v2.1.0 出问题的地方：高度被内容撑开）。
	HeaderRows, FooterRows int

	// LeftTiles / RightTiles 是左右栏声明的磁贴数量（0 / 1 / ≥2）。
	// 它们决定槽位是"长条"还是"上下各半"。
	LeftTiles, RightTiles int

	// DockVisible 为假时中栏停靠区整块隐藏（req.md："可选 0~2 个磁贴"）。
	DockVisible bool
	// DockTiles 是停靠区声明的磁贴数量。
	DockTiles int

	// SideWidths 是左右栏宽度策略。零值会被 withDefaults 补成 KQFLOW 那套。
	SideWidths SideWidths

	// MinCenterWidth 是中栏的保底宽度（不足时从两侧借）。
	//
	// 48 的来源（v2.1.0 的注释）：中栏不只有 Logo，帮助/设置/历史/随手记/
	// 统计都挤在这里；完整版 KQFLOW 字模有 52 列，要放它得让中栏 ≥56，
	// 那会把 80 列以下的左右面板挤爆。所以保底按**内容**定，
	// Logo 交给它自己按可用宽度降档。
	MinCenterWidth int

	// MinContentWidth 是"整页内容愿意占据单栏的最小中栏宽度"。
	//
	// 低于它时 Primary 会改用整行（左右栏让位），免得中文被折得太碎。
	MinContentWidth int
}

// DefaultConfig 返回 KQFLOW 正在用的那套布局配置。
//
// 默认停靠区可见但没有磁贴：停靠区无磁贴时主控区独占中栏，
// 因此"可见但没有磁贴"与"隐藏"在几何上等价，不会留下空条。
func DefaultConfig() LayoutConfig {
	return LayoutConfig{
		HeaderRows:      1,
		FooterRows:      1,
		DockVisible:     true,
		SideWidths:      DefaultSideWidths(),
		MinCenterWidth:  48,
		MinContentWidth: 48,
	}
}

// withDefaults 把零值补成默认值。
//
// 只有 HeaderRows/FooterRows 与两个阈值用"零值即默认"的语义——
// 它们取 0 没有意义。**左右栏磁贴数不用这个语义**：0 是合法且常见的输入
// （用户把某栏的磁贴全关了），当成默认值会把布局搞错。
func (c LayoutConfig) withDefaults() LayoutConfig {
	if c.HeaderRows <= 0 {
		c.HeaderRows = 1
	}
	if c.FooterRows < 0 {
		c.FooterRows = 0
	}
	if c.SideWidths == (SideWidths{}) {
		c.SideWidths = DefaultSideWidths()
	}
	if c.MinCenterWidth <= 0 {
		c.MinCenterWidth = 48
	}
	if c.MinContentWidth <= 0 {
		c.MinContentWidth = 48
	}
	if c.LeftTiles < 0 {
		c.LeftTiles = 0
	}
	if c.RightTiles < 0 {
		c.RightTiles = 0
	}
	if c.DockTiles < 0 {
		c.DockTiles = 0
	}
	return c
}
