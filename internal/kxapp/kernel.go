package kxapp

import (
	"fmt"
	"strings"

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
