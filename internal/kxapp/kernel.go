package kxapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// KernelID 是内核包的 ID。
const KernelID = "kqflow.core"

// PowerBy 是内核必须显示的字样（req.md：中栏渲染本内核 LOGO 与 "Power by KXFLOW"）。
const PowerBy = "Power by KXFLOW"

// engineRange 是包们编译时对应的引擎 API 范围。
//
// 它写成常量而不是每处重复：引擎破坏性改动时只改这一处，
// 忘了改的包会在装载报告里被明确点出来（而不是静默错位）。
var engineRange = semver.MustRange(">=0.1 <0.2")

// kernelPack 是内核整合包：只有一个成员（内核插件）。
//
// 做成"只有一个成员的包"是为了让装载路径只有一条——不必为内核开特例，
// 它也能自然享受版本与冲突裁决（design §4.3）。
type kernelPack struct {
	kernel *kernel
}

// newKernelPack 创建内核包。
func newKernelPack(src Source, st *HostState) *kernelPack {
	return &kernelPack{kernel: &kernel{src: src, state: st}}
}

func (p *kernelPack) ID() string               { return KernelID }
func (p *kernelPack) Name() string             { return "KQFLOW 内核" }
func (p *kernelPack) Version() semver.Version  { return semver.MustParse("0.1.0") }
func (p *kernelPack) EngineAPI() semver.Range  { return engineRange }
func (p *kernelPack) Enabled() bool            { return true }
func (p *kernelPack) Provides() []string       { return []string{"kqflow.kernel"} }
func (p *kernelPack) Requires() []string       { return nil }
func (p *kernelPack) Conflicts() []string      { return nil }
func (p *kernelPack) Members() []plugin.Plugin { return []plugin.Plugin{&kernelPlugin{p: p}} }

func (p *kernelPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	p.kernel.svc = s
	return &kernelAssembled{p: p}, nil
}

type kernelPlugin struct{ p *kernelPack }

func (k *kernelPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: "kqflow.kernel", Name: "内核", Kind: plugin.KindKernel,
		Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
	}
}

func (k *kernelPlugin) New(s svc.Services) (plugin.Component, error) {
	k.p.kernel.svc = s
	return k.p.kernel, nil
}

type kernelAssembled struct{ p *kernelPack }

func (a *kernelAssembled) Pack() plugin.Pack                  { return a.p }
func (a *kernelAssembled) Kernel() plugin.Kernel              { return a.p.kernel }
func (a *kernelAssembled) Tiles() []plugin.Component          { return nil }
func (a *kernelAssembled) BoardOptions() []plugin.BoardOption { return a.p.kernel.BoardOptions() }
func (a *kernelAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *kernelAssembled) Services() []plugin.Service { return nil }
func (a *kernelAssembled) Dispose()                   {}

// kernel 是内核组件：提供默认看板与全局看板选项。
type kernel struct {
	src   Source
	state *HostState
	svc   svc.Services
	// options 由引擎注入：返回"当前可用的选项与键位"。
	//
	// 看板每一帧都去问它，因此选中条目后联动选项会**立刻出现**在
	// 看板列表里——这是"功能可被发现"的关键一环。
	options func() []plugin.OptionBindingView

	// quoteIdx 是当前显示的字条下标（见 Quotes）。
	// quoteAt 是**当前这条字条开始显示的时刻**（不是"上次 Tick 的时刻"）。
	quoteIdx int
	quoteAt  time.Time
}

// QuoteEvery 是看板字条的轮换间隔（与 2.1.0 的 quoteEvery 一致）。
const QuoteEvery = 12 * time.Second

// Quotes 是看板中间滚动展示的句子（照 2.1.0 的 state.go 原样搬过来）。
//
// 它们不是功能，但是**产品气质**的一部分：2.1.0 的看板正中一直有句话，
// 引擎版把它漏了，看板就显得空。配置里设了自定义字条时用用户的那份。
var Quotes = []string{
	"时机成熟时，一切都会水到渠成。",
	"把今天过好，就是对未来最好的投资。",
	"专注不是做更多，而是少做一点别的。",
	"不必完美地开始，只需开始。",
	"你不需要更多时间，你需要更少的干扰。",
	"每一次开始计时，都是一次对目标的投票。",
	"拖延的代价，是别人替你过完这一生。",
	"慢慢来，但别停。",
	"今天的一小步，抵得过明天的一大步。",
	"记录本身就是觉察，觉察本身就是改变。",
	"完成胜过完美。",
	"时间不会等人，但它会奖励尊重它的人。",
	"你专注的地方，就是你人生生长的地方。",
	"先做最重要的那件事，其余自会排队。",
}

