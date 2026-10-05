// 命令 kxflow-demo 是 KXFLOW 引擎的演示程序。
//
// 它存在的理由很具体：这套设计里最核心的交互假设是"磁贴借调中栏舞台"，
// 而那种手感（按下去、中栏变成菜单、esc 逐级退回、焦点回到原磁贴）
// **纸面验证不了**，只能在真终端上试。
//
// 它同时是一份"怎么写一个 KXFLOW 产品"的可运行文档：
// 内核怎么定义、磁贴怎么借调舞台、服务怎么接副作用，全在这一个文件里。
//
// 它不参与 kqf.exe 的构建（是独立命令），但属于引擎模块——
// 这样它就不能偷偷依赖 KQFLOW 的业务代码，示例才立得住。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/layout"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// engineAPI 是演示程序编译时对应的引擎 API 版本。
var engineAPI = semver.MustParse("0.1.0")

func main() {
	// -render 是**离屏渲染**模式：渲染一帧就退出，不进入交互循环。
	//
	// 它有两个用处：
	//  1. 在没有真控制台的环境里验证引擎（交互式 TUI 会挂住——这是本项目
	//     既有的一条教训：不要用没有控制台的方式跑 TUI 程序）；
	//  2. 它同时是一份"引擎怎么用"的最短示例：下面二十来行就是完整流程。
	render := flag.Bool("render", false, "离屏渲染一帧后退出（不进入交互）")
	width := flag.Int("w", 120, "离屏渲染的终端宽度")
	height := flag.Int("h", 40, "离屏渲染的终端高度")
	flag.Parse()

	m := kxflow.New(kxflow.Config{
		EngineAPI: engineAPI,
		Services:  newDemoServices(),
		Layout:    layout.DefaultConfig(),
		View:      plugin.NewViewConfig(),
		Packs: []plugin.Pack{
			newCorePack(),
			newClockPack(),
		},
	})

	if *render {
		m.Resize(*width, *height)
		fmt.Println(m.View())
		fmt.Fprintln(os.Stderr, "--- 装载报告 ---")
		fmt.Fprintln(os.Stderr, m.Report().Explain())
		return
	}

	p := tea.NewProgram(adapter{model: m}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "kxflow-demo:", err)
		os.Exit(1)
	}
	fmt.Println("装载报告：")
	fmt.Println(m.Report().Explain())
}

// adapter 把引擎接到 bubbletea 上。
//
// 这一层刻意**薄**：它只做"消息格式转换"，不含任何业务判断。
// 这样引擎本身完全不依赖 bubbletea——换一个 TUI 框架只需重写这个文件。
type adapter struct{ model *kxflow.Model }

func (a adapter) Init() tea.Cmd { return tick() }

// tickMsg 驱动"每秒重绘一次"。
//
// 引擎的渲染是纯函数，因此**必须由宿主推动重绘**：时钟不会自己走。
// 这一点值得写清楚——如果宿主不发这个 tick，界面会停在第一帧，
// 看起来像"程序卡死了"，而引擎本身没有任何问题。
type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (a adapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.model.Resize(m.Width, m.Height)
		return a, nil
	case tickMsg:
		return a, tick()
	case tea.KeyMsg:
		ev := plugin.Event{Kind: plugin.EventKey, Key: m.String()}
		if m.Type == tea.KeyRunes {
			ev.Runes = m.Runes
		}
		if m.Paste {
			ev.Kind = plugin.EventPaste
		}
		if quit := a.model.Dispatch(ev); quit {
			a.model.Dispose()
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a adapter) View() string { return a.model.View() }

// demoServices 是宿主提供的能力实现。
//
// 它演示了引擎与宿主的边界：引擎只说"我要响一下""我要存这个东西"，
// 具体怎么做由宿主决定（这里就真的去写 stdout 与响铃）。
type demoServices struct{}

func newDemoServices() svc.Services { return demoServices{} }

func (demoServices) Clock() time.Time { return time.Now() }

func (demoServices) Persist(req svc.PersistRequest) error {
	// 演示程序不落盘，只确认通道畅通。
	return nil
}

func (demoServices) Effect(e svc.Effect) {
	switch e.Kind {
	case svc.EffectBell:
		fmt.Fprint(os.Stdout, "\a")
	default:
		// 其它副作用在演示里忽略。
	}
}

func (demoServices) Capabilities() svc.Capability {
	return svc.Capability{"demo.bell"}
}

// ---------- 内核 ----------

type corePack struct {
	kernel *coreKernel
}

func newCorePack() *corePack {
	return &corePack{kernel: newCoreKernel()}
}

func (p *corePack) ID() string               { return "demo.core" }
func (p *corePack) Name() string             { return "演示内核" }
func (p *corePack) Version() semver.Version  { return semver.MustParse("0.1.0") }
func (p *corePack) Enabled() bool            { return true }
func (p *corePack) Provides() []string       { return []string{"demo.core"} }
func (p *corePack) Requires() []string       { return nil }
func (p *corePack) Conflicts() []string      { return nil }
func (p *corePack) EngineAPI() semver.Range  { return semver.MustRange(">=0.1 <0.2") }
func (p *corePack) Members() []plugin.Plugin { return []plugin.Plugin{&corePlugin{p: p}} }
func (p *corePack) Assemble(s svc.Services) (plugin.Assembled, error) {
	p.kernel.svc = s
	return &coreAssembled{p: p}, nil
}

type coreAssembled struct{ p *corePack }

func (a *coreAssembled) Pack() plugin.Pack                  { return a.p }
func (a *coreAssembled) Kernel() plugin.Kernel              { return a.p.kernel }
func (a *coreAssembled) Tiles() []plugin.Component          { return nil }
func (a *coreAssembled) BoardOptions() []plugin.BoardOption { return a.p.kernel.BoardOptions() }
func (a *coreAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *coreAssembled) Services() []plugin.Service { return nil }
func (a *coreAssembled) Dispose()                   {}

type corePlugin struct{ p *corePack }

func (c *corePlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: "demo.kernel", Name: "演示内核", Kind: plugin.KindKernel,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
	}
}
func (c *corePlugin) New(s svc.Services) (plugin.Component, error) { return c.p.kernel, nil }

