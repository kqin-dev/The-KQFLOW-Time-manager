// 命令 kqf 是 KQFLOW 时间管理器的入口。
//
// KQFLOW 是一个无环境依赖、可直接分发的命令行时间管理工具：
// 每日 TODO、长期 GOAL、番茄钟与计时、历史统计，全部数据保存在本地。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/kxapp"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/store"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/ui"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kqf:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		showVersion = flag.Bool("version", false, "显示版本号后退出")
		showDataDir = flag.Bool("where", false, "显示数据目录后退出")
		dataDir     = flag.String("data-dir", "", "指定数据目录（覆盖配置）")
		engine      = flag.String("engine", "",
			"选择渲染引擎：legacy（默认，v2.1.0 界面）或 kxflow（新引擎）")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("KQFLOW %s\n", version.Version)
		return nil
	}

	// 先解析路径，再读配置，最后打开数据层。
	cfg := config.Default()
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	paths, err := config.Resolve(cfg)
	if err != nil {
		return err
	}
	if *showDataDir {
		fmt.Println(paths.Root)
		return nil
	}

	loaded, err := config.Load(paths)
	if err != nil {
		return err
	}
	// 命令行显式指定的数据目录优先级最高。
	if *dataDir != "" {
		loaded.DataDir = *dataDir
		if paths, err = config.Resolve(loaded); err != nil {
			return err
		}
	}
	cfg = loaded
	// 命令行显式指定的引擎优先于配置：它主要用于"这一刻想试一下新引擎"，
	// 不该逼用户先去改配置文件（也不该被写回配置——试完就忘）。
	if *engine != "" {
		cfg.Engine = *engine
	}

	// 数据版本保护（见 bug.md 注意 1）：数据来自更新的版本时拒绝启动，
	// 而不是按当前结构读进来再整份写回——那会把新版本的字段直接抹掉。
	// 必须排在 store.Open 之前：Open 会建目录，而发现未来版本后不该留下痕迹。
	if err := store.CheckDataVersion(paths.Root); err != nil {
		return err
	}

	st, err := store.Open(paths.Root)
	if err != nil {
		return err
	}

	// 迁移开关：新老引擎并行，**老路径不许坏**（design §8 的纪律）。
	//
	// 两个分支共用上面那一段装配（路径解析、配置加载、数据版本检查），
	// 因此"换成新引擎"不会改变任何数据层面的行为——
	// 差别只在这里往下：谁来渲染。
	if cfg.UseKXFLOW() {
		if err := runKXFLOW(st, paths, cfg); err != nil {
			return err
		}
		printFarewell(st)
		return nil
	}

	app, err := ui.NewApp(ui.Options{
		Store:  st,
		Config: cfg,
		Clock:  clock.NewWith(time.Now, cfg.Location()),
		Paths:  paths,
	})
	if err != nil {
		return err
	}

	// 括号粘贴是 Bubble Tea 的默认行为，粘贴进来的中文会整段写入输入框。
	// 鼠标上报默认关闭——它会让终端无法用鼠标选中文本，反而堵死“复制粘贴中文”这条路。
	// 需要鼠标时可在配置里把 mouse 设为 true。
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	prog := tea.NewProgram(app, opts...)
	if _, err := prog.Run(); err != nil {
		return err
	}

	printFarewell(st)
	return nil
}

// runKXFLOW 用 KXFLOW 引擎跑界面（v3.0.0 的新路径）。
//
// 它与 legacy 路径共用同一个 store 与配置对象：因此切换引擎**不会**
// 触碰任何数据，两种界面看到的是同一份东西。
//
// 适配层只有薄薄一层（窗口尺寸与按键的格式转换）：引擎本身不认识
// bubbletea，这正是它能被单独拆出去的前提（design §2）。
func runKXFLOW(st *store.Store, paths *config.Paths, cfg *config.Config) error {
	now := func() time.Time { return clock.NewWith(time.Now, cfg.Location()).Now() }
	src, err := kxapp.NewStoreSource(st, paths, cfg, now)
	if err != nil {
		return err
	}
	m, _, rep := kxapp.NewLoader(src, cfg).Build()

	// 装载报告里有"警告/错误"两类信息，用户可能想知道。
	// 只在**有错误**时打印：警告（某某包当前没起作用）不该在启动时刷屏。
	if !rep.OK() {
		fmt.Fprintln(os.Stderr, rep.Explain())
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	prog := tea.NewProgram(kxAdapter{model: m}, opts...)
	_, err = prog.Run()
	m.Dispose()
	return err
}

// kxAdapter 把 bubbletea 的消息转成引擎事件。
type kxAdapter struct{ model *kxflow.Model }

type kxTickMsg struct{}

// Init 起一个每秒的心跳：计时走完要响铃，而那件事与按键无关。
func (a kxAdapter) Init() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return kxTickMsg{} })
}

func (a kxAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.model.Resize(m.Width, m.Height)
		return a, nil
	case kxTickMsg:
		a.model.Tick(time.Now())
		return a, tea.Tick(time.Second, func(time.Time) tea.Msg { return kxTickMsg{} })
	case tea.KeyMsg:
		ev := plugin.Event{Kind: plugin.EventKey, Key: m.String()}
		if m.Type == tea.KeyRunes {
			ev.Runes = m.Runes
		}
		if m.Paste {
			ev.Kind = plugin.EventPaste
		}
		if quit := a.model.Dispatch(ev); quit {
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a kxAdapter) View() string { return a.model.View() }

// printFarewell 在退出后打印当天小结，让用户离开时也有反馈。
func printFarewell(st *store.Store) {
	cfgPaths, err := config.Resolve(config.Default())
	if err != nil {
		return
	}
	cfg, err := config.Load(cfgPaths)
	if err != nil {
		cfg = config.Default()
	}
	day := clock.LogicalDay(clock.New().Now(), cfg.Cutoff())
	data, err := st.Day(day)
	if err != nil || data == nil {
		return
	}
	done, total := data.Counts()
	focus, _ := data.FocusTotal()
	fmt.Printf("\nKQFLOW · %s · 完成 %d/%d 项待办 · 今日专注 %s\n",
		day, done, total, clock.HumanDuration(focus))
}

func usage() {
	fmt.Fprintf(os.Stderr, `KQFLOW — 每天向前一点的时间管理器

版本: %s

用法:
  kqf [选项]

选项:
  -data-dir <路径>   指定数据目录（默认放在可执行文件同级的 kqflow-data）
  -engine <名称>     选择渲染引擎：legacy（默认）或 kxflow（新引擎，开发中）
  -where             只打印数据目录后退出
  -version           只打印版本号后退出
  -h, -help          显示本帮助

数据目录也可以用环境变量 KQFLOW_HOME 指定。

看板操作:
  tab 切换栏位 · j/k 移动 · space 勾选 · a 添加 · t 加子任务 · r 继承昨日 · ? 帮助 · q 退出

计时:
  中间栏按 enter 选择番茄钟 / 倒计时 / 正计时 / 自定义。
  计时中按 p 打开计时菜单（暂停 / 继续 / 结束），结束会先确认。
  空格与回车在计时中仍然是勾选完成 / 进入子任务，不会被计时占用，
  因此终端里最容易误触的两个键不会误伤正在进行的专注。
`, version.Version)
}