// CurrentQuote 返回当前要展示的字条（自定义优先，与 2.1.0 同口径）。
func (k *kernel) CurrentQuote() string {
	list := k.quoteList()
	if len(list) == 0 {
		return ""
	}
	if k.quoteIdx < 0 || k.quoteIdx >= len(list) {
		k.quoteIdx = 0
	}
	return list[k.quoteIdx]
}

// quoteList 返回当前生效的字条列表。
func (k *kernel) quoteList() []string {
	if cfg := k.src.Config(); cfg != nil && len(cfg.Quotes) > 0 {
		out := make([]string, 0, len(cfg.Quotes))
		for _, q := range cfg.Quotes {
			if q = DisplayTitle(q); q != "" {
				out = append(out, q)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return Quotes
}

// NextQuote 轮换到下一条字条。
//
// 顺序轮换（不是随机）：2.1.0 用随机并刻意避开重复，但在"每 12 秒换一次"
// 的频率下，顺序轮换更可预期——用户能感觉到"它在走"，而不是"它有时重复"。
func (k *kernel) NextQuote() {
	n := len(k.quoteList())
	if n <= 1 {
		return
	}
	k.quoteIdx = (k.quoteIdx + 1) % n
}

// maybeRotateQuote 按"这条字条已经显示了多久"决定要不要换。
//
// ⚠️ 判据必须从**当前字条开始显示的时刻**算起，不能在每次 Tick 时
// 都刷新基准时刻。我第一版就是每次 Tick 都把基准设成 now，于是
// "每 11 秒 Tick 一次"永远攒不满 12 秒，字条再也不换——而且不换
// 没有任何报错，只是静静地卡在第一句。测试（TestQuotesRotateOnTick）
// 正是为了钉住这个场景：不规则的 Tick 间隔也必须能按时轮换。
func (k *kernel) maybeRotateQuote(now time.Time) {
	if k.quoteAt.IsZero() {
		k.quoteAt = now
		return
	}
	// 用 for 而不是 if：如果程序被挂起了很久（比如笔记本合盖），
	// 醒来后不该只前进一条——那会让用户看到"刚打开还停在老句子"。
	// 按经过的整倍数前进，视觉上等价于"它一直在走"。
	for now.Sub(k.quoteAt) >= QuoteEvery && !k.quoteAt.IsZero() {
		k.quoteAt = k.quoteAt.Add(QuoteEvery)
		k.NextQuote()
	}
}

func (k *kernel) Title() string { return "内核" }

// PowerBy 返回 "Power by KXFLOW"。
func (k *kernel) PowerBy() string { return PowerBy }

// SetOptionSource 注入选项查询函数（由引擎在装配时调用）。
func (k *kernel) SetOptionSource(f func() []plugin.OptionBindingView) { k.options = f }

// Dashboard 返回栈空时的底色视图：LOGO + 今日概况 + **可用选项** + 操作提示。
//
// 它对应 v2.1.0 中栏那块看板（Logo、今日待办计数、选项列表、字条）。
// 选项列表是重点：没有它，联动选项虽然在机制上"出现了"，
// 用户却无从知晓怎么用（实机反馈过这一点）。
func (k *kernel) Dashboard() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "看板",
		// Update 处理 Tick：字条按时间轮换。
		//
		// 引擎的 Tick 会走借调中的视图（stage top），而看板正是
		// "没有借调时"的 stage top，因此这条路走得通。
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			if ev.Kind == plugin.EventTick {
				k.maybeRotateQuote(ec.Now)
			}
			return plugin.None(), false
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			inner := ctx.Rect
			if inner.H < 1 || inner.W < 8 {
				return
			}
			y := inner.Y

			// LOGO 按可用宽度自动降档，绝不折行。
			for _, l := range canvas.LogoLines(inner.W) {
				if y >= inner.Y1() {
					return
				}
				ctx.Canvas.Text(inner.X, y, centerLine(l, inner.W), tile.StyleAccent)
				y++
			}
			y = putLine(ctx, y, inner, centerLine(k.PowerBy(), inner.W), tile.StyleMuted)

			// 今日概况：一行说清"我现在处于什么状况"。
			if data := k.src.Day(); data != nil {
				done, total := data.Counts()
				focus, _ := data.FocusTotal()
				line := fmt.Sprintf("今日待办 %d/%d · 专注 %s · %s",
					done, total, clock.HumanDuration(focus), data.Day)
				y = putLine(ctx, y, inner, centerLine(line, inner.W), tile.StyleStatus)
			}
			y++

			// 字条：看板正中那句话（2.1.0 一直有，引擎版补上）。
			//
			// 折行而不是截断：字条是给人读的，半句话比不显示更糟。
			if q := k.CurrentQuote(); q != "" {
				for _, line := range canvas.Wrap(q, inner.W-2) {
					if y >= inner.Y1() {
						break
					}
					ctx.Canvas.Text(inner.X, y, centerLine(line, inner.W), tile.StyleAccent)
					y++
				}
				y++
			}

			// **可用选项**：这是本视图最重要的部分。
			y = k.drawOptions(ctx, y, inner)
			putLine(ctx, y, inner,
				centerLine("tab 换栏位 · j/k 移动 · l 操作 · esc 退回 · q 退出", inner.W),
				tile.StyleHint)
		},
	}
}