// coreKernel 是内核：渲染 LOGO 与"Power by"，提供默认看板与全局选项。
type coreKernel struct {
	svc     svc.Services
	entered int
	// options 由引擎注入：返回当前可用的选项与键位。
	options func() []plugin.OptionBindingView
}

func newCoreKernel() *coreKernel { return &coreKernel{} }

func (k *coreKernel) Title() string { return "内核" }

// PowerBy 是内核必须显示的字样（req.md：中栏渲染本内核 LOGO 与 "Power by KXFLOW"）。
func (k *coreKernel) PowerBy() string { return "Power by KXFLOW" }

// SetOptionSource 接收引擎注入的"当前可用选项"查询函数。
func (k *coreKernel) SetOptionSource(f func() []plugin.OptionBindingView) { k.options = f }

// Dashboard 是栈空时的底色视图：LOGO + **当前可用选项** + 说明。
func (k *coreKernel) Dashboard() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "看板",
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				for _, l := range ctx.Wrap(s) {
					if y >= ctx.Rect.Y1() {
						return
					}
					ctx.Canvas.Text(ctx.Rect.X, y, l, style)
					y++
				}
			}
			for _, l := range canvas.LogoLines(ctx.Rect.W) {
				put(l, 1)
			}
			put(k.PowerBy(), 1)
			put("", 1)
			put("这是引擎的演示程序，不含业务功能。", 2)
			put("", 1)

			// 列出当前可用选项——这是"选中之后能做什么"的可见入口。
			// 空列表时说明原因，而不是留白（留白会让人以为界面坏了）。
			//
			// 注意不显示按键编号：选项用方向键在菜单里选（按 l 打开），
			// 数量不受限（用户反馈：有 10 个选项怎么办）。
			if k.options != nil {
				bindings := k.options()
				if len(bindings) == 0 {
					put("（选中一个磁贴后，这里会出现可用操作）", 6)
				} else {
					put("可用操作（按 l 选择）", 6)
				}
				for _, b := range bindings {
					style := canvas.StyleID(2)
					mark := "  · "
					if b.Context {
						style, mark = 1, "  ▸ "
					}
					put(mark+b.Label, style)
				}
			}
			put("", 1)
			put("  左栏「时钟」enter 借调；「关于」a 打开关于页、enter 上报选中", 6)
			put("  左右栏之间用 tab 切换焦点；esc 逐级退回", 6)
		},
	}
}

// Render 让内核也能作为组件被渲染（这里只画一行，示意它可用）。
func (k *coreKernel) Render(ctx plugin.RenderCtx) {
	ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y, canvas.Truncate("KXFLOW "+k.PowerBy(), ctx.Rect.W), 1)
}

// Update 内核组件本身不处理按键。
func (k *coreKernel) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	return plugin.None()
}

// BoardOptions 返回内核自带的全局选项。
//
// ⚠️ 内核**不要**在这里返回任何选项：引擎的 Manager.BoardOptions 已经把
// 内核的成员选项（aboutOption 那个成员）收过一次了。这里再返回一遍，
// 看板上就会出现重复条目——实测症状是"1 帮助 / 2 帮助 / 3 关于 / 4 关于"，
// 按 1 与按 2 效果相同，看起来像界面坏了。
//
// 内核自带的**真正**全局选项（设置/帮助/退出这类）应当在这里返回；
// 本演示没有，因此返回 nil。
func (k *coreKernel) BoardOptions() []plugin.BoardOption { return nil }

