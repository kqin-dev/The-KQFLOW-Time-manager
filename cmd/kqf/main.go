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
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
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