// drawOptions 画出当前可用的选项（联动选项在前，看板选项在后）。
//
// ⚠️ 这里**不显示任何按键编号**（用户 2026-10-05 的反馈）：
//
//	原话：不应该保留数字作为快捷键：如果有 10 个选项怎么办呢？
//
// 看板只是**告诉用户有哪些事可做**（需要时按 l 打开菜单去选），
// 因此列表用符号而不是数字，数量也不受 9 个限制。
func (k *kernel) drawOptions(ctx plugin.RenderCtx, y int, inner geometry.Rect) int {
	if k.options == nil {
		return y
	}
	bindings := k.options()
	if len(bindings) == 0 {
		return putLine(ctx, y, inner, centerLine("（选中一个条目后，这里会出现可用操作）", inner.W), tile.StyleMuted)
	}
	putLine(ctx, y, inner, "可用操作（按 l 选择）", tile.StyleMuted)
	y++
	for _, b := range bindings {
		if y >= inner.Y1() {
			return y
		}
		style := tile.StyleMuted
		mark := "  · "
		if b.Context {
			style = tile.StyleAccent
			mark = "  ▸ "
		}
		// 折行而不是截断：选项名带条目名时可能很长。
		y = drawWrappedInset(ctx, y, inner, mark, optionGutter, b.Label, style)
	}
	return y
}

// optionGutter 是选项说明文字的续行缩进（对齐到记号右侧）。
const optionGutter = "    "