type aboutOption struct{ k *coreKernel }

func (o *aboutOption) Label() string { return "关于 / About" }
func (o *aboutOption) Order() int    { return 100 }

// Activate 打开"关于"页（借调舞台）。
func (o *aboutOption) Activate(svc.Services) (plugin.View, error) {
	return &plugin.ViewFunc{
		ViewName: "关于",
		RenderFn: func(ctx plugin.RenderCtx) {
			drawLines(ctx, []string{
				"KXFLOW 引擎演示",
				"",
				"  " + o.k.PowerBy(),
				"",
				"  本页面由内核的看板选项借调舞台打开。",
			}, 2)
		},
	}, nil
}

// ---------- 时钟磁贴包 ----------

type clockPack struct{}

func newClockPack() *clockPack { return &clockPack{} }

func (p *clockPack) ID() string              { return "demo.clock" }
func (p *clockPack) Name() string            { return "时钟与关于" }
func (p *clockPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *clockPack) Enabled() bool           { return true }
func (p *clockPack) Provides() []string      { return []string{"demo.clock"} }
func (p *clockPack) Requires() []string      { return []string{"demo.core"} }
func (p *clockPack) Conflicts() []string     { return nil }
func (p *clockPack) EngineAPI() semver.Range { return semver.MustRange(">=0.1 <0.2") }
func (p *clockPack) Members() []plugin.Plugin {
	return []plugin.Plugin{
		&clockPlugin{},
		&aboutPlugin{},
	}
}
func (p *clockPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	return &clockAssembled{
		svc:   s,
		clock: &clockTile{svc: s},
		about: &aboutTile{svc: s},
	}, nil
}

type clockAssembled struct {
	svc   svc.Services
	clock *clockTile
	about *aboutTile
}

func (a *clockAssembled) Pack() plugin.Pack                  { return &clockPack{} }
func (a *clockAssembled) Kernel() plugin.Kernel              { return nil }
func (a *clockAssembled) Tiles() []plugin.Component          { return []plugin.Component{a.clock, a.about} }
func (a *clockAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *clockAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *clockAssembled) Services() []plugin.Service { return nil }
func (a *clockAssembled) Dispose()                   {}

type clockPlugin struct{}

func (p *clockPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: "demo.clock.tile", Name: "时钟", Kind: plugin.KindTile,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     plugin.SlotPreference{Anchor: geometry.AnchorLeftTop, Priority: 10},
	}
}
func (p *clockPlugin) New(s svc.Services) (plugin.Component, error) { return &clockTile{svc: s}, nil }

type aboutPlugin struct{}

func (p *aboutPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: "demo.about.tile", Name: "关于", Kind: plugin.KindTile,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     plugin.SlotPreference{Anchor: geometry.AnchorLeftBottom, Priority: 5},
	}
}
func (p *aboutPlugin) New(s svc.Services) (plugin.Component, error) { return &aboutTile{svc: s}, nil }

// clockTile 是一个会走动的时钟磁贴——演示"磁贴自己管自己的内容"。
type clockTile struct{ svc svc.Services }

func (t *clockTile) Title() string { return "时钟" }

func (t *clockTile) Render(ctx plugin.RenderCtx) {
	now := ctx.Svc.Clock().Format("15:04:05")
	lines := []string{
		"",
		"  " + now,
		"",
		"  按 enter 借调中栏舞台",
	}
	y := ctx.Rect.Y
	for _, l := range ctx.WrapLines(lines) {
		if y >= ctx.Rect.Y1() {
			break
		}
		ctx.Canvas.Text(ctx.Rect.X, y, l, 2)
		y++
	}
}

