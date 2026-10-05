package kxapp

import (
	"fmt"

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
}

func (k *kernel) Title() string { return "内核" }

// PowerBy 返回 "Power by KXFLOW"。
func (k *kernel) PowerBy() string { return PowerBy }

// Dashboard 返回栈空时的底色视图：LOGO + 今日概况 + 操作提示。
//
// 它对应 v2.1.0 中栏那块看板（Logo、今日待办计数、选项列表、字条）。
// 这里先实现最必要的三样：LOGO、计数、提示。
func (k *kernel) Dashboard() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "看板",
		RenderFn: func(ctx plugin.RenderCtx) {
			inner := ctx.Rect
			if inner.H < 1 || inner.W < 8 {
				return
			}
			logo := []string{
				"   ╭───────────────────────────────╮",
				"   │           K Q F L O W         │",
				"   ╰───────────────────────────────╯",
			}
			y := inner.Y
			// LOGO 只在高度够的时候画：矮面板里它会把真正要紧的信息挤没。
			if inner.H >= len(logo)+5 {
				for _, l := range logo {
					ctx.Canvas.Text(inner.X, y, canvas.Truncate(l, inner.W), tile.StyleAccent)
					y++
				}
				y++
			}
			y = putLine(ctx, y, inner, PowerBy, tile.StyleMuted)

			// 今日概况：这一行是"我现在处于什么状况"的唯一入口。
			if data := k.src.Day(); data != nil {
				done, total := data.Counts()
				focus, _ := data.FocusTotal()
				line := fmt.Sprintf("今日待办 %d/%d · 专注 %s", done, total, clock.HumanDuration(focus))
				y = putLine(ctx, y, inner, line, tile.StyleStatus)
			}
			if k.src.Day() != nil {
				y = putLine(ctx, y, inner, "日期 "+k.src.Day().Day, tile.StyleMuted)
			}
			y++
			putLine(ctx, y, inner, "tab 切换栏位 · enter 借调中栏 · esc 退回 · q 退出", tile.StyleHint)
		},
	}
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