// centerLine 在给定宽度内居中一段**纯文本**（不截断，超宽原样返回）。
func centerLine(s string, width int) string {
	w := canvas.StringWidth(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", (width-w)/2) + s
}

// FocusSelection 回报"本组件获得焦点时选中了谁"。
//
// 内核与各种"没有可选条目"的磁贴都返回零值——这一点很关键：
// 它把**上一个磁贴的选中清掉**。否则 tab 到一个没有列表的磁贴上，
// 内里的选中还留着上一条，按 l 就会作用在看不见的地方。
func (k *kernel) FocusSelection(plugin.RenderCtx) plugin.Selection {
	return plugin.Selection{}
}

// HeaderContent 提供上栏内容。
//
// 用户反馈"上栏和下栏用得不多"，根因就是这里没人提供内容。
// 上栏该显示的都是**业务概念**（今天是哪天、完成多少、现在几点、
// 昵称问候），因此只能由内核给——引擎只负责把它排版。
//
// 内容与 2.1.0 的 renderHeader 对齐（照它的源码抄，而不是自己发明）：
//
//	左：问候语（+昵称）  +  日期 · 星期 · 时刻 · 日界线
//	右：今日专注时长
//
// 问候语与日界线都用 internal/clock 的现成实现：那两处都是
// "算错了极难看出来"的逻辑（日界线尤其），只能有一份实现。
func (k *kernel) HeaderContent(ctx plugin.RenderCtx) plugin.HeaderContent {
	now := ctx.Svc.Clock()
	cfg := k.src.Config()

	cut := time.Duration(0)
	loc := time.Local
	if cfg != nil {
		cut = cfg.Cutoff()
		loc = cfg.Location()
	}

	// 问候语。Nickname 为空时不留"你好，"这种半句。
	greeting := clock.Greeting(now, cut) + "！"
	if cfg != nil {
		if nick := strings.TrimSpace(DisplayTitle(cfg.Nickname)); nick != "" {
			greeting = clock.Greeting(now, cut) + "，" + nick + "！"
		}
	}

	// 日期段：日期 · 星期 · 时刻 · 日界线。
	day := ""
	if data := k.src.Day(); data != nil {
		day = data.Day
	}
	weekCN := ""
	if day != "" {
		if wd, err := clock.Weekday(day, loc); err == nil {
			weekCN = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[wd]
		}
	}
	dateParts := make([]string, 0, 4)
	if day != "" {
		dateParts = append(dateParts, day)
	}
	if weekCN != "" {
		dateParts = append(dateParts, weekCN)
	}
	dateParts = append(dateParts, now.Format("15:04:05"))
	if cfg != nil {
		dateParts = append(dateParts, "日界线 "+clock.WallClock(cut))
	}

	left := greeting
	if len(dateParts) > 0 {
		left += "  " + strings.Join(dateParts, " · ")
	}

	// 右：今日专注时长（用户最关心的一项，窄终端下它优先保留）。
	right := ""
	if data := k.src.Day(); data != nil {
		focus, _ := data.FocusTotal()
		right = "今日专注 " + clock.HumanDuration(focus)
	}
	return plugin.HeaderContent{Left: left, Right: right}
}

// Render 让内核也能作为组件被渲染（中栏以外的场合不画东西）。
func (k *kernel) Render(ctx plugin.RenderCtx) {}

// Update 内核组件本身不处理按键：它的界面由 Dashboard 视图承担。
func (k *kernel) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }

// BoardOptions 返回内核自带的全局看板选项。
//
// 见 design §5.3：它们是**内核自带**而不是"选项插件"——
// 任何基于 KXFLOW 的产品都需要"设置/帮助/退出"，
// 让它们归属某个可卸载的包没有意义。
func (k *kernel) BoardOptions() []plugin.BoardOption {
	return []plugin.BoardOption{
		&simpleOption{label: "帮助 / Help", order: 90, view: k.helpView},
		&simpleOption{label: "关于 / About", order: 95, view: k.aboutView},
	}
}

// helpView 是帮助页。
func (k *kernel) helpView() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "帮助",
		RenderFn: func(ctx plugin.RenderCtx) {
			lines := []string{
				"KQFLOW · 帮助",
				"",
				"  tab        切换栏位（左栏固定/临时、右栏目标）",
				"  j / k      上下移动",
				"  space      勾选完成",
				"  enter      借调中栏（进入下级界面）",
				"  esc        退回上一级",
				"  q          退出",
				"",
				"  联动选项（如设截止时间、打标签）只在选中条目时出现。",
			}
			putLines(ctx, lines)
		},
	}
}

// aboutView 是"关于"页。
func (k *kernel) aboutView() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "关于",
		RenderFn: func(ctx plugin.RenderCtx) {
			putLines(ctx, []string{
				"KQFLOW · 关于",
				"",
				"  " + PowerBy,
				"",
				"  本界面由 KXFLOW 渲染引擎驱动：",
				"    · 布局只有一处权威实现（kxflow/layout）",
				"    · 所有内容画在固定尺寸画布上，越界写不进去",
				"    · 二级内容通过「借调舞台」显示，天然只占中栏",
			})
		},
	}
}

// simpleOption 是一个"打开某个页面"的看板选项。
//
// 它刻意做得很薄：内核的选项都是"打开一页"，没有别的花样。
// 各业务包需要更复杂的行为时，自己实现 plugin.BoardOption 即可。
type simpleOption struct {
	label string
	order int
	view  func() plugin.View
}