// Update 演示**借调**：按 enter 把一个视图推进中栏舞台。
func (t *clockTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	if ev.Key != "enter" {
		return plugin.None()
	}
	return plugin.Borrow(&plugin.ViewFunc{
		ViewName: "时钟菜单",
		RenderFn: func(rc plugin.RenderCtx) {
			lines := []string{
				"时钟菜单（借调中栏舞台）",
				"",
				"  · 现在时刻：" + rc.Svc.Clock().Format("2006-01-02 15:04:05"),
				"  · 时区：" + rc.Svc.Clock().Format("MST"),
				"",
				"  按 s 进入第二级（演示多级菜单 = 栈更深）",
				"  按 esc 逐级退回，焦点会回到「时钟」磁贴",
			}
			drawLines(rc, lines, 1)
		},
		UpdateFn: func(ec plugin.EventCtx, e plugin.Event) (plugin.Action, bool) {
			switch e.Key {
			case "s":
				// 再借调一层：栈深 > 1 就是 req.md 说的"菜单栈"。
				return plugin.Borrow(&plugin.ViewFunc{
					ViewName: "时钟二级",
					RenderFn: func(rc plugin.RenderCtx) {
						drawLines(rc, []string{
							"第二级菜单（栈深 = 2）",
							"",
							"  这一层是「时钟菜单」再借调出来的。",
							"  esc 退回第一级，再 esc 退回看板。",
						}, 1)
					},
				}), false
			}
			return plugin.None(), false
		},
	})
}

// aboutTile 演示"联动选项"的来源：它上报选中上下文，由引擎广播。
//
// 它自己也是"二级内容必须借调舞台"的示范：磁贴只有二十几列宽，
// 而"关于"里有整段说明文字。**把长文本塞进小磁贴必然出问题**——
// 实测被截成"（焦点在本磁贴时按 enter 会上"，用户以为渲染坏了。
// 正确做法是磁贴只放一行摘要，长内容按 enter 借调中栏显示。
type aboutTile struct{ svc svc.Services }

func (t *aboutTile) Title() string { return "关于" }

func (t *aboutTile) Render(ctx plugin.RenderCtx) {
	sel := ctx.Selection
	// 磁贴里只放**短摘要**：一眼能读完，不依赖折行。
	summary := "未选中"
	if sel.ID != "" {
		summary = "已选中：" + sel.Title
	}
	lines := []string{
		"  " + nowOrDash(ctx) + "  KXFLOW 演示",
		"  " + summary,
		"  enter 打开关于",
	}
	y := ctx.Rect.Y
	for _, l := range ctx.WrapLines(lines) {
		if y >= ctx.Rect.Y1() {
			break
		}
		ctx.Canvas.Text(ctx.Rect.X, y, l, 2)
		y++
	}
}

// nowOrDash 返回当前时刻，供摘要行使用。
func nowOrDash(ctx plugin.RenderCtx) string { return ctx.Svc.Clock().Format("15:04") }

// Update 演示**借调 + 选中上报**：按 a 借调"关于"页，按 enter 上报选中。
//
// 两个动作分开按键，是为了让"借调"与"选中"这两套机制各自可观察。
func (t *aboutTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	switch ev.Key {
	case "a":
		return plugin.Borrow(aboutView())
	case "enter":
		if ctx.Selection.ID == "about" {
			// 再按一次取消选中，用来观察联动选项的消失。
			return plugin.Select(plugin.Selection{})
		}
		return plugin.Select(plugin.Selection{
			Kind: "demo.tile", ID: "about", Title: "关于",
			Can: svc.Capability{"demo.bell"},
		})
	}
	return plugin.None()
}

// aboutView 返回"关于"页（借调舞台显示）。
//
// 它拿到的是**整个中栏**而不是一个小磁贴，所以可以放心写长句。
func aboutView() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "关于",
		RenderFn: func(rc plugin.RenderCtx) {
			// 在这里用 ctx.WrapLines：借调区虽然宽，窄终端下仍可能不够，
			// 折行比截断安全。
			drawWrappedLines(rc, []string{
				"KXFLOW 渲染引擎",
				"",
				"  Power by KXFLOW",
				"",
				"  你以为这是一句很长的话会被截断，但其实它会自动折行显示——",
				"  磁贴只有二十几列宽时，长文本必须折行而不是截断，",
				"  否则用户看到的是半句话，会以为渲染坏了。",
				"",
				"  esc 退回看板，焦点会回到「关于」磁贴。",
			}, 1)
		},
	}
}

// drawWrappedLines 逐行画，但**按当前宽度折行**而不是截断。
//
// 演示里原先用的是 canvas.Truncate，于是"关于"磁贴里前两行直接被砍掉，
// 只剩下第五行的尾巴——这正是用户反馈的那个现象。
func drawWrappedLines(ctx plugin.RenderCtx, lines []string, style canvas.StyleID) {
	y := ctx.Rect.Y
	for _, l := range lines {
		for _, w := range ctx.Wrap(l) {
			if y >= ctx.Rect.Y1() {
				return
			}
			ctx.Canvas.Text(ctx.Rect.X, y, w, style)
			y++
		}
	}
}

// drawLines 是演示里反复用到的"逐行画"辅助（会折行，不截断）。
func drawLines(ctx plugin.RenderCtx, lines []string, style canvas.StyleID) {
	drawWrappedLines(ctx, lines, style)
}