func (o *simpleOption) Label() string { return o.label }
func (o *simpleOption) Order() int    { return o.order }

// Activate 返回要借调舞台显示的页面。
func (o *simpleOption) Activate(svc.Services) (plugin.View, error) { return o.view(), nil }

// putLine 在 (x, y) 画一行，返回下一行的 y；超出区域则原样返回 y 且不画。
func putLine(ctx plugin.RenderCtx, y int, r geometry.Rect, text string, style canvas.StyleID) int {
	if y >= r.Y1() {
		return y
	}
	ctx.Canvas.Text(r.X, y, canvas.Truncate(text, r.W), style)
	return y + 1
}

// drawWrapped 画一段可能超宽的文本，**折行而不是截断**，返回下一行的 y。
//
// 这是磁贴里画文字的标准入口。用它的理由来自实机反馈：
// 一句"（焦点在本磁贴时按 enter 会上报选中）"在 24 列的磁贴里被截成
// "（焦点在本磁贴时按 enter 会上"——用户看到半句话，会以为渲染坏了。
//
// 折行会**消耗额外的行**，因此行数不够时仍然要截断；但那种情况下
// 界面上是"这段文字没显示完"，而不是"一句话被拦腰砍断"，语义清楚得多。
func drawWrapped(ctx plugin.RenderCtx, y int, r geometry.Rect, text string, style canvas.StyleID) int {
	return drawWrappedInset(ctx, y, r, "", " ", text, style)
}

// drawWrappedInset 是 drawWrapped 的加强版：可指定**行首前缀**与续行缩进。
//
// 用途是"记号 + 内容"的列表项：第一行 "▸ ○ 写文档"，续行对齐到内容起始处。
// 不这样做的话续行会顶到最左边，看起来像另一条条目。
//
// ⚠️ prefix 与 indent 的宽度**都**要算进可用宽度：缩进是真实占用的列，
// 不算的话每行会多画几列（曾经写成只减 markup 的宽度，
// 于是续行总比可用宽度长一截，被画布裁掉或压到边框上）。
func drawWrappedInset(ctx plugin.RenderCtx, y int, r geometry.Rect, prefix, indent, text string, style canvas.StyleID) int {
	if r.W <= 0 {
		return y
	}
	// 缩进也不能超过总宽：过长的缩进会让可用宽度变成负数。
	if indentW := canvas.StringWidth(indent); indentW >= r.W {
		return putLine(ctx, y, r, prefix, style)
	}
	avail := textAreaWidth(r.W, canvas.StringWidth(prefix), canvas.StringWidth(indent))
	if avail < 1 {
		// 前缀就把宽度吃光了：退化为"只画前缀"，至少不丢条目记号。
		return putLine(ctx, y, r, prefix, style)
	}
	lines := canvas.Wrap(text, avail)
	for i, l := range lines {
		if y >= r.Y1() {
			return y
		}
		head := indent
		if i == 0 {
			head = prefix
		}
		ctx.Canvas.Text(r.X, y, head+l, style)
		y++
	}
	return y
}

// textAreaWidth 返回"记号 + 缩进"之后真正能给正文用的列数。
//
// 它必须与 drawWrappedInset 内部用的是**同一个**算式：
// 之前两边各写一份，改了一处没改另一处，结果是滚动计算与绘制
// 对"一条占几行"的判断不一致（长条目会算少一行，光标跑到可视区外）。
func textAreaWidth(total, prefixW, indentW int) int {
	return total - prefixW - indentW
}

// itemRowWidth 返回列表项正文的可用列数（供滚动计算与绘制共用）。
func itemRowWidth(r geometry.Rect) int {
	return textAreaWidth(r.W, markerWidth, listIndentWidth)
}

// listIndentWidth 是列表项续行的缩进宽度。
const listIndentWidth = 2

// putLines 从内容区顶部逐行画一段文本。
func putLines(ctx plugin.RenderCtx, lines []string) {
	y := ctx.Rect.Y
	for _, l := range lines {
		if y >= ctx.Rect.Y1() {
			return
		}
		ctx.Canvas.Text(ctx.Rect.X, y, canvas.Truncate(l, ctx.Rect.W), tile.StyleMuted)
		y++
	}
}
